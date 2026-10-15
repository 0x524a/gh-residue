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
	// AcceptHidden lets the "Ready for Support?" line ignore forks GitHub hides. They
	// stay unknown in every finding and the exit code is unchanged.
	AcceptHidden bool
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
	total, source, err := p.repoInfo(ctx, repo)
	if err != nil {
		return p.failed(commits, "fork count: "+err.Error())
	}
	forks, listComplete, gaps, listErr := p.collect(ctx, repo, maxPages)
	if listErr != nil {
		return p.failed(commits, "fork list: "+listErr.Error())
	}
	reason := incompleteReason(listComplete, maxPages, source, len(gaps))
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
				snapshot(ctx, res.Findings)
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

	hiddenNames := p.markHidden(ctx, &res)
	hidden := len(hiddenNames)
	if p.Refs {
		p.checkRefs(ctx, &res, repo, forks, commits, conc)
	}

	// forks_count is a cached counter that undercounts (newren/git-filter-repo
	// reports 1014 for 1056 real forks), so it is only a display figure and a
	// lower bound. Completeness comes from the listing itself.
	listed := len(allForks)
	res.ForksTotal, res.ForksChecked = max(total, listed), len(forks)
	res.ForksIncompleteReason = reason
	res.ForkCommitsSkipped = len(all) - len(commits)
	res.ForksHidden = hidden
	res.HiddenForks = hiddenNames
	res.HiddenAccepted = p.AcceptHidden && hidden > 0
	skip := map[string]bool{}
	acceptedReason := reason
	if res.HiddenAccepted {
		for _, h := range hiddenNames {
			skip[h] = true
		}
		// Forks below a hidden fork cannot be listed either; they are covered by the
		// same acceptance.
		open := 0
		for _, g := range gaps {
			if !skip[g] {
				open++
			}
		}
		acceptedReason = incompleteReason(listComplete, maxPages, source, open)
		res.ForksReasonAccepted = acceptedReason
	}
	forksDone := res.ForksChecked >= res.ForksTotal && acceptedReason == ""
	res.Surfaces = []model.SurfaceCoverage{{
		Surface:  model.SurfaceForkObject,
		Complete: completeExcept(res.Findings, model.SurfaceForkObject, skip) && forksDone,
	}}
	if p.Refs {
		res.Surfaces = append(res.Surfaces, model.SurfaceCoverage{
			Surface:  model.SurfaceForkRef,
			Complete: completeExcept(res.Findings, model.SurfaceForkRef, skip) && forksDone,
		})
	}
	return res
}

// markHidden finds listed forks that GitHub hides: the repository answers 404 for
// every commit and its owner's account answers 404 too, while other forks answer
// normally (so it is not a broken token). They stay Unknown: a 404 cannot be told
// from missing access, and a hidden repository may still hold refs that only
// GitHub Support can see. The count is reported so the reader can tell "hidden by
// GitHub" from "failed to check".
func (p Forks) markHidden(ctx context.Context, res *Result) []string {
	all404 := map[string]bool{}
	reachable := false
	for _, f := range res.Findings {
		if f.Surface != model.SurfaceForkObject {
			continue
		}
		if f.Status == model.Confirmed || f.Status == model.NotFound {
			reachable = true
		}
		if _, seen := all404[f.Location]; !seen {
			all404[f.Location] = true
		}
		if f.Status != model.Unknown || f.Evidence != "HTTP 404" {
			all404[f.Location] = false
		}
	}
	if !reachable {
		return nil
	}
	hidden := map[string]bool{}
	for fork, ok := range all404 {
		if !ok {
			continue
		}
		owner, _, _ := strings.Cut(fork, "/")
		r, err := p.Client.Get(ctx, "/users/"+owner)
		if err == nil && r.Status == 404 {
			hidden[fork] = true
		}
	}
	for i := range res.Findings {
		f := &res.Findings[i]
		if f.Surface == model.SurfaceForkObject && hidden[f.Location] {
			f.Evidence = "HTTP 404; the fork and its owner account are hidden from the API"
		}
	}
	names := make([]string, 0, len(hidden))
	for h := range hidden {
		names = append(names, h)
	}
	sort.Strings(names)
	return names
}

