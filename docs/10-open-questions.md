# 10. Open questions and unverified claims

Last updated 2026-10-10. Closed items are kept (struck through) so the trail is visible.

## Open

| # | Claim or question | Where | How to close |
|---|---|---|---|
| 6 | Fork URL resolves a parent-only SHA (shared object store) | doc 03 | Gate 0 remainder: needs a second GitHub account |
| 7a | Whether specific force-pushes actually appear in GH Archive with the `before` SHA | doc 03 | Query a known public force-push in GH Archive or BigQuery |
| 8 | Events API retention window for `PushEvent` (docs say a timeline of up to 300 events; the time window wasn't in the fetched text) | doc 05 | GitHub Events docs; public-repo spike run |
| 9 | Commercial products already covering post-rewrite verification | doc 01 | Survey beyond GitHub repo search |
| 11 | Default fork cap (200) is reasonable (concurrency is now 1 per GitHub's best-practices page) | doc 05 | Tune after M1 |
| 12 | Formal trademark clearance for the name | doc 07 | Separate legal step, if desired |
| 13 | How long `refs/pull/N/merge` keeps the old commit after a PR-branch force-push (seen about 1 minute) | doc 03 | Repeat the Gate 0 scenario in a new throwaway repo and poll `refs/pull/N/merge` over time (the original spike repo was deleted 2026-10-11); GitHub docs |
| 14 | ~~Web commit view status for a dangling SHA on a public repo~~ | docs 03, ADR-0005 | **Closed 2026-10-10:** HTTP 200 for both rewritten commits (spike run 2). Still opt-in per doc 07 |
| 15 | Whether the Events API records force-pushes as `PushEvent` (run 2: it did not, within minutes, for either rewrite) | doc 05 | Re-check after some hours; treat as unreliable meanwhile |
| 16 | GraphQL `beforeCommit` documentation and retention for old-SHA discovery (works in Gate 0; not in the fetched reference page) | doc 05 | GraphQL docs in a browser; test on older PRs |
| 17 | Whether a `git-filter-repo` `commit-map` reliably yields the same "First Changed Commit(s)" value filter-repo prints | doc 04 | Compare on a test rewrite |

## Closed

_Item 10 (licence) closed 2026-10-11: owner chose Apache-2.0._

| # | Question | Resolution |
|---|---|---|
| 1 | ~~Exact fields GitHub Support requires~~ | **Closed 2026-10-10.** Repo name; affected-PR count; First Changed Commit(s) from filter-repo; LFS orphan note and file. Also a documented precondition: no non-PR references and no forks holding references. See doc 04 |
| 2 | ~~Authenticated REST rate limit and secondary limits~~ | **Closed 2026-10-10.** 5,000/hour personal; 1,000/hour `GITHUB_TOKEN`; secondary limits documented. See doc 05 |
| 3 | ~~Terms on automated access~~ | **Closed 2026-10-10** (read, not legal advice). API use under ToS section H; web access counts as scraping under the AUP. Probe 5 stays opt-in. See doc 07 |
| 4 | ~~Dangling SHA resolves via REST commit endpoint~~ | Confirmed, Gate 0 |
| 5 | ~~`git clone --mirror` carries `refs/pull/*`~~ | Confirmed, Gate 0 |
| 7 | ~~GH Archive retains push events~~ | **Mostly closed 2026-10-10.** Public timeline archived since 2015; `PushEvent.before` documented. Specific force-push presence moved to 7a |
