# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## State of the repo

M1 (single-repo CLI) is implemented in Go (stdlib only, `go 1.23`; the floor is 1.23 because a cgo-linked Go 1.22 binary aborts on macOS 26 with `missing LC_UUID`, found in CI). M0 was accepted with fact 3 (shared fork object store) deferred. M2 is done (golden files for table, JSON, Support draft and diff live in `internal/report/testdata/`; HTTP fixtures are still inline `httptest` servers): Support draft (`report/support.go`), diff mode (`report/diff.go`), checkpoint/resume (`probe/known.go`, `app.merge`, `cmd` `loadCheckpoint`). Not built yet: org mode (M4), web-view probe 5.

Packaging (M3): remote `0x524a/gh-residue` (public, Apache-2.0). Commit as `0x524a` using the repo-local identity (GitHub noreply email), never the global git identity. Release by bumping `baseVersion` in `cmd/gh-residue/main.go` and pushing a matching `v*` tag; tags containing `-` are prereleases. `dist/` must stay in `.gitignore` or builds report `-dirty`.

README banner: source is `docs/assets/banner.html` (wide `#banner`, stacked `#banner-mobile`); the four PNGs are rendered from it with Playwright (serve the folder, emulate `prefers-color-scheme` light/dark, element-screenshot at device scale). Re-render after editing it; the README's `<picture>` picks by colour scheme and width (600px), verified on github.com. No GitHub or Octocat logo is used. The owner reports registering for the GitHub Developer Program on 2026-10-11 (acceptance not verified here); add a logo only if the owner asks, and follow github.com/logos guidelines, which have not been reviewed.

## Commands

```bash
go build -o gh-residue ./cmd/gh-residue
go test ./...                                   # no network; mirror tests use real `git` on local bare repos
go test ./internal/probe -run TestMirrorProbe   # single test
go test ./internal/report -update               # regenerate golden files; then READ the diff before keeping it
go vet ./... && gofmt -l .
./gh-residue verify OWNER/REPO --input <filter-repo dir | sha-list file> [--json] [--max-forks N] [--concurrency N] [--no-forks] [--no-mirror] [--yes]
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
- The checkpoint is also written during a run (`app.VerifyWithSave`, `probe.Snapshot` every 50 findings); a partial run must keep results carried from the previous checkpoint. Resume (`--checkpoint`) carries only confirmed/not_found results, never unknown, and `app.merge` drops carried rows superseded by fresh ones: per (SHA, surface) for enumerated ref surfaces, per exact key otherwise. A new probe that supports skipping implements `probe.Resumable`. Staleness is judged on each carried result's `checked_at`, not the checkpoint's `started_at`.
- Diff mode never calls something `resolved` unless the new run fully enumerated that ref surface; absent forks are `now-unknown`.
- Probes are independent; one failing never discards others.
- Probe status mapping lives in `internal/probe/commit.go` (REST: 200 confirmed, 422 "No commit found" not_found, everything else incl. 404 unknown) and `mirror.go` (absent from a *successful* clone is not_found; clone failure is unknown). Any change there needs the matching property test.
- Only SHAs, ref names, repo names and PR numbers are stored, never file contents/diffs/secrets. Output ordering is deterministic so runs diff cleanly. All JSON carries `schema_version`.
- Fork coverage is sampled (default cap 200) and always disclosed (ADR-0004); fork *object resolvable* is reported separately from *ref-holding* (`--fork-refs`, `probe/forkrefs.go`, GraphQL `Ref.compare`, never `ls-remote`: tips only would turn a buried commit into a false not_found), and the latter drives the "Ready for Support?" line. `yes` needs the flag, full fork coverage and no unknowns. A fork's own `refs/pull/*` are checked over REST (`probe/forkpulls.go`: matching-refs, then compare through the user's repo); forks that GitHub hides (repo and owner 404) stay unknown and are counted (`ForksHidden`); a repo that is itself a fork is incomplete; forks of forks are listed (3 levels, 100 calls) and only unlistable ones are a gap. `--accept-hidden-forks` changes only `Readiness` (via `Run.IncompleteForSupport`), never findings or `ExitCode`. Fork checks narrow to filter-repo's first changed commits only when read from its own file; never from `--first-changed` or a SHA list.
- Web-view probe (probe 5) is opt-in (ADR-0005); the tool downloads content only from the user's own repo, never forks.
- Support draft is never auto-submitted; the "why rotation can't mitigate" section is left blank for the user; fork owner usernames omitted by default.

## Doc conventions

- Claims not confirmed against official GitHub docs or an experiment are tagged **(unverified)** and tracked in `docs/10-open-questions.md`. Keep that list in sync when adding or resolving a claim; remove the tag only with evidence (cite the doc or `docs/spike/gate0-results.md`).
- Docs are numbered 01–10; the README's doc map must stay consistent with them. Decisions go in `docs/adr/` as new numbered ADRs.
- Gate 0 status: facts 1 (dangling SHA resolvable via REST commit endpoint) and 2 (`git clone --mirror` carries `refs/pull/*`) confirmed; fact 3 (shared fork object store) confirmed 2026-10-12 from GitHub's docs plus a live check on public forks; private and detached forks untested.
