// Package client is a small client for the Tailcove /api/v1 write API.
package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultEndpoint is the hosted Tailcove app.
const DefaultEndpoint = "https://app.tailcove.dev"

// ErrNotFound is returned for a 404: the thing is gone, or it belongs to another team.
var ErrNotFound = errors.New("not found")

// APIError is a refusal from the API with its message and, for a 422, the per-field messages.
type APIError struct {
	Status  int
	Message string
	Fields  map[string][]string
}

func (e *APIError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "tailcove API %d: %s", e.Status, e.Message)
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "; %s: %s", k, strings.Join(e.Fields[k], " "))
	}
	return b.String()
}

// Client talks to one team's API through one token.
type Client struct {
	base      string
	token     string
	userAgent string
	http      *http.Client
	// maxRetries bounds the retries after a 429 or a 409 (a request with the same key still running).
	maxRetries int
	// sleep is replaced in tests.
	sleep func(time.Duration)
}

// New returns a client. endpoint is the app's origin (no /api/v1).
func New(endpoint, token, userAgent string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Client{
		base:       strings.TrimRight(endpoint, "/") + "/api/v1",
		token:      token,
		userAgent:  userAgent,
		http:       &http.Client{Timeout: 60 * time.Second},
		maxRetries: 5,
		sleep:      time.Sleep,
	}
}

// do sends one request. A POST carries an Idempotency-Key that stays the same across its retries,
// so a retry after a timeout never creates a second thing.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return err
		}
	}

	key := ""
	if method == http.MethodPost {
		key = newKey()
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			req.Header.Set("Idempotency-Key", key)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			if attempt < c.maxRetries && method != http.MethodPost && ctx.Err() == nil {
				c.sleep(backoff(attempt))
				continue
			}
			return err
		}
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}

		switch {
		case resp.StatusCode == http.StatusNotFound:
			return ErrNotFound
		case (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusConflict) && attempt < c.maxRetries:
			c.sleep(retryAfter(resp, attempt))
			continue
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if out == nil || len(raw) == 0 {
				return nil
			}
			return json.Unmarshal(raw, out)
		default:
			return parseError(resp.StatusCode, raw)
		}
	}
}

func parseError(status int, raw []byte) error {
	e := &APIError{Status: status, Message: http.StatusText(status)}
	var body struct {
		Message string              `json:"message"`
		Errors  map[string][]string `json:"errors"`
	}
	if json.Unmarshal(raw, &body) == nil {
		if body.Message != "" {
			e.Message = body.Message
		}
		e.Fields = body.Errors
	}
	return e
}

func backoff(attempt int) time.Duration {
	return time.Duration(1<<uint(attempt)) * 500 * time.Millisecond
}

func retryAfter(resp *http.Response, attempt int) time.Duration {
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s >= 0 && s <= 120 {
		return time.Duration(s) * time.Second
	}
	return backoff(attempt)
}

func newKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

type envelope[T any] struct {
	Data T `json:"data"`
}

func get[T any](ctx context.Context, c *Client, path string) (T, error) {
	var e envelope[T]
	err := c.do(ctx, http.MethodGet, path, nil, &e)
	return e.Data, err
}

func send[T any](ctx context.Context, c *Client, method, path string, body map[string]any) (T, error) {
	var e envelope[T]
	err := c.do(ctx, method, path, body, &e)
	return e.Data, err
}

func id(n int64) string { return url.PathEscape(strconv.FormatInt(n, 10)) }
