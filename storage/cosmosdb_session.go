package storage

import (
	"container/list"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// sessionTokenStore keeps the latest Cosmos DB session token per container and
// partition key so reads issued after a write can see it (read-your-writes).
// Tokens for the same partition range are merged per region (see
// mergeSessionTokens), so writes accepted in different regions are all kept.
// Entries expire after ttl and the store holds at most max of them, dropping
// the least recently updated first.
//
// Entries are kept in a list ordered by last update. Because every entry has
// the same ttl, that is also expiry order, so expired and evicted entries are
// always at the front and neither needs a scan of the whole store.
//
// Alongside the per-partition entries, byRange keeps the merged token for each
// partition range of a container, updated on every set, so a read across
// partitions only reads that small map. It is dropped with the container's
// last partition entry.
type sessionTokenStore struct {
	mu          sync.RWMutex
	byContainer map[string]map[string]*list.Element
	byRange     map[string]map[string]rangeSessionToken
	order       *list.List // of *sessionTokenEntry, least recently updated first
	ttl         time.Duration
	max         int
	now         func() time.Time
}

type rangeSessionToken struct {
	token     string
	expiresAt time.Time
}

type sessionTokenEntry struct {
	containerName string
	pk            string
	token         string
	expiresAt     time.Time
}

func newSessionTokenStore() *sessionTokenStore {
	return &sessionTokenStore{
		byContainer: make(map[string]map[string]*list.Element),
		byRange:     make(map[string]map[string]rangeSessionToken),
		order:       list.New(),
		ttl:         defaultSessionTokenTTL,
		max:         defaultSessionTokenMax,
		now:         time.Now,
	}
}

func (s *sessionTokenStore) set(containerName string, pk string, token string) {
	if token == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.removeExpiredLocked(now)
	s.setRangeLocked(containerName, token, now)

	partitions := s.byContainer[containerName]
	if el, ok := partitions[pk]; ok {
		entry := el.Value.(*sessionTokenEntry)
		next, ok := nextSessionToken(entry.token, token)
		if !ok {
			return
		}
		entry.token = next
		entry.expiresAt = now.Add(s.ttl)
		s.order.MoveToBack(el)
		return
	}

	if partitions == nil {
		partitions = make(map[string]*list.Element)
		s.byContainer[containerName] = partitions
	}
	partitions[pk] = s.order.PushBack(&sessionTokenEntry{
		containerName: containerName,
		pk:            pk,
		token:         token,
		expiresAt:     now.Add(s.ttl),
	})
	for s.order.Len() > s.max {
		s.removeLocked(s.order.Front())
	}
}

// get returns the token for one partition of a container, or "" if none is
// held. It never falls back to a token from another partition.
func (s *sessionTokenStore) get(containerName string, pk string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	el, ok := s.byContainer[containerName][pk]
	if !ok {
		return ""
	}
	entry := el.Value.(*sessionTokenEntry)
	if !entry.expiresAt.After(s.now()) {
		return ""
	}
	return entry.token
}

// getForContainer returns a token for a query across all partitions of a
// container: the merged token for each partition range, comma separated, which
// is the compound form Cosmos DB accepts. Tokens that cannot be parsed are left
// out, since they cannot be matched to a range.
func (s *sessionTokenStore) getForContainer(containerName string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := s.now()
	ranges := s.byRange[containerName]
	tokens := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r.expiresAt.After(now) {
			tokens = append(tokens, r.token)
		}
	}
	sort.Strings(tokens)
	return strings.Join(tokens, ",")
}

// setRangeLocked merges token into the token held for its partition range.
func (s *sessionTokenStore) setRangeLocked(containerName string, token string, now time.Time) {
	parsed, ok := parseSessionToken(token)
	if !ok {
		return
	}
	ranges := s.byRange[containerName]
	if ranges == nil {
		ranges = make(map[string]rangeSessionToken)
		s.byRange[containerName] = ranges
	}
	if current, ok := ranges[parsed.rangeID]; ok && current.expiresAt.After(now) {
		token, _ = nextSessionToken(current.token, token)
	}
	ranges[parsed.rangeID] = rangeSessionToken{token: token, expiresAt: now.Add(s.ttl)}
}

func (s *sessionTokenStore) removeExpiredLocked(now time.Time) {
	for el := s.order.Front(); el != nil; el = s.order.Front() {
		if el.Value.(*sessionTokenEntry).expiresAt.After(now) {
			return
		}
		s.removeLocked(el)
	}
}

