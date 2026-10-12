# ADR-0003: `git-filter-repo` commit-map as primary input

**Status:** accepted (design stage)

## Context

The tool needs the old SHAs. They cannot be discovered reliably after the fact without the rewrite input. `git-filter-repo` writes `.git/filter-repo/commit-map` (old to new SHA, sorted by old SHA), `changed-refs` (not read by the tool yet) and `first-changed-commits`, and GitHub's own guide uses `changed-refs` to count affected PRs.

## Decision

Primary input is a `filter-repo` working directory; fallback is a plain list of full 40-character SHAs (BFG, manual rewrites, `push --mirror` logs). Short SHAs are rejected.

## Consequences

- Near zero configuration for the common case.
- Other rewrite tools need the SHA list.
- Automatic discovery of old SHAs is not a v1 feature. Update 2026-10-12: the Events API does record force-pushes (with 30 s to 6 h latency, 300 events and 30 days at most; doc 10 item 15), so it is a viable candidate alongside the GraphQL PR-timeline adapter (doc 05). Neither finds rewrites of branches that never had a PR unless the push event is still inside the window.
