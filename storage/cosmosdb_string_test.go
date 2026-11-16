package storage

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNonEmptyString(t *testing.T) {
	if _, err := nonEmptyString("ok", "item id"); err != nil {
		t.Fatalf("string: %v", err)
	}
	for _, tc := range []struct {
		name string
		v    any
	}{
		{"nil", nil},
		{"int", 42},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := nonEmptyString(tc.v, "item id")
			if err == nil || !strings.Contains(err.Error(), "non-empty string") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestCosmosDBCreateContextRejectsNonStringID(t *testing.T) {
	s := &CosmosDBAdapter{}
	type badID struct {
		ID any    `json:"id"`
		PK string `json:"pk"`
	}
	err := s.CreateContext(context.Background(), &badID{ID: 42, PK: "p"}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "non-empty string") {
		t.Fatalf("error %q", err.Error())
	}
}

func TestCosmosDBCreateContextRejectsMissingID(t *testing.T) {
	s := &CosmosDBAdapter{}
	type noID struct {
		PK string `json:"pk"`
	}
	err := s.CreateContext(context.Background(), &noID{PK: "p"}, nil)
	if err == nil || !strings.Contains(err.Error(), "id field") {
		t.Fatalf("got %v", err)
	}
}

func TestCosmosDBCreateContextAllowsEmptyPartitionKey(t *testing.T) {
	var gotPK string
	s := newFakeCosmosAdapter(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Path == "/" {
			return fakeCosmosJSON(req, `{"id":"fake","writableLocations":[],"readableLocations":[]}`), nil
		}
		if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/docs") {
			gotPK = req.Header.Get("x-ms-documentdb-partitionkey")
			header := http.Header{"Content-Type": []string{"application/json"}}
			header.Set("x-ms-session-token", "0:-1#1")
			return &http.Response{
				StatusCode: http.StatusCreated,
				Header:     header,
				Body:       io.NopCloser(strings.NewReader(`{"id":"abc","pk":""}`)),
				Request:    req,
			}, nil
		}
		t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		return nil, nil
	})

	type item struct {
		ID string `json:"id"`
		PK string `json:"pk"`
	}
	err := s.CreateContext(context.Background(), &item{ID: "abc", PK: ""})
	if err != nil {
		t.Fatalf("empty partition key: %v", err)
	}
	if gotPK != `[""]` {
		t.Fatalf("partition key header = %q; want %q", gotPK, `[""]`)
	}
}

func TestCosmosDBCreateContextRejectsNonStringPartitionKey(t *testing.T) {
	s := newFakeCosmosAdapter(t, func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodGet && req.URL.Path == "/" {
			return fakeCosmosJSON(req, `{"id":"fake","writableLocations":[],"readableLocations":[]}`), nil
		}
		t.Fatal("non-string partition key reached the client")
		return nil, nil
	})
	type item struct {
		ID string `json:"id"`
		PK any    `json:"pk"`
	}
	err := s.CreateContext(context.Background(), &item{ID: "abc", PK: 7})
	if err == nil || !strings.Contains(err.Error(), "partition key must be a string") {
		t.Fatalf("got %v", err)
	}
}
