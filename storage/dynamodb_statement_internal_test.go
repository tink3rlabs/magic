package storage

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	serviceErrors "github.com/tink3rlabs/magic/errors"
)

// executeStatementRequest is the part of an ExecuteStatement request body the
// tests inspect. Parameters keep DynamoDB's wire form, e.g. {"S": "acme"}.
type executeStatementRequest struct {
	Statement  string
	Parameters []map[string]any
	Limit      int
	NextToken  string
	// HasParameters reports whether the body had a Parameters key at all;
	// DynamoDB rejects an empty list.
	HasParameters bool `json:"-"`
}

// statementRecorder captures the ExecuteStatement requests sent by an adapter
// whose client points at a local HTTP server instead of AWS.
type statementRecorder struct {
	mu       sync.Mutex
	requests []executeStatementRequest
}

func (r *statementRecorder) last(t *testing.T) executeStatementRequest {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		t.Fatal("no ExecuteStatement request was sent")
	}
	return r.requests[len(r.requests)-1]
}

func (r *statementRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// newRecordingDynamoAdapter returns an adapter whose ExecuteStatement calls
// are recorded and answered with responseItems (DynamoDB JSON, may be "").
func newRecordingDynamoAdapter(t *testing.T, responseItems string) (*DynamoDBAdapter, *statementRecorder) {
	t.Helper()
	rec := &statementRecorder{}
	if responseItems == "" {
		responseItems = "[]"
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasSuffix(req.Header.Get("X-Amz-Target"), ".ExecuteStatement") {
			http.Error(w, "unexpected operation "+req.Header.Get("X-Amz-Target"), http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var parsed executeStatementRequest
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(body, &parsed); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := json.Unmarshal(body, &keys); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, parsed.HasParameters = keys["Parameters"]
		rec.mu.Lock()
		rec.requests = append(rec.requests, parsed)
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-amz-json-1.0")
		_, _ = io.WriteString(w, `{"Items":`+responseItems+`}`)
	}))
	t.Cleanup(srv.Close)

	client := dynamodb.New(dynamodb.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(srv.URL),
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
		HTTPClient:   srv.Client(),
	})
	return &DynamoDBAdapter{DB: client}, rec
}

func stringParam(t *testing.T, p map[string]any) string {
	t.Helper()
	s, ok := p["S"].(string)
	if !ok {
		t.Fatalf("parameter %v is not a string attribute", p)
	}
	return s
}

