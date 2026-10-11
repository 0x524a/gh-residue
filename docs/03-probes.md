# 03. Probes

Every probe answers: is old SHA X still reachable via surface Y? Any error, timeout or rate limit yields `unknown`, never `not_found`.

| # | Probe | Mechanism | Proves | Cannot prove |
|---|---|---|---|---|
| 1 | Base-repo object | `GET /repos/{o}/{r}/commits/{sha}` (200 confirmed; 422 "No commit found" is `not_found`, verified live; 404 is `unknown` because a hidden repo looks the same) | Object resolvable through this repo's URL, including dangling commits (**confirmed**, Gate 0) | Which ref holds it |
| 2 | PR refs and ancestry | Temp bare `git clone --mirror` (carries `refs/pull/*`, **confirmed**), then `git for-each-ref --contains <sha>` | Which refs (`refs/pull/N/head`, `merge`, tags) reach the SHA, including as an ancestor of a PR tip. Gate 0: `refs/pull/N/merge` kept the old commit as a parent after the PR branch was force-pushed | Dangling objects the mirror does not fetch (a SHA absent from a *successful* mirror is `not_found` for ref surfaces; a failed clone is `unknown`). Gate 0: a commit removed from `main` was invisible here and seen only by probe 1 |
| 3 | Fork network | `GET /repos/{o}/{r}/forks` (paginated), probe 1 per fork | Object resolvable through that fork's URL | See caveat |
| 4 | Tags and releases | Covered by probe 2 (`--contains` includes tags) | Tag reachability | Release notes or asset contents (not commits; out of scope) |
| 5 | Web commit view | `HEAD https://github.com/{o}/{r}/commit/{sha}` | The cached view Support removes. Gate 0 run 2: 200 for rewritten commits on a public repo, agreeing with probe 1 | Anything the API hides (none observed). Needs no token on public repos; 404 for private repos unauthenticated. **Opt-in only** (ADR-0005) |

## Caveats

- **Forks share one object store (unverified).** A hit on a fork's commit URL means the object exists in the network, not that the fork holds a ref to it. The report separates *object resolvable* (cheap) from *ref-holding* (one `ls-remote` per fork; opt-in).
- **Large networks.** Repos can have thousands of forks. Default cap 200 (tunable), prioritized by recent push activity and stars. Output always prints coverage ("checked 200 of 4,312 forks"). See ADR-0004.
- **Probe 2 costs a clone.** Warn on large repos; use a temp dir with restrictive permissions, always deleted, including on error.
- **Probe 5 is automated access to web pages.** Off by default until GitHub's terms on automated access are reviewed (doc 07).
- The tool downloads content only from the user's own repo (probe 2), never from forks.

## Reported but not probed

- Local clones, CI caches, other mirrors: checklist items.
- **GH Archive** (gharchive.org): records the public GitHub timeline as hourly archives, from 2015 taken from the Events API, also on BigQuery. The Events API's `PushEvent` payload documents a `before` field (the SHA of the most recent commit on the ref before the push). Together these mean push events for **public** repos, including pre-rewrite SHAs, are held in a third-party dataset outside GitHub Support's reach. This is a documented inference. Caution: the archive is derived from the Events API, and Gate 0 run 2 saw no force-push events in the Events API, so whether force-pushed `before` SHAs reach the archive is untested and may be less than this suggests (doc 10, item 7a). Private repos are not in the public timeline.

## Support precondition and the fork probe

GitHub's page says Support acts only once no forks hold references to the sensitive data (doc 04). So fork *ref-holding* is not a nice-to-have: it is the check that decides whether a Support request will be actionable. The report's *Ready for Support?* line depends on it. Object-resolvable-only fork results (cheap) are shown separately and do not block.
