# ADR-0003: `git-filter-repo` commit-map as primary input

**Status:** accepted (design stage)

## Context

The tool needs the old SHAs. They cannot be discovered reliably after the fact without the rewrite input. `git-filter-repo` writes `.git/filter-repo/commit-map` (old to new SHA) and `changed-refs`, and GitHub's own guide uses `changed-refs` to count affected PRs.

## Decision

Primary input is a `filter-repo` working directory; fallback is a plain list of full 40-character SHAs (BFG, manual rewrites, `push --mirror` logs). Short SHAs are rejected.

## Consequences

- Near zero configuration for the common case.
- Other rewrite tools need the SHA list.
- Automatic discovery via the Events API is not a v1 feature and looks unreliable (Gate 0 run 2 recorded no force-push events). A GraphQL PR-timeline adapter is the more promising candidate (doc 05), limited to PR branches.
