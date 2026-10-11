// Package ghapi is a minimal read-only GitHub REST client.
package ghapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

type Client struct {
	BaseURL    string
	Token      string // never logged or serialised
	HTTP       *http.Client
	MaxRetries int
	MaxWait    time.Duration // longest single rate-limit wait we accept
	Sleep      func(context.Context, time.Duration) error
}

func New(token string) *Client {
	return &Client{
		BaseURL:    "https://api.github.com",
		Token:      token,
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		MaxRetries: 3,
		MaxWait:    2 * time.Minute,
		Sleep:      sleep,
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ResolveToken: GH_TOKEN, GITHUB_TOKEN, then `gh auth token`. Empty means
// unauthenticated (fine for public repos, 60 req/h).
func ResolveToken() string {
	for _, k := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ErrRateLimited means the retry budget or the acceptable wait was exhausted.
var ErrRateLimited = errors.New("rate limited")

// Get performs a GET. Non-2xx statuses are returned, not errors, so probes
// decide what they mean. Errors are transport failures or exhausted rate limits;
// callers must map them to Unknown.
func (c *Client) Get(ctx context.Context, path string) (*Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", "gh-residue")
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request failed: %w", scrub(err, c.Token))
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if rerr != nil {
			return nil, fmt.Errorf("read body: %w", rerr)
		}
		r := &Response{Status: resp.StatusCode, Header: resp.Header, Body: body}
		if r.Status != http.StatusForbidden && r.Status != http.StatusTooManyRequests {
			return r, nil
		}
		wait, ok := rateLimitWait(r.Header, time.Now())
		if !ok {
			return r, nil // a plain 403 (permissions), not a limit
		}
		if attempt >= c.MaxRetries || wait > c.MaxWait {
			return nil, fmt.Errorf("%w (would wait %s)", ErrRateLimited, wait.Round(time.Second))
		}
		if err := c.Sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

// rateLimitWait decides whether a 403/429 is a rate limit and how long to wait.
func rateLimitWait(h http.Header, now time.Time) (time.Duration, bool) {
	if v := h.Get("Retry-After"); v != "" {
		if s, err := strconv.Atoi(v); err == nil {
			return time.Duration(s) * time.Second, true
		}
	}
	if h.Get("X-RateLimit-Remaining") == "0" {
		if s, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			d := time.Unix(s, 0).Sub(now)
			if d < time.Second {
				d = time.Second
			}
			return d, true
		}
	}
	return 0, false
}

func scrub(err error, token string) error {
	if token == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "***"))
}
