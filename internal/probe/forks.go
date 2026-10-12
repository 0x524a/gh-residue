package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/0x524a/gh-residue/internal/ghapi"
	"github.com/0x524a/gh-residue/internal/model"
)

// Forks is probe 3: for a capped, prioritised sample of forks, does the old SHA
// resolve under the fork's URL. Forks in a network share Git data (GitHub's docs,
// "About permissions and visibility of forks"), so a hit means "object
// resolvable", not "fork holds a ref"; Refs adds that second check. Coverage is
// always disclosed (ADR-0004).
type Forks struct {
	Client       *ghapi.Client
	Cap          int // forks to probe; 0 means 200
	Concurrency  int // 0 means 1: GitHub advises serial requests (secondary rate limits)
	MaxListPages int // pages of 100 to list before prioritising; 0 means 10
	Known        *Known
	// Refs also asks, for each fork that can resolve an old SHA, whether any branch
	// or tag in it contains the SHA. Needs a token (GraphQL). Opt-in.
	Refs bool
	// Anchors are filter-repo's first changed commits. Every rewritten commit has
	// one as an ancestor, and a ref that contains a commit contains all its
	// ancestors, so a fork that holds any rewritten commit also holds an anchor.
	// When set, fork checks run for the anchors only and the other commits are
	// not requested per fork (forks x commits would be millions of calls).
	Anchors []string
}

func (Forks) Name() string { return "forks" }

func (p Forks) WithKnown(k *Known) Probe { p.Known = k; return p }

type forkInfo struct {
	FullName   string `json:"full_name"`
	PushedAt   string `json:"pushed_at"`
	Stars      int    `json:"stargazers_count"`
	ForksCount int    `json:"forks_count"` // this fork's own forks (cached by GitHub)
}

func (p Forks) Run(ctx context.Context, repo string, commits []model.OldCommit) Result {
	capN, conc, maxPages := p.Cap, p.Concurrency, p.MaxListPages
	if capN <= 0 {
		capN = 200
	}
	if conc <= 0 {
		conc = 1
	}
	if maxPages <= 0 {
		maxPages = 10
	}

	all := commits
	commits = p.forkScope(commits)
	total, err := p.forksCount(ctx, repo)
	if err != nil {
		return p.failed(commits, "fork count: "+err.Error())
	}
	forks, listComplete, listErr := p.list(ctx, repo, maxPages)
	if listErr != nil {
		return p.failed(commits, "fork list: "+listErr.Error())
	}
	reason := incompleteReason(forks, listComplete, maxPages)
	allForks := forks
	sort.SliceStable(forks, func(i, j int) bool {
		if forks[i].PushedAt != forks[j].PushedAt {
			return forks[i].PushedAt > forks[j].PushedAt
		}
		return forks[i].Stars > forks[j].Stars
	})
	if len(forks) > capN {
		forks = forks[:capN]
	}

	type job struct {
		fork string
		sha  string
	}
	jobs := make(chan job)
	var mu sync.Mutex
	var res Result
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				st, ev := checkCommit(ctx, p.Client, j.fork, j.sha)
				f := finding(j.sha, model.SurfaceForkObject, j.fork, st, ev)
				mu.Lock()
				res.Findings = append(res.Findings, f)
				mu.Unlock()
			}
		}()
	}
	for _, f := range forks {
		for _, c := range commits {
			if p.Known.Has(c.SHA, model.SurfaceForkObject, f.FullName) {
				continue
			}
			jobs <- job{f.FullName, c.SHA}
		}
	}
	close(jobs)
	wg.Wait()

	if p.Refs {
		p.checkRefs(ctx, &res, forks, commits, conc)
	}

	// forks_count is a cached counter that undercounts (newren/git-filter-repo
	// reports 1014 for 1056 real forks), so it is only a display figure and a
	// lower bound. Completeness comes from the listing itself.
	listed := len(allForks)
	res.ForksTotal, res.ForksChecked = max(total, listed), len(forks)
	res.ForksIncompleteReason = reason
	res.ForkCommitsSkipped = len(all) - len(commits)
	forksDone := res.ForksChecked >= res.ForksTotal && reason == ""
	res.Surfaces = []model.SurfaceCoverage{{
		Surface:  model.SurfaceForkObject,
		Complete: complete(res.Findings, model.SurfaceForkObject) && forksDone,
	}}
	if p.Refs {
		res.Surfaces = append(res.Surfaces, model.SurfaceCoverage{
			Surface:  model.SurfaceForkRef,
			Complete: complete(res.Findings, model.SurfaceForkRef) && forksDone,
		})
	}
	return res
}