func (s *sessionTokenStore) removeLocked(el *list.Element) {
	entry := s.order.Remove(el).(*sessionTokenEntry)
	partitions := s.byContainer[entry.containerName]
	delete(partitions, entry.pk)
	if len(partitions) == 0 {
		delete(s.byContainer, entry.containerName)
		delete(s.byRange, entry.containerName)
	}
}

// sessionToken is a parsed Cosmos DB session token:
// "<rangeId>:<version>#<globalLSN>[#<regionId>=<localLSN>...]". Accounts that
// write in more than one region carry a local LSN for each write region.
type sessionToken struct {
	rangeID   string
	version   int64
	globalLSN int64
	regions   []regionLSN // in token order
}

type regionLSN struct {
	id  int64
	lsn int64
}

func parseSessionToken(token string) (sessionToken, bool) {
	rangeID, rest, ok := strings.Cut(token, ":")
	if !ok || rangeID == "" {
		return sessionToken{}, false
	}
	parts := strings.Split(rest, "#")
	if len(parts) < 2 {
		return sessionToken{}, false
	}
	version, err := strconv.ParseInt(parts[0], 10, 64) // may be -1
	if err != nil {
		return sessionToken{}, false
	}
	globalLSN, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return sessionToken{}, false
	}
	parsed := sessionToken{rangeID: rangeID, version: version, globalLSN: globalLSN}
	for _, part := range parts[2:] {
		idStr, lsnStr, ok := strings.Cut(part, "=")
		if !ok {
			return sessionToken{}, false
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			return sessionToken{}, false
		}
		lsn, err := strconv.ParseInt(lsnStr, 10, 64)
		if err != nil {
			return sessionToken{}, false
		}
		parsed.regions = append(parsed.regions, regionLSN{id: id, lsn: lsn})
	}
	return parsed, true
}

func (t sessionToken) String() string {
	var b strings.Builder
	b.WriteString(t.rangeID)
	b.WriteByte(':')
	b.WriteString(strconv.FormatInt(t.version, 10))
	b.WriteByte('#')
	b.WriteString(strconv.FormatInt(t.globalLSN, 10))
	for _, r := range t.regions {
		b.WriteByte('#')
		b.WriteString(strconv.FormatInt(r.id, 10))
		b.WriteByte('=')
		b.WriteString(strconv.FormatInt(r.lsn, 10))
	}
	return b.String()
}

// nextSessionToken returns the token to hold once incoming arrives while
// current is held. Tokens for the same partition range are merged. A token for
// a different range (after a partition split) replaces current, since LSNs
// aren't comparable across ranges; a late response from the old range can
// therefore replace the new range's token, and the next read may not see the
// newer write. It returns false when incoming should be ignored: a token that
// can't be parsed never replaces one that can, or extends its expiry.
func nextSessionToken(current string, incoming string) (string, bool) {
	cur, curOK := parseSessionToken(current)
	inc, incOK := parseSessionToken(incoming)
	switch {
	case !curOK:
		return incoming, true
	case !incOK:
		return current, false
	case inc.rangeID != cur.rangeID:
		return incoming, true
	default:
		return mergeSessionTokens(cur, inc).String(), true
	}
}

// mergeSessionTokens combines two tokens for the same partition range the way
// the Cosmos DB SDKs do: the higher version wins, with the higher global LSN and
// the higher local LSN for each region. A region found only in the
// lower-version token is dropped; when the versions are equal, every region
// from both is kept.
func mergeSessionTokens(a, b sessionToken) sessionToken {
	hi, lo := b, a
	if a.version > b.version {
		hi, lo = a, b
	}
	loLSN := make(map[int64]int64, len(lo.regions))
	for _, r := range lo.regions {
		loLSN[r.id] = r.lsn
	}

	merged := sessionToken{rangeID: hi.rangeID, version: hi.version, globalLSN: max(a.globalLSN, b.globalLSN)}
	for _, r := range hi.regions {
		if lsn, ok := loLSN[r.id]; ok {
			r.lsn = max(r.lsn, lsn)
			delete(loLSN, r.id)
		}
		merged.regions = append(merged.regions, r)
	}
	if a.version == b.version {
		for _, r := range lo.regions {
			if _, ok := loLSN[r.id]; ok {
				merged.regions = append(merged.regions, r)
			}
		}
	}
	return merged
}
