package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/0x524a/gh-residue/internal/ghapi"
	"github.com/0x524a/gh-residue/internal/model"
)

// A fork's own refs/pull/* are invisible to GraphQL. REST lists them
// (GET /repos/{fork}/git/matching-refs/pull/ returns ref names and tip SHAs
// only), and the compare endpoint, called through the user's own repository,
// says whether a tip contains an old commit: the network shares Git data, so
// the tip resolves there. The response is asked for an out-of-range page, so it
// names no commits and no files.
//
//	compare {old}...{tip}: ahead, identical -> the tip contains old
//	                       behind, diverged -> it does not
//	anything else (404, odd status, error)  -> unknown
const (
	maxPullTips    = 100
	comparePageOOR = 100000
)

type pullRef struct {
	Ref    string `json:"ref"`
	Object struct {
		SHA  string `json:"sha"`
		Type string `json:"type"`
	} `json:"object"`
}

// pullVerdicts says, for each SHA, whether any of the fork's own pull refs
// contains it. prTotal is the fork's pull request count; the listing must show at
// least that many head refs, or it is treated as incomplete.
func pullVerdicts(ctx context.Context, c *ghapi.Client, base, fork string, shas []string, prTotal int) map[string]refVerdict {
	out := make(map[string]refVerdict, len(shas))
	all := func(st model.Status, ev string) map[string]refVerdict {
		for _, s := range shas {
			out[s] = refVerdict{st, ev}
		}
		return out
	}
	r, err := c.Get(ctx, "/repos/"+fork+"/git/matching-refs/pull/")
	if err != nil {
		return all(model.Unknown, "fork pull refs unavailable: "+err.Error())
	}
	if r.Status != 200 {
		return all(model.Unknown, fmt.Sprintf("fork pull refs unavailable: HTTP %d", r.Status))
	}
	var refs []pullRef
	if err := json.Unmarshal(r.Body, &refs); err != nil {
		return all(model.Unknown, "fork pull refs unreadable")
	}
	heads := 0
	tips := map[string]string{} // tip SHA -> a ref carrying it
	for _, pr := range refs {
		if strings.HasSuffix(pr.Ref, "/head") {
			heads++
		}
		if pr.Object.Type != "commit" || !sha40.MatchString(pr.Object.SHA) {
			return all(model.Unknown, "fork pull ref "+pr.Ref+" does not point at a commit")
		}
		if _, ok := tips[pr.Object.SHA]; !ok {
			tips[pr.Object.SHA] = pr.Ref
		}
	}
	if heads < prTotal {
		return all(model.Unknown, fmt.Sprintf("fork lists %d pull request refs for %d pull requests", heads, prTotal))
	}
	if len(tips) > maxPullTips {
		return all(model.Unknown, fmt.Sprintf("fork has more than %d distinct pull ref tips", maxPullTips))
	}

	for _, s := range shas {
		var holder, unknown string
		for tip, ref := range tips {
			cr, err := c.Get(ctx, fmt.Sprintf("/repos/%s/compare/%s...%s?per_page=1&page=%d", base, s, tip, comparePageOOR))
			var body struct {
				Status string `json:"status"`
			}
			switch {
			case err != nil:
				unknown = "error: " + err.Error()
			case cr.Status != 200:
				unknown = fmt.Sprintf("compare HTTP %d", cr.Status)
			case json.Unmarshal(cr.Body, &body) != nil:
				unknown = "compare unreadable"
			case body.Status == "ahead" || body.Status == "identical":
				if holder == "" || ref < holder {
					holder = ref
				}
			case body.Status == "behind" || body.Status == "diverged":
			default:
				unknown = "unexpected compare status " + body.Status
			}
		}
		switch {
		case holder != "": // a proven holder stands even if other tips failed
			out[s] = refVerdict{model.Confirmed, holder}
		case unknown != "":
			out[s] = refVerdict{model.Unknown, unknown}
		default:
			out[s] = refVerdict{model.NotFound, "no pull ref of the fork contains it"}
		}
	}
	return out
}
