package storage

import (
	"container/list"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultSessionTokenTTL = 5 * time.Minute
	defaultSessionTokenMax = 10000
)

// sessionTokenStore keeps the newest Cosmos DB session token per container and
// partition key so reads issued after a write can see it (read-your-writes).
// Entries expire after ttl and the store holds at most max of them, dropping
// the least recently updated first.
//
// Entries are kept in a list ordered by last update. Because every entry has
// the same ttl, that is also expiry order, so expired and evicted entries are
// always at the front and neither needs a scan of the whole store.
//
// Alongside the per-partition entries, byRange keeps the newest token for each
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
		if !shouldReplaceSessionToken(entry.token, token) {
			return
		}
		entry.token = token
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
// container: the newest token for each partition range, comma separated, which
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

// setRangeLocked records token as the newest for its partition range, unless
// the range already holds a newer one that hasn't expired.
func (s *sessionTokenStore) setRangeLocked(containerName string, token string, now time.Time) {
	rank, ok := parseSessionToken(token)
	if !ok {
		return
	}
	ranges := s.byRange[containerName]
	if ranges == nil {
		ranges = make(map[string]rangeSessionToken)
		s.byRange[containerName] = ranges
	}
	if current, ok := ranges[rank.rangeID]; ok && current.expiresAt.After(now) && !shouldReplaceSessionToken(current.token, token) {
		return
	}
	ranges[rank.rangeID] = rangeSessionToken{token: token, expiresAt: now.Add(s.ttl)}
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

// sessionTokenRank is the comparable part of a Cosmos DB session token:
// "<rangeId>:<version>#<globalLSN>[#<regionId>=<localLSN>...]". Region
// entries are ignored; the global LSN orders writes within a range.
type sessionTokenRank struct {
	rangeID string
	version int64
	lsn     int64
}

func parseSessionToken(token string) (sessionTokenRank, bool) {
	rangeID, rest, ok := strings.Cut(token, ":")
	if !ok || rangeID == "" {
		return sessionTokenRank{}, false
	}
	parts := strings.Split(rest, "#")
	if len(parts) < 2 {
		return sessionTokenRank{}, false
	}
	version, err := strconv.ParseInt(parts[0], 10, 64) // may be -1
	if err != nil {
		return sessionTokenRank{}, false
	}
	lsn, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return sessionTokenRank{}, false
	}
	return sessionTokenRank{rangeID: rangeID, version: version, lsn: lsn}, true
}

// shouldReplaceSessionToken reports whether incoming should replace current.
// Within one partition range it keeps the newest write; after a split (a new
// range id) it takes the incoming token, since LSNs aren't comparable across
// ranges.
func shouldReplaceSessionToken(current string, incoming string) bool {
	cur, curOK := parseSessionToken(current)
	inc, incOK := parseSessionToken(incoming)
	switch {
	case !curOK:
		return true
	case !incOK:
		return false
	case inc.rangeID != cur.rangeID:
		return true
	case inc.version != cur.version:
		return inc.version > cur.version
	default:
		return inc.lsn >= cur.lsn // equal refreshes the expiry
	}
}