// checkRefs adds one fork_ref finding per (fork, SHA). The object check decides:
// a SHA the fork cannot resolve cannot be held by any of its refs (not_found); an
// unknown object check stays unknown; only resolvable pairs cost a GraphQL query.
func (p Forks) checkRefs(ctx context.Context, res *Result, forks []forkInfo, commits []model.OldCommit, conc int) {
	obj := map[string]map[string]model.Status{} // fork -> sha -> object status
	for _, f := range res.Findings {
		if f.Surface != model.SurfaceForkObject {
			continue
		}
		if obj[f.Location] == nil {
			obj[f.Location] = map[string]model.Status{}
		}
		obj[f.Location][f.OldSHA] = f.Status
	}
	type work struct {
		fork     string
		resolved []string
	}
	var mu sync.Mutex
	add := func(f model.Finding) { mu.Lock(); res.Findings = append(res.Findings, f); mu.Unlock() }
	var todo []work
	for _, f := range forks {
		w := work{fork: f.FullName}
		for _, c := range commits {
			if p.Known.Has(c.SHA, model.SurfaceForkRef, f.FullName) {
				continue
			}
			st, ok := obj[f.FullName][c.SHA]
			if !ok {
				st, ok = p.Known.Status(c.SHA, model.SurfaceForkObject, f.FullName)
			}
			switch {
			case ok && st == model.NotFound:
				add(finding(c.SHA, model.SurfaceForkRef, f.FullName, model.NotFound, "not resolvable through this fork, so no ref can hold it"))
			case ok && st == model.Confirmed:
				w.resolved = append(w.resolved, c.SHA)
			default:
				add(finding(c.SHA, model.SurfaceForkRef, f.FullName, model.Unknown, "object check for this fork did not complete"))
			}
		}
		if len(w.resolved) > 0 {
			todo = append(todo, w)
		}
	}
	jobs := make(chan work)
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range jobs {
				for sha, v := range forkRefHolders(ctx, p.Client, w.fork, w.resolved) {
					add(finding(sha, model.SurfaceForkRef, w.fork, v.Status, v.Evidence))
				}
			}
		}()
	}
	for _, w := range todo {
		jobs <- w
	}
	close(jobs)
	wg.Wait()
}

// forkScope narrows the commits to check on forks to the anchors, but only when
// every anchor is one of the commits; otherwise the inference is unsafe and all
// commits are checked.
func (p Forks) forkScope(commits []model.OldCommit) []model.OldCommit {
	if len(p.Anchors) == 0 {
		return commits
	}
	want := map[string]bool{}
	for _, a := range p.Anchors {
		want[a] = true
	}
	var out []model.OldCommit
	for _, c := range commits {
		if want[c.SHA] {
			out = append(out, c)
		}
	}
	if len(out) != len(want) {
		return commits
	}
	return out
}

func (p Forks) forksCount(ctx context.Context, repo string) (int, error) {
	r, err := p.Client.Get(ctx, "/repos/"+repo)
	if err != nil {
		return 0, err
	}
	if r.Status != 200 {
		return 0, fmt.Errorf("HTTP %d", r.Status)
	}
	var v struct {
		ForksCount int `json:"forks_count"`
	}
	if err := json.Unmarshal(r.Body, &v); err != nil {
		return 0, err
	}
	return v.ForksCount, nil
}

// list returns the distinct forks GitHub lists, and whether the listing reached
// its end (a short page). sort=oldest keeps pages stable while new forks appear;
// duplicates from forks deleted between pages are dropped by name.
func (p Forks) list(ctx context.Context, repo string, maxPages int) ([]forkInfo, bool, error) {
	var all []forkInfo
	seen := map[string]bool{}
	for page := 1; page <= maxPages; page++ {
		r, err := p.Client.Get(ctx, fmt.Sprintf("/repos/%s/forks?per_page=100&sort=oldest&page=%d", repo, page))
		if err != nil {
			return nil, false, err
		}
		if r.Status != 200 {
			return nil, false, fmt.Errorf("HTTP %d", r.Status)
		}
		var batch []forkInfo
		if err := json.Unmarshal(r.Body, &batch); err != nil {
			return nil, false, err
		}
		for _, f := range batch {
			if !seen[f.FullName] {
				seen[f.FullName] = true
				all = append(all, f)
			}
		}
		if len(batch) < 100 {
			return all, true, nil
		}
	}
	return all, false, nil // last page was full: there may be more
}

// incompleteReason says why the listed forks cannot be treated as every fork.
func incompleteReason(forks []forkInfo, listComplete bool, maxPages int) string {
	var why []string
	if !listComplete {
		why = append(why, fmt.Sprintf("only the first %d forks GitHub lists were seen", maxPages*100))
	}
	nested := 0
	for _, f := range forks {
		if f.ForksCount > 0 {
			nested++
		}
	}
	if nested > 0 {
		why = append(why, fmt.Sprintf("%d listed forks have forks of their own, which are not checked", nested))
	}
	return strings.Join(why, "; ")
}

func (p Forks) failed(commits []model.OldCommit, ev string) Result {
	res := Result{Failed: true}
	for _, c := range commits {
		res.Findings = append(res.Findings, finding(c.SHA, model.SurfaceForkObject, "*", model.Unknown, ev))
	}
	res.Surfaces = []model.SurfaceCoverage{{Surface: model.SurfaceForkObject, Complete: false}}
	if p.Refs {
		for _, c := range commits {
			res.Findings = append(res.Findings, finding(c.SHA, model.SurfaceForkRef, "*", model.Unknown, ev))
		}
		res.Surfaces = append(res.Surfaces, model.SurfaceCoverage{Surface: model.SurfaceForkRef, Complete: false})
	}
	return res
}
