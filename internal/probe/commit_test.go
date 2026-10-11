package probe

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/0x524a/gh-residue/internal/ghapi"
	"github.com/0x524a/gh-residue/internal/model"
)

const sha = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func apiClient(h http.HandlerFunc) (*ghapi.Client, func()) {
	s := httptest.NewServer(h)
	c := ghapi.New("")
	c.BaseURL = s.URL
	return c, s.Close
}

// Property for ADR-0002: no status other than 200/422-no-commit may yield NotFound.
func TestCheckCommitNeverNotFoundOnErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   model.Status
	}{
		{200, "{}", model.Confirmed},
		{422, `{"message":"No commit found for SHA: x"}`, model.NotFound},
		{422, `{"message":"Validation Failed"}`, model.Unknown},
		{404, `{"message":"Not Found"}`, model.Unknown},
		{403, `{}`, model.Unknown},
		{409, `{"message":"Git Repository is empty."}`, model.Unknown},
		{500, ``, model.Unknown},
		{502, ``, model.Unknown},
		{301, ``, model.Unknown},
	}
	for _, tc := range cases {
		c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			w.Write([]byte(tc.body))
		})
		got, _ := checkCommit(context.Background(), c, "o/r", sha)
		done()
		if got != tc.want {
			t.Errorf("status %d body %q: got %s want %s", tc.status, tc.body, got, tc.want)
		}
	}
}

func TestCheckCommitTransportErrorIsUnknown(t *testing.T) {
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) {})
	done() // server closed: connection refused
	if got, _ := checkCommit(context.Background(), c, "o/r", sha); got != model.Unknown {
		t.Fatalf("got %s", got)
	}
}

func TestBaseObject(t *testing.T) {
	c, done := apiClient(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	defer done()
	res := BaseObject{Client: c}.Run(context.Background(), "o/r", []model.OldCommit{{SHA: sha}})
	if len(res.Findings) != 1 || res.Findings[0].Status != model.Confirmed || !res.Surfaces[0].Complete {
		t.Fatalf("%+v", res)
	}
}
