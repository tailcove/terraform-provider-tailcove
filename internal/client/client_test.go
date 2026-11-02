package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(srv.URL, "tok", "test")
	c.sleep = func(time.Duration) {}
	return c
}

func TestCreateSourceSendsTokenAndKey(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sources" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("no bearer token")
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Errorf("no idempotency key on a POST")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "web" {
			t.Errorf("body = %v", body)
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"data":{"id":"src_01jbx5q8k7m3n2p4r6s8t0v1w3","name":"web","retentionDays":3,"ingestToken":"abc"}}`))
	})

	s, err := c.CreateSource(context.Background(), map[string]any{"name": "web"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "src_01jbx5q8k7m3n2p4r6s8t0v1w3" || s.IngestToken != "abc" || s.RetentionDays != 3 {
		t.Fatalf("got %+v", s)
	}
}

func TestPutHasNoIdempotencyKey(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Idempotency-Key") != "" {
			t.Errorf("a PUT should not carry a key")
		}
		_, _ = w.Write([]byte(`{"data":{"id":"src_01jbx5q8k7m3n2p4r6s8t0v1w3","name":"web"}}`))
	})
	if _, err := c.UpdateSource(context.Background(), "src_01jbx5q8k7m3n2p4r6s8t0v1w3", map[string]any{"name": "web"}); err != nil {
		t.Fatal(err)
	}
}

func TestNotFound(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	if _, err := c.GetMonitor(context.Background(), "mon_01jbx5q8k7m3n2p4r6s8t0v1w3"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestValidationErrorCarriesFields(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"message":"The given data was invalid.","errors":{"url":["The url is invalid."]}}`))
	})
	_, err := c.CreateMonitor(context.Background(), map[string]any{})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 422 || apiErr.Fields["url"][0] != "The url is invalid." {
		t.Fatalf("err = %v", err)
	}
	if got := apiErr.Error(); got != "tailcove API 422: The given data was invalid.; url: The url is invalid." {
		t.Fatalf("message = %q", got)
	}
}

func TestRetriesAThrottleWithTheSameKey(t *testing.T) {
	var calls int32
	var firstKey string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			firstKey = r.Header.Get("Idempotency-Key")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		if r.Header.Get("Idempotency-Key") != firstKey {
			t.Errorf("the retry used another key")
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"data":{"id":"src_01jbx5q8k7m3n2p4r6s8t0v1w4","name":"a"}}`))
	})
	if _, err := c.CreateSource(context.Background(), map[string]any{"name": "a"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestGivesUpAfterTheRetries(t *testing.T) {
	var calls int32
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(429)
	})
	_, err := c.ListSources(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 429 {
		t.Fatalf("err = %v", err)
	}
	if calls != int32(c.maxRetries)+1 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestAlertPathsUseTheSource(t *testing.T) {
	var path string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(204)
	})
	if err := c.DeleteAlert(context.Background(), "src_01jbx5q8k7m3n2p4r6s8t0v1w3", "alr_01jbx5qb8d2f4h6j8k0m2n4p6q"); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/sources/src_01jbx5q8k7m3n2p4r6s8t0v1w3/alerts/alr_01jbx5qb8d2f4h6j8k0m2n4p6q" {
		t.Fatalf("path = %s", path)
	}
}
