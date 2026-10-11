package probe

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/0x524a/gh-residue/internal/ghapi"
	"github.com/0x524a/gh-residue/internal/model"
)

// checkCommit asks whether sha resolves under repo's URL.
//
//	200            -> Confirmed (verified by Gate 0, also for dangling commits)
//	422 "No commit found" -> NotFound (verified live: existing repo, absent SHA)
//	anything else  -> Unknown. In particular 404 is a missing OR hidden repo and
//	can't be told apart, so it must not mean "clean".
func checkCommit(ctx context.Context, c *ghapi.Client, repo, sha string) (model.Status, string) {
	r, err := c.Get(ctx, fmt.Sprintf("/repos/%s/commits/%s", repo, url.PathEscape(sha)))
	if err != nil {
		return model.Unknown, "error: " + err.Error()
	}
	switch {
	case r.Status == 200:
		return model.Confirmed, "HTTP 200"
	case r.Status == 422 && strings.Contains(string(r.Body), "No commit found"):
		return model.NotFound, "HTTP 422 No commit found"
	default:
		return model.Unknown, fmt.Sprintf("HTTP %d", r.Status)
	}
}

// BaseObject is probe 1: is the SHA resolvable through the repo's own URL.
type BaseObject struct {
	Client *ghapi.Client
	Known  *Known
}

func (p BaseObject) WithKnown(k *Known) Probe { p.Known = k; return p }

func (BaseObject) Name() string { return "base-object" }

func (p BaseObject) Run(ctx context.Context, repo string, commits []model.OldCommit) Result {
	var res Result
	for _, c := range commits {
		if p.Known.Has(c.SHA, model.SurfaceBaseObject, repo) {
			continue
		}
		st, ev := checkCommit(ctx, p.Client, repo, c.SHA)
		res.Findings = append(res.Findings, finding(c.SHA, model.SurfaceBaseObject, repo, st, ev))
	}
	res.Surfaces = []model.SurfaceCoverage{{
		Surface: model.SurfaceBaseObject, Complete: complete(res.Findings, model.SurfaceBaseObject),
	}}
	return res
}
