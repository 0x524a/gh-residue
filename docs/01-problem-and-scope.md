# 01. Problem and scope

## Problem

After a secret leaks into a GitHub repo, the usual response is: rotate the credential, rewrite history (`git-filter-repo`, BFG), force-push. GitHub's documentation states that this is not enough for exposure on GitHub itself:

- commits stay accessible in clones and forks;
- commits stay accessible by SHA in cached views;
- pull requests that reference them keep references (`refs/pull/*`).

Cached views and PR references can only be removed by GitHub Support, and per the same page Support only assists where the risk cannot be mitigated by rotating credentials. (Source: docs.github.com, "Removing sensitive data from a repository", fetched 2026-10.)

Today the "did it work?" step is manual `grep`, ad hoc `curl` and guesswork.

## Users

Primary: both of the following, single-repo first.

1. A maintainer of one repo, mid-incident, at a terminal. (v1)
2. A security or platform engineer running this across many repos from a manifest. (v1.1)

## Scope (v1)

- One repo per run. Core is a library so org mode is a loop (see doc 05).
- Inputs: a `git-filter-repo` working directory (`commit-map` and `first-changed-commits`), or a plain list of old SHAs.
- Outputs: human table, stable JSON, Support-request draft, diff against an earlier run.
- Read-only. GitHub only.

## Non-goals

- No purging. No secret detection, rotation or revocation. No history rewriting.
- No GitLab or Bitbucket: the model depends on GitHub's shared fork object store and `refs/pull`.
- No reach into other people's local clones, CI caches or third-party mirrors; these are listed as a checklist.
- Never stores or prints secret values or commit contents.

## Success criteria

After a rewrite, one command lists every reachable surface for each old SHA, with honest coverage; a Support draft is ready; a re-run shows `resolved` after Support acts; and no failure mode prints a false all-clear.

## Prior art (kill-check, 2026-10, GitHub repo search; incomplete)

| Repo | Stars | Role |
|---|---|---|
| trufflesecurity/force-push-scanner | 491 | finds secrets in dangling commits via GH Archive |
| neodyme-labs/github-secrets | 185 | same, per repo |
| RichardoC/gitlab-secrets | 47 | GitLab equivalent |
| bthuilot/git-lost-and-found | 1 | orphaned/dangling commits |
| Zxela/git-history-rewrite | 0 | path removal with verification |
| nikolareljin/leak-lock | 1 | VS Code extension, history secrets and live credential check |

All are scanners or rewriters; none found that does post-rewrite reachability verification plus Support evidence. Search was keyword-based; commercial products were not surveyed **(unverified)**.
