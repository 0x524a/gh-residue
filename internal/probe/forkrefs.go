package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/0x524a/gh-residue/internal/ghapi"
	"github.com/0x524a/gh-residue/internal/model"
)

// The ref-holding fork check answers what the object check cannot: does any
// branch or tag in the fork reach the old commit, including as an ancestor of
// later commits? `git ls-remote` can't say (it lists tips only), and fetching a
// fork would download another person's content. GraphQL's Ref.compare returns
// only a status, so no content leaves GitHub.
//
// Ref.compare(headRef: X) on a ref R, verified live on a public repo:
//
//	IDENTICAL, BEHIND  -> X is R's tip or an ancestor of it: R contains X.
//	AHEAD, DIVERGED    -> R does not contain X.
//
// An unresolvable X returns an opaque "Something went wrong" error that cannot
// be told apart from a transient fault, so every error path is Unknown.

const (
	refsPageSize    = 100
	maxRefPages     = 5  // per prefix; beyond that the fork is reported Unknown, not clean
	shaChunk        = 20 // SHAs per query, one aliased compare each
	maxEvidenceRefs = 5
)

var sha40 = regexp.MustCompile(`^[0-9a-f]{40}$`)

type refVerdict struct {
	Status   model.Status
	Evidence string
}

type refQueryResp struct {
	Errors []json.RawMessage `json:"errors"`
	Data   struct {
		Repository *struct {
			Refs struct {
				PageInfo struct {
					HasNextPage bool   `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
				Nodes []map[string]json.RawMessage `json:"nodes"`
			} `json:"refs"`
		} `json:"repository"`
	} `json:"data"`
}

func refsQuery(prefix string, shas []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `query($o:String!,$n:String!,$after:String){repository(owner:$o,name:$n){refs(refPrefix:%q,first:%d,after:$after){pageInfo{hasNextPage endCursor} nodes{name`, prefix, refsPageSize)
	for i, s := range shas { // s is validated 40-hex by the caller, so inlining is safe
		fmt.Fprintf(&b, ` c%d:compare(headRef:"%s"){status}`, i, s)
	}
	b.WriteString(`}}}}`)
	return b.String()
}

// forkRefHolders returns a verdict per SHA for one fork.
func forkRefHolders(ctx context.Context, c *ghapi.Client, fork string, shas []string) map[string]refVerdict {
	out := make(map[string]refVerdict, len(shas))
	owner, name, ok := strings.Cut(fork, "/")
	if !ok {
		for _, s := range shas {
			out[s] = refVerdict{model.Unknown, "bad fork name"}
		}
		return out
	}
	holders := map[string][]string{}
	unknown := map[string]string{}
	var valid []string
	for _, s := range shas {
		if !sha40.MatchString(s) {
			unknown[s] = "not a full SHA"
			continue
		}
		valid = append(valid, s)
	}

	for start := 0; start < len(valid); start += shaChunk {
		chunk := valid[start:min(start+shaChunk, len(valid))]
		for _, prefix := range []string{"refs/heads/", "refs/tags/"} {
			cursor := ""
			for page := 1; ; page++ {
				vars := map[string]any{"o": owner, "n": name}
				if cursor != "" {
					vars["after"] = cursor
				}
				resp, err := c.GraphQL(ctx, refsQuery(prefix, chunk), vars)
				var parsed refQueryResp
				switch {
				case err != nil:
					markAll(unknown, chunk, "error: "+err.Error())
				case resp.Status != 200:
					markAll(unknown, chunk, fmt.Sprintf("HTTP %d", resp.Status))
				case json.Unmarshal(resp.Body, &parsed) != nil:
					markAll(unknown, chunk, "unreadable response")
				case len(parsed.Errors) > 0:
					markAll(unknown, chunk, "GraphQL error")
				case parsed.Data.Repository == nil:
					markAll(unknown, chunk, "repository not available")
				default:
					for _, node := range parsed.Data.Repository.Refs.Nodes {
						var refName string
						_ = json.Unmarshal(node["name"], &refName)
						for i, s := range chunk {
							var cmp *struct{ Status string }
							_ = json.Unmarshal(node[fmt.Sprintf("c%d", i)], &cmp)
							switch {
							case cmp == nil:
								unknown[s] = "compare unavailable for " + prefix + refName
							case cmp.Status == "IDENTICAL" || cmp.Status == "BEHIND":
								holders[s] = append(holders[s], prefix+refName)
							case cmp.Status == "AHEAD" || cmp.Status == "DIVERGED":
							default:
								unknown[s] = "unexpected compare status " + cmp.Status
							}
						}
					}
					if parsed.Data.Repository.Refs.PageInfo.HasNextPage {
						if page >= maxRefPages {
							markAll(unknown, chunk, fmt.Sprintf("more than %d %s refs", maxRefPages*refsPageSize, prefix))
						} else {
							cursor = parsed.Data.Repository.Refs.PageInfo.EndCursor
							continue
						}
					}
				}
				break
			}
		}
	}

	for _, s := range shas {
		switch h := holders[s]; {
		case len(h) > 0: // a proven holder stands even if other pages failed
			ev := strings.Join(h[:min(len(h), maxEvidenceRefs)], ", ")
			if len(h) > maxEvidenceRefs {
				ev += fmt.Sprintf(" (+%d more)", len(h)-maxEvidenceRefs)
			}
			out[s] = refVerdict{model.Confirmed, ev}
		case unknown[s] != "":
			out[s] = refVerdict{model.Unknown, unknown[s]}
		default:
			out[s] = refVerdict{model.NotFound, "no branch or tag in the fork contains it"}
		}
	}
	return out
}

func markAll(m map[string]string, shas []string, why string) {
	for _, s := range shas {
		m[s] = why
	}
}
