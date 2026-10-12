package ghapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTest(h http.HandlerFunc) (*Client, func()) {
	s := httptest.NewServer(h)
	c := New("secret-token")
	c.BaseURL = s.URL
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	return c, s.Close
}

func TestRetryAfterThenOK(t *testing.T) {
	var n int32
	c, done := newTest(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Error("missing auth")
		}
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		w.Write([]byte("ok"))
	})
	defer done()
	r, err := c.Get(context.Background(), "/x")
	if err != nil || r.Status != 200 {
		t.Fatalf("%v %v", r, err)
	}
}

func TestPlain403IsReturnedNotRetried(t *testing.T) {
	c, done := newTest(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) })
	defer done()
	r, err := c.Get(context.Background(), "/x")
	if err != nil || r.Status != 403 {
		t.Fatalf("%v %v", r, err)
	}
}

func TestExhaustedRateLimitIsError(t *testing.T) {
	c, done := newTest(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
		w.WriteHeader(403)
	})
	defer done()
	_, err := c.Get(context.Background(), "/x")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
}

func TestGraphQLPostsJSONAndNeedsToken(t *testing.T) {
	var gotBody, gotType, gotMethod, gotPath string
	c, done := newTest(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotType, gotMethod, gotPath = string(b), r.Header.Get("Content-Type"), r.Method, r.URL.Path
		w.Write([]byte(`{"data":{}}`))
	})
	defer done()
	r, err := c.GraphQL(context.Background(), "query{viewer{login}}", map[string]any{"n": 1})
	if err != nil || r.Status != 200 {
		t.Fatalf("%v %v", r, err)
	}
	if gotMethod != "POST" || gotPath != "/graphql" || gotType != "application/json" || !strings.Contains(gotBody, `"query":"query{viewer{login}}"`) || !strings.Contains(gotBody, `"n":1`) {
		t.Errorf("%s %s %s %s", gotMethod, gotPath, gotType, gotBody)
	}
	anon := New("")
	anon.BaseURL = c.BaseURL
	if _, err := anon.GraphQL(context.Background(), "query{}", nil); err == nil {
		t.Error("anonymous GraphQL must be refused locally, not sent")
	}
}

func TestGraphQLRetryResendsBody(t *testing.T) {
	var n int32
	var bodies []string
	c, done := newTest(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if atomic.AddInt32(&n, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		w.Write([]byte(`{"data":{}}`))
	})
	defer done()
	if _, err := c.GraphQL(context.Background(), "query{a}", nil); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] || bodies[1] == "" {
		t.Fatalf("retry must resend the same body: %q", bodies)
	}
}
