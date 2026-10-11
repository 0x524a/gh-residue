package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/0x524a/gh-residue/internal/ghapi"
	"github.com/0x524a/gh-residue/internal/model"
)

// Forks is probe 3: for a capped, prioritised sample of forks, does the old SHA
// resolve under the fork's URL. Whether forks share one object store is still
// unverified (doc 10, item 6), so a hit means "object resolvable", not "fork
// holds a ref". Coverage is always disclosed (ADR-0004).
type Forks struct {
	Client       *ghapi.Client
	Cap          int // forks to probe; 0 means 200
	Concurrency  int // 0 means 4
	MaxListPages int // pages of 100 to list before prioritising; 0 means 10
	Known        *Known
}

func (Forks) Name() string { return "forks" }

func (p Forks) WithKnown(k *Known) Probe { p.Known = k; return p }

type forkInfo struct {
	FullName string `json:"full_name"`
	PushedAt string `json:"pushed_at"`
	Stars    int    `json:"stargazers_count"`
}

func (p Forks) Run(ctx context.Context, repo string, commits []model.OldCommit) Result {
	capN, conc, maxPages := p.Cap, p.Concurrency, p.MaxListPages
	if capN <= 0 {
		capN = 200
	}
	if conc <= 0 {
		conc = 4
	}
	if maxPages <= 0 {
		maxPages = 10
	}

	total, err := p.forksCount(ctx, repo)
	if err != nil {
		return p.failed(commits, "fork count: "+err.Error())
	}
	forks, listErr := p.list(ctx, repo, maxPages)
	if listErr != nil {
		return p.failed(commits, "fork list: "+listErr.Error())
	}
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

	res.ForksTotal, res.ForksChecked = total, len(forks)
	if res.ForksChecked > res.ForksTotal { // forks_count lags; never report >100%
		res.ForksTotal = res.ForksChecked
	}
	res.Surfaces = []model.SurfaceCoverage{{
		Surface:  model.SurfaceForkObject,
		Complete: complete(res.Findings, model.SurfaceForkObject) && res.ForksChecked >= res.ForksTotal,
	}}
	return res
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

func (p Forks) list(ctx context.Context, repo string, maxPages int) ([]forkInfo, error) {
	var all []forkInfo
	for page := 1; page <= maxPages; page++ {
		r, err := p.Client.Get(ctx, fmt.Sprintf("/repos/%s/forks?per_page=100&page=%d", repo, page))
		if err != nil {
			return nil, err
		}
		if r.Status != 200 {
			return nil, fmt.Errorf("HTTP %d", r.Status)
		}
		var batch []forkInfo
		if err := json.Unmarshal(r.Body, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	return all, nil
}

func (p Forks) failed(commits []model.OldCommit, ev string) Result {
	res := Result{Failed: true}
	for _, c := range commits {
		res.Findings = append(res.Findings, finding(c.SHA, model.SurfaceForkObject, "*", model.Unknown, ev))
	}
	res.Surfaces = []model.SurfaceCoverage{{Surface: model.SurfaceForkObject, Complete: false}}
	return res
}