func TestDynamoDBQueryBindsParametersInOrder(t *testing.T) {
	s, rec := newRecordingDynamoAdapter(t, `[{"id":{"S":"1"},"name":{"S":"O'Brien"}}]`)

	var dest []dynamoSampleItem
	stmt := `SELECT * FROM "dynamo_sample_items" WHERE "id" = ? AND "name" = ?`
	_, err := s.Query(&dest, stmt, 10, "", map[string]any{
		QueryParametersKey: []any{"1", "O'Brien"},
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}

	got := rec.last(t)
	if got.Statement != stmt {
		t.Errorf("Statement = %q; want it sent unchanged", got.Statement)
	}
	if len(got.Parameters) != 2 {
		t.Fatalf("Parameters = %v; want 2", got.Parameters)
	}
	if stringParam(t, got.Parameters[0]) != "1" || stringParam(t, got.Parameters[1]) != "O'Brien" {
		t.Errorf("Parameters = %v; want [1, O'Brien] in order", got.Parameters)
	}
	if len(dest) != 1 || dest[0].Name != "O'Brien" {
		t.Errorf("dest = %v; want the returned row", dest)
	}
}

func TestDynamoDBQueryAcceptsTypedSlices(t *testing.T) {
	s, rec := newRecordingDynamoAdapter(t, "")
	var dest []dynamoSampleItem
	if _, err := s.Query(&dest, `SELECT * FROM "t" WHERE "id" IN (?, ?)`, 10, "", map[string]any{
		QueryParametersKey: []string{"a", "b"},
	}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if n := len(rec.last(t).Parameters); n != 2 {
		t.Fatalf("len(Parameters) = %d; want 2", n)
	}
}

func TestDynamoDBQueryWithoutParametersSendsNone(t *testing.T) {
	s, rec := newRecordingDynamoAdapter(t, "")
	var dest []dynamoSampleItem
	if _, err := s.Query(&dest, `SELECT * FROM "t"`, 10, ""); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if rec.last(t).HasParameters {
		t.Fatal("request has a Parameters key; want none")
	}
}

// ExecuteStatement requires Parameters to have at least one item, so a
// statement with no placeholders must omit the key rather than send [].
func TestDynamoDBStatementsWithoutPlaceholdersOmitParameters(t *testing.T) {
	cases := map[string]func(s *DynamoDBAdapter, dest *[]dynamoSampleItem) error{
		"List with a nil filter value": func(s *DynamoDBAdapter, dest *[]dynamoSampleItem) error {
			_, err := s.List(dest, "id", map[string]any{"name": nil}, 10, "")
			return err
		},
		"Search for any value": func(s *DynamoDBAdapter, dest *[]dynamoSampleItem) error {
			_, err := s.Search(dest, "id", "id:*", 10, "")
			return err
		},
		"Query with an empty parameter slice": func(s *DynamoDBAdapter, dest *[]dynamoSampleItem) error {
			_, err := s.Query(dest, `SELECT * FROM "t"`, 10, "", map[string]any{QueryParametersKey: []any{}})
			return err
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			s, rec := newRecordingDynamoAdapter(t, "")
			var dest []dynamoSampleItem
			if err := run(s, &dest); err != nil {
				t.Fatal(err)
			}
			if got := rec.last(t); got.HasParameters {
				t.Fatalf("statement %q sent Parameters %v; want the key omitted", got.Statement, got.Parameters)
			}
		})
	}
}

func TestDynamoDBQueryRejectsNonSliceParameters(t *testing.T) {
	s, rec := newRecordingDynamoAdapter(t, "")
	var dest []dynamoSampleItem
	for _, bad := range []any{"1", 1, map[string]any{"id": "1"}, []byte("1")} {
		if _, err := s.Query(&dest, `SELECT * FROM "t" WHERE "id" = ?`, 10, "", map[string]any{
			QueryParametersKey: bad,
		}); err == nil {
			t.Errorf("Query with %s=%T returned no error", QueryParametersKey, bad)
		}
	}
	if rec.count() != 0 {
		t.Fatalf("a rejected Query must not reach DynamoDB, sent %d requests", rec.count())
	}
}

func TestDynamoDBListSendsOrderedFilterStatement(t *testing.T) {
	s, rec := newRecordingDynamoAdapter(t, "")
	var dest []dynamoSampleItem
	if _, err := s.List(&dest, "id", map[string]any{"name": "bob", "id": "1"}, 10, ""); err != nil {
		t.Fatalf("List: %v", err)
	}
	got := rec.last(t)
	want := `SELECT * FROM "dynamo_sample_items" WHERE "id" = ? AND "name" = ? ORDER BY id`
	if got.Statement != want {
		t.Fatalf("Statement = %q; want %q", got.Statement, want)
	}
	if stringParam(t, got.Parameters[0]) != "1" || stringParam(t, got.Parameters[1]) != "bob" {
		t.Fatalf("Parameters = %v; want [1, bob]", got.Parameters)
	}
}

func TestDynamoDBListAndSearchReadFromIndex(t *testing.T) {
	s, rec := newRecordingDynamoAdapter(t, "")
	params := map[string]any{IndexKey: "name_index"}

	var dest []dynamoSampleItem
	if _, err := s.List(&dest, "id", map[string]any{"name": "bob"}, 10, "", params); err != nil {
		t.Fatalf("List: %v", err)
	}
	if want := `SELECT * FROM "dynamo_sample_items"."name_index" WHERE "name" = ? ORDER BY id`; rec.last(t).Statement != want {
		t.Fatalf("List Statement = %q; want %q", rec.last(t).Statement, want)
	}

	if _, err := s.Search(&dest, "id", "name:bob", 10, "", params); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := rec.last(t).Statement; !strings.HasPrefix(got, `SELECT * FROM "dynamo_sample_items"."name_index" WHERE `) {
		t.Fatalf("Search Statement = %q; want it to read from the index", got)
	}
}

func TestDynamoDBRejectsInvalidIndexBeforeQuerying(t *testing.T) {
	s, rec := newRecordingDynamoAdapter(t, "")
	var dest []dynamoSampleItem
	for _, bad := range []any{`x" WHERE "1"="1`, "ab", "name index", strings.Repeat("a", 256), 42, nil} {
		params := map[string]any{IndexKey: bad}
		if _, err := s.List(&dest, "id", nil, 10, "", params); err == nil {
			t.Errorf("List with %s=%v returned no error", IndexKey, bad)
		}
		if _, err := s.Search(&dest, "id", "", 10, "", params); err == nil {
			t.Errorf("Search with %s=%v returned no error", IndexKey, bad)
		}
	}
	if rec.count() != 0 {
		t.Fatalf("an invalid index must not reach DynamoDB, sent %d requests", rec.count())
	}
}

// The SQL adapter reports invalid search queries as BadRequest so HTTP
// handlers can return 400; DynamoDB returned them as plain errors.
func TestDynamoDBSearchReportsInvalidQueriesAsBadRequest(t *testing.T) {
	s, rec := newRecordingDynamoAdapter(t, "")
	var dest []dynamoSampleItem
	for _, query := range []string{
		"secret:x",     // unknown field
		"name:(",       // syntax error
		"name:*abc",    // pattern DynamoDB can't express
		"name:a OR )(", // unbalanced parentheses
	} {
		t.Run(query, func(t *testing.T) {
			_, err := s.Search(&dest, "id", query, 10, "")
			var bad *serviceErrors.BadRequest
			if !errors.As(err, &bad) {
				t.Fatalf("Search(%q) error = %v (%T); want *errors.BadRequest", query, err, err)
			}
		})
	}
	if rec.count() != 0 {
		t.Fatalf("an invalid query must not reach DynamoDB, sent %d requests", rec.count())
	}
}

// GetInstance wraps adapters for telemetry; the wrapper must pass params on.
func TestDynamoDBQueryParametersPassThroughTelemetryWrapper(t *testing.T) {
	inner, rec := newRecordingDynamoAdapter(t, "")
	s := wrapForTelemetry(inner)
	var dest []dynamoSampleItem
	if _, err := s.Query(&dest, `SELECT * FROM "t" WHERE "id" = ?`, 10, "", map[string]any{
		QueryParametersKey: []any{"1"},
	}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if n := len(rec.last(t).Parameters); n != 1 {
		t.Fatalf("len(Parameters) = %d; want 1", n)
	}
}
