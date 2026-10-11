package ghapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