// checkRefs adds one fork_ref finding per (fork, SHA). The object check decides:
// a SHA the fork cannot resolve cannot be held by any of its refs (not_found); an
// unknown object check stays unknown; only resolvable pairs cost a GraphQL query.
func (p Forks) checkRefs(ctx context.Context, res *Result, repo string, forks []forkInfo, commits []model.OldCommit, conc int) {
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
	add := func(f model.Finding) {
		mu.Lock()
		res.Findings = append(res.Findings, f)
		snapshot(ctx, res.Findings)
		mu.Unlock()
	}
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
				for sha, v := range forkRefHolders(ctx, p.Client, repo, w.fork, w.resolved) {
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

// repoInfo returns the repository's cached fork count and, when the repository is
// itself a fork, the full name of the network's source (else "").
func (p Forks) repoInfo(ctx context.Context, repo string) (int, string, error) {
	r, err := p.Client.Get(ctx, "/repos/"+repo)
	if err != nil {
		return 0, "", err
	}
	if r.Status != 200 {
		return 0, "", fmt.Errorf("HTTP %d", r.Status)
	}
	var v struct {
		ForksCount int  `json:"forks_count"`
		Fork       bool `json:"fork"`
		Parent     *struct {
			FullName string `json:"full_name"`
		} `json:"parent"`
		Source *struct {
			FullName string `json:"full_name"`
		} `json:"source"`
	}
	if err := json.Unmarshal(r.Body, &v); err != nil {
		return 0, "", err
	}
	source := ""
	switch {
	case v.Source != nil:
		source = v.Source.FullName
	case v.Parent != nil:
		source = v.Parent.FullName
	case v.Fork:
		source = "its parent repository"
	}
	return v.ForksCount, source, nil
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

// Forks of forks are listed breadth-first, bounded in depth and in listing calls.
const (
	maxForkDepth   = 3
	maxNestedLists = 100
)

// collect lists the repository's forks and, below them, forks of forks. gaps counts
// the forks whose own forks could not be listed (the listing failed, for example
// because the fork is hidden, or the depth or call budget ran out): their children
// stay unchecked, so the coverage is incomplete. complete is false if any listing
// was cut short. A failure to list the repository's own forks is returned as an error.
func (p Forks) collect(ctx context.Context, repo string, maxPages int) (all []forkInfo, complete bool, gaps []string, err error) {
	top, complete, err := p.list(ctx, repo, maxPages)
	if err != nil {
		return nil, false, nil, err
	}
	seen := map[string]bool{strings.ToLower(repo): true}
	type node struct {
		f     forkInfo
		depth int
	}
	var queue []node
	add := func(fs []forkInfo, depth int) {
		for _, f := range fs {
			if k := strings.ToLower(f.FullName); !seen[k] {
				seen[k] = true
				all = append(all, f)
				queue = append(queue, node{f, depth})
			}
		}
	}
	add(top, 1)
	lists := 0
	for i := 0; i < len(queue); i++ {
		n := queue[i]
		if n.f.ForksCount == 0 {
			continue
		}
		if n.depth >= maxForkDepth || lists >= maxNestedLists {
			gaps = append(gaps, n.f.FullName)
			continue
		}
		lists++
		kids, kidsComplete, kerr := p.list(ctx, n.f.FullName, maxPages)
		if kerr != nil {
			gaps = append(gaps, n.f.FullName)
			continue
		}
		complete = complete && kidsComplete
		add(kids, n.depth+1)
	}
	return all, complete, gaps, nil
}

// incompleteReason says why the listed forks cannot be treated as every fork.
func incompleteReason(listComplete bool, maxPages int, source string, gaps int) string {
	var why []string
	if source != "" {
		// Only the repository's own forks are listed. The rest of its network (the
		// source, its other forks) shares Git data with it but is never enumerated.
		why = append(why, fmt.Sprintf("the repository is itself a fork of %s, and the rest of that network is not checked", source))
	}
	if !listComplete {
		why = append(why, fmt.Sprintf("only the first %d forks GitHub lists were seen", maxPages*100))
	}
	if gaps > 0 {
		why = append(why, fmt.Sprintf("%d listed forks have forks of their own that could not be listed", gaps))
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
