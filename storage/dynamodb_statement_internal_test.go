package storage

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// executeStatementRequest is the part of an ExecuteStatement request body the
// tests inspect. Parameters keep DynamoDB's wire form, e.g. {"S": "acme"}.
type executeStatementRequest struct {
	Statement  string
	Parameters []map[string]any
	Limit      int
	NextToken  string
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
		if err := json.Unmarshal(body, &parsed); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
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
	if n := len(rec.last(t).Parameters); n != 0 {
		t.Fatalf("len(Parameters) = %d; want 0", n)
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
