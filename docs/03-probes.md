# 03. Probes

Every probe answers: is old SHA X still reachable via surface Y? Any error, timeout or rate limit yields `unknown`, never `not_found`.

| # | Probe | Mechanism | Proves | Cannot prove |
|---|---|---|---|---|
| 1 | Base-repo object | `GET /repos/{o}/{r}/commits/{sha}` (200 confirmed; 422 "No commit found" is `not_found`, verified live; 404 is `unknown` because a hidden repo looks the same) | Object resolvable through this repo's URL, including dangling commits (**confirmed**, Gate 0) | Which ref holds it |
| 2 | PR refs and ancestry | Temp bare `git clone --mirror` (carries `refs/pull/*`, **confirmed**), then `git for-each-ref --contains <sha>` | Which refs (`refs/pull/N/head`, `merge`, tags) reach the SHA, including as an ancestor of a PR tip. Gate 0: `refs/pull/N/merge` kept the old commit as a parent after the PR branch was force-pushed | Dangling objects the mirror does not fetch (a SHA absent from a *successful* mirror is `not_found` for ref surfaces; a failed clone is `unknown`). Gate 0: a commit removed from `main` was invisible here and seen only by probe 1 |
| 3 | Fork network | `GET /repos/{o}/{r}/forks` (paginated), probe 1 per fork | Object resolvable through that fork's URL | See caveat |
| 4 | Tags and releases | Covered by probe 2 (`--contains` includes tags) | Tag reachability | Release notes or asset contents (not commits; out of scope) |
| 5 | Web commit view | `HEAD https://github.com/{o}/{r}/commit/{sha}` | The cached view Support removes. Gate 0 run 2: 200 for rewritten commits on a public repo, agreeing with probe 1 | Anything the API hides (none observed). Needs no token on public repos; 404 for private repos unauthenticated. **Opt-in only** (ADR-0005); **not implemented yet** |

## Caveats

- **Forks share Git data (verified 2026-10-12, doc 10 item 6).** A hit on a fork's commit URL means the object exists in the network, not that the fork holds a ref to it. The report separates *object resolvable* (cheap) from *ref-holding* (`--fork-refs`, opt-in, below).
- **Large networks.** Repos can have thousands of forks. Default cap 200 (`--max-forks`), prioritized by recent push activity and stars **among the first 1000 forks GitHub lists** (oldest first); a larger network is reported as incomplete coverage, never as complete. Output always prints coverage ("checked 200 of 4,312 forks"). See ADR-0004.
- **Probe 2 costs a clone.** Warn on large repos; use a temp dir with restrictive permissions, always deleted, including on error.
- **Probe 5 is automated access to web pages.** Off by default until GitHub's terms on automated access are reviewed (doc 07).
- The tool downloads content only from the user's own repo (probe 2), never from forks.

## Reported but not probed

- Local clones, CI caches, other mirrors: checklist items.
- **GH Archive** (gharchive.org): records the public GitHub timeline as hourly archives, from 2015 taken from the Events API, also on BigQuery. The Events API's `PushEvent` payload documents a `before` field (the SHA of the most recent commit on the ref before the push). Together these mean push events for **public** repos, including pre-rewrite SHAs, are held in a third-party dataset outside GitHub Support's reach. This is a documented inference. Caution: the archive is derived from the Events API, and Gate 0 run 2 saw no force-push events in the Events API, so whether force-pushed `before` SHAs reach the archive is untested and may be less than this suggests (doc 10, item 7a). Private repos are not in the public timeline.

## Support precondition and the fork probe

GitHub's page says Support acts only once no forks hold references to the sensitive data (doc 04). So fork *ref-holding* is the check that decides whether a Support request will be actionable. The report's *Ready for Support?* line depends on it.

### Ref-holding fork check (`--fork-refs`, implemented 2026-10-12)

Per (fork, old SHA), in order:

1. The object check above. If the SHA is **not resolvable** through the fork, no ref in that fork can hold it: `not_found`. If that check is `unknown`, the ref check is `unknown`.
2. If resolvable, GraphQL queries per fork (every ref except `refs/pull/*`, via `refPrefix:"refs/"`, 100 refs a page, up to 10 pages, 20 SHAs per query) call `Ref.compare(headRef: <sha>)` on each ref. `IDENTICAL` or `BEHIND` means the ref contains the SHA; `AHEAD` or `DIVERGED` means it does not. Heads, tags and unusual namespaces (`refs/notes/*`, `refs/remotes/*`, `refs/original/*`) are all covered.
3. A fork's own `refs/pull/*` are invisible to GraphQL, so a fork with any pull requests is `unknown` unless a holder was already proven.
4. Any error, null compare, unexpected status, missing repository or truncated ref list is `unknown`. A ref already proven to hold the SHA stands even if a later page fails.

Why not `git ls-remote`: it lists tips only, so a fork branch with the old commit buried under later commits would be missed, and `not_found` would be a false all-clear. Why not fetching the fork: it would download another person's content (see "Caveats"). `Ref.compare` returns a status and nothing else.

Verified 2026-10-12: statuses and semantics on `octocat/Hello-World`; an opaque `Something went wrong` error for an unresolvable SHA (so it cannot be distinguished from a transient fault); and, for 4 real forks, agreement with the REST compare endpoint on all 9 branch pairs checked, 5 of which were ancestor-only (the case `ls-remote` cannot see). **Not verified:** that GraphQL compare works for a SHA that exists only in the parent on a fork with a non-shared object store (open question 6 still blocks that, because it needs a second account).

### Fork coverage and "Ready for Support? yes"

Coverage is judged from the fork **listing**, not from `forks_count`, which is a cached counter that undercounts (1014 reported, 1056 real forks on `newren/git-filter-repo`). The listing is deduplicated by name and uses `sort=oldest` so pages stay stable while forks are created. Fork coverage is recorded as incomplete, with a stated reason in `forks_incomplete_reason`, when:

- the listing did not end on a short page (only the first 1000 forks were seen), or
- a listed fork has forks of its own that could not be listed. Forks of forks are enumerated breadth-first (a fork with `forks_count` > 0 is listed in turn), to three levels and 100 extra listing calls; a fork whose listing fails (for example a hidden fork) or lies past those bounds is a gap.

A sample smaller than the list (`--max-forks`) is incomplete too. "Ready for Support? yes" needs: `--fork-refs` ran, every listed fork was checked, none of the reasons above applies, nothing is `unknown`, and there is at least one finding. It still rests on GitHub's own counters: a nested fork is only looked for where a fork's cached `forks_count` is above zero, so a fork-of-a-fork created moments ago that the count does not yet show would be missed.
