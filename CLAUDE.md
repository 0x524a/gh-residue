# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## State of the repo

M1 (single-repo CLI) is implemented in Go (stdlib only, `go 1.22`). M0 was accepted with fact 3 (shared fork object store) deferred. M2 is done (golden files for table, JSON, Support draft and diff live in `internal/report/testdata/`; HTTP fixtures are still inline `httptest` servers): Support draft (`report/support.go`), diff mode (`report/diff.go`), checkpoint/resume (`probe/known.go`, `app.merge`, `cmd` `loadCheckpoint`). Not built yet: `gh` extension packaging (M3), org mode (M4), web-view probe 5, ref-holding fork probe.

## Commands

```bash
go build -o gh-residue ./cmd/gh-residue
go test ./...                                   # no network; mirror tests use real `git` on local bare repos
go test ./internal/probe -run TestMirrorProbe   # single test
go test ./internal/report -update               # regenerate golden files; then READ the diff before keeping it
go vet ./... && gofmt -l .
./gh-residue verify OWNER/REPO --input <filter-repo dir | sha-list file> [--json] [--max-forks N] [--no-forks] [--no-mirror] [--yes]
         [--support-draft FILE] [--first-changed SHA,...] [--lfs-file NAME] [--against PREV.json]
         [--checkpoint FILE] [--checkpoint-max-age 24h]
```

Layout: `cmd/gh-residue` (CLI, exit codes), `internal/{input,probe,report,app,model,ghapi}`. Token comes from `GH_TOKEN`/`GITHUB_TOKEN`/`gh auth token`; the mirror clone passes it via a `GIT_ASKPASS` helper, never on a command line.

## What the project is

`gh-residue`: a read-only `gh` extension (planned Go CLI) that, after a history rewrite to remove a leaked secret, reports where the old commits are still reachable on GitHub (base repo, `refs/pull/*`, forks, tags, cached web view) and drafts a GitHub Support request. It never purges, rotates/detects secrets, rewrites history, or shows commit contents.

## Architecture (docs/02, 03, 04)

Three layers over small typed interfaces: input adapters (`filter-repo` `commit-map` dir, or a file of full 40-char SHAs; short SHAs rejected) → independent probes (`Probe.Run(ctx, repo, shas) -> []Finding`) → pure, network-free reporters (table, JSON, Support draft, diff). Org mode is a thin batch loop over the same library.

Invariants that shape every doc and future code:
- **Unknown is never clean (ADR-0002).** Every finding is `confirmed | not_found | unknown`; any error, timeout, rate limit, truncation or missing permission yields `unknown`. The rule lives in the status type, not in each probe. Exit codes: 0 nothing exposed, 1 confirmed exposure, 2 incomplete (treat as not clean). Enforced by `TestCheckCommitNeverNotFoundOnErrors` and the mirror clone-failure test: no error path may produce `not_found`.
- Anything a run did not check must read as incomplete: disabled probes register via `probe.Skipped`, sampled forks and failed probes via coverage. `Run.Incomplete()` is the single definition; `ExitCode()` and `report.Readiness` both use it, and a confirmed exposure must never mask incompleteness.
- Resume (`--checkpoint`) carries only confirmed/not_found results, never unknown, and `app.merge` drops carried rows superseded by fresh ones: per (SHA, surface) for enumerated ref surfaces, per exact key otherwise. A new probe that supports skipping implements `probe.Resumable`. Staleness is judged on each carried result's `checked_at`, not the checkpoint's `started_at`.
- Diff mode never calls something `resolved` unless the new run fully enumerated that ref surface; absent forks are `now-unknown`.
- Probes are independent; one failing never discards others.
- Probe status mapping lives in `internal/probe/commit.go` (REST: 200 confirmed, 422 "No commit found" not_found, everything else incl. 404 unknown) and `mirror.go` (absent from a *successful* clone is not_found; clone failure is unknown). Any change there needs the matching property test.
- Only SHAs, ref names, repo names and PR numbers are stored, never file contents/diffs/secrets. Output ordering is deterministic so runs diff cleanly. All JSON carries `schema_version`.
- Fork coverage is sampled (default cap 200) and always disclosed (ADR-0004); fork *object resolvable* is reported separately from *ref-holding*, and the latter drives the "Ready for Support?" line.
- Web-view probe (probe 5) is opt-in (ADR-0005); the tool downloads content only from the user's own repo, never forks.
- Support draft is never auto-submitted; the "why rotation can't mitigate" section is left blank for the user; fork owner usernames omitted by default.

## Doc conventions

- Claims not confirmed against official GitHub docs or an experiment are tagged **(unverified)** and tracked in `docs/10-open-questions.md`. Keep that list in sync when adding or resolving a claim; remove the tag only with evidence (cite the doc or `docs/spike/gate0-results.md`).
- Docs are numbered 01–10; the README's doc map must stay consistent with them. Decisions go in `docs/adr/` as new numbered ADRs.
- Gate 0 status: facts 1 (dangling SHA resolvable via REST commit endpoint) and 2 (`git clone --mirror` carries `refs/pull/*`) confirmed; fact 3 (shared fork object store) untested.
- Loose end: private throwaway repo `0x524a/residue-spike-1791666833` still exists (kept for open question 13); delete with `gh repo delete 0x524a/residue-spike-1791666833 --yes` only when the owner says so.
