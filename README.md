# gh-residue

> **Status: M1 (single-repo CLI) implemented 2026-10-10; M2 (Support draft, diff, resume) not started.** Everything here is a proposal; the gating spike in [`docs/08-testing-strategy.md`](docs/08-testing-strategy.md) is two-thirds done.

## Where this stands (2026-10-10)

- Spec complete and consistency-reviewed: docs 01 to 10, ADRs 0001 to 0005.
- Gate 0 spike: facts 1 and 2 confirmed, public-repo run done ([`docs/spike/gate0-results.md`](docs/spike/gate0-results.md)). Fact 3 (shared fork object store) untested: no second account.
- Doc-only questions 1 to 3 closed against GitHub's official docs; 13 questions remain open ([`docs/10-open-questions.md`](docs/10-open-questions.md)).
- **Decision pending (owner):** accept M0 with fact 3 deferred, or close it first via a GitHub machine account (allowed by the ToS). Proposed: accept and defer ([`docs/09-roadmap.md`](docs/09-roadmap.md)).
- **M0 decision:** accepted with fact 3 deferred; M1 built. Next: M2.
- **Loose end:** the private throwaway repo `0x524a/residue-spike-1791666833` still exists (kept for question 13, merge-ref lifetime). Remove with `gh repo delete 0x524a/residue-spike-1791666833 --yes`.

A read-only `gh` extension that answers one question after you rewrite git history to remove a leaked secret:

**Where are the old commits still reachable on GitHub, and what do I send GitHub Support?**

## Install and use

```bash
gh extension install 0x524a/gh-residue            # latest stable release
gh extension install 0x524a/gh-residue --pin v0.1.0-rc.2   # a prerelease must be pinned
gh residue --version
```

```bash
# After: git filter-repo ... && git push --force
gh residue verify OWNER/REPO --input /path/to/rewritten/clone --support-draft draft.md --checkpoint cp.json
```

- `--input` is a `git-filter-repo` working directory, or a file with one full 40-character SHA per line.
- Exit code `0` nothing exposed, `1` something is still exposed, `2` incomplete (an error, rate limit, sampled forks or a skipped probe). **Treat 2 as not clean.**
- Re-run with the same `--checkpoint` file to re-check only what came back `unknown`. Use `--against previous.json` to see what Support resolved.
- Token: `GH_TOKEN`, `GITHUB_TOKEN`, or your `gh` login. Read-only is enough (fine-grained Contents:read and Metadata:read, or classic `repo` for private repos). Public repos work without one at 60 requests/hour.
- Reports, checkpoints and drafts list the SHAs of a leak. Do not commit them to a public repo.

Unaffiliated with GitHub. The `gh-` prefix is the naming convention for CLI extensions, not an endorsement.

## The gap

Rewriting history and force-pushing does not make the old commits unreachable on GitHub. GitHub's own documentation ("Removing sensitive data from a repository") says the commits may remain accessible in clones and forks, directly by SHA in cached views, and through pull requests that reference them. Only GitHub Support can remove cached views and PR references.

Existing popular tools are on the finding side (for example `trufflesecurity/force-push-scanner`, `neodyme-labs/github-secrets`: scan for secrets in dangling commits). None found that confirms, after a rewrite, that the exposure is closed. See [`docs/01-problem-and-scope.md`](docs/01-problem-and-scope.md).

## What it would do

1. Take the old SHAs, ideally from `git-filter-repo`'s `commit-map`.
2. Probe every surface where they can still be reachable (base repo, `refs/pull/*`, forks, tags).
3. Print a report and stable JSON; draft the Support request; re-run later to confirm closure.

## What it will never do

- Purge anything (only GitHub Support can).
- Detect, rotate or revoke secrets. **Rotate the credential first.**
- Rewrite history.
- Show commit contents, or claim "clean" when a probe failed (see [ADR-0002](docs/adr/0002-unknown-never-counts-as-clean.md)).

## Doc map

| Doc | Topic |
|---|---|
| [01](docs/01-problem-and-scope.md) | Problem, users, scope, non-goals |
| [02](docs/02-architecture.md) | Three-layer architecture |
| [03](docs/03-probes.md) | Each probe, what it proves, caveats |
| [04](docs/04-output-and-data-model.md) | Data model, JSON, exit codes, Support draft, diff |
| [05](docs/05-auth-rate-limits-org-mode.md) | Auth, rate limits, org mode |
| [06](docs/06-threat-model-and-safety.md) | Threat model and safety defaults |
| [07](docs/07-legal-and-licensing.md) | License, trademark, terms |
| [08](docs/08-testing-strategy.md) | Gating spike and test plan |
| [09](docs/09-roadmap.md) | Milestones |
| [10](docs/10-open-questions.md) | Every unverified claim |
| [adr/](docs/adr/) | Decision records |

## Conventions

Claims not yet confirmed against official docs or an experiment are tagged **(unverified)**. They are collected in `docs/10-open-questions.md`.
