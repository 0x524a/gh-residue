# Gate 0 results (2026-10-10)

Throwaway **private** repo on the owner's account, fake placeholder content (no real secret), created and deleted by the spike. Single account, no orgs, so fact 6 could not be tested.

## Scenario

- **B**: placeholder commit on branch `feature`, PR #1 opened, then `feature` force-pushed to a replacement commit (B no longer on any branch).
- **C**: placeholder commit pushed to `main`, then `main` force-pushed back one commit (no PR involved).

SHAs: B `a8fe590d...`, C `78b6c2ec...` (both removed from every branch).

## Results

| Probe | B (PR branch rewritten) | C (main rewritten) |
|---|---|---|
| REST `GET /repos/{r}/commits/{sha}` | **200** | **200** |
| `git ls-remote` refs | `refs/heads/{main,feature}`, `refs/pull/1/head`, `refs/pull/1/merge` | same (no ref for C) |
| `git clone --mirror` carries `refs/pull/*` | **yes** | yes |
| Mirror clone has the object (`cat-file`) | **yes**, via `refs/pull/1/merge` (parents: main, B) | **no**, object not fetched |
| `for-each-ref --contains` | `refs/pull/1/merge` | error: no such commit |
| PR timeline REST `head_ref_force_pushed` | exposes only the **new** head (`commit_id`) | n/a |
| GraphQL `HeadRefForcePushedEvent.beforeCommit.oid` | **returns B** | n/a |
| Events API `PushEvent` | none present (only `PullRequestEvent`) | none present |
| Web `HEAD /commit/{sha}` unauthenticated | 404 | 404 (inconclusive: private repo) |

## Conclusions

1. **Fact 4 confirmed.** Dangling SHAs (B and C) still resolve via the REST commit endpoint after force-push. Probe 1 is the only probe that sees C: the mirror clone cannot (design justification for keeping probe 1).
2. **Fact 5 confirmed.** `git clone --mirror` carries `refs/pull/*`.
3. **New finding:** `refs/pull/N/merge` can keep the old commit as a parent after the PR branch is force-pushed (observed still present about a minute later; lifetime unknown). Probe 2 (`--contains`) catches this; a probe that only checks `refs/pull/*/head` tips would miss it.
4. **New finding:** GraphQL `HeadRefForcePushedEvent` exposes `beforeCommit`, i.e. the pre-rewrite head. This is a possible **input source** for old SHAs on PR branches, and also means anyone who can read the PR can learn those SHAs (dual-use note in doc 06).
5. **Not found:** a trace of C (main-branch force-push) in refs or PR timeline. Without the filter-repo input or an Events/archive record, C is undiscoverable.
6. **Inconclusive:** web view (private repo, unauthenticated); Events API `PushEvent` absence (private repo, retention or event filtering unknown).
7. **Fact 6 (shared fork object store) not tested:** needs a second account.

## Run 2: public repo (2026-10-10, deleted afterwards)

Same scenario on a throwaway **public** repo (B = PR branch rewritten, C = `main` rewritten).

| Probe | B | C |
|---|---|---|
| REST `GET /repos/{r}/commits/{sha}`, **unauthenticated** | 200 | 200 |
| Web `HEAD /{r}/commit/{sha}`, unauthenticated | **200** | **200** |
| GraphQL `beforeCommit.oid` | returns B | n/a |
| Events API | 1 `PushEvent` (the push of C to `main`: `before` = base, `head` = C); `CreateEvent`; `PullRequestEvent` | see left |

Conclusions:
1. **Web view confirmed (public repo):** the cached commit view still serves rewritten commits B and C with HTTP 200. Closes question 14 for public repos. This is exactly what Support removes, so probe 5 has real value, subject to the terms limits in doc 07.
2. **Public repos need no token for probes 1 and 5**; a token is for private repos and rate limits.
3. **Events API did not record the force-pushes** (no `PushEvent` with a rewritten `before`/`head` for either rewrite, nor for the initial branch push) within the minutes after the run. Either events lag, or force-pushes are not emitted as expected. Treat Events API as **not a reliable discovery source**; question 15 stays open but leans negative. GH Archive derives from the same API, so question 7a should be re-checked against it.
4. GraphQL `beforeCommit` also works on public repos.

## Commands used (reproducible)

REST: `gh api -i repos/{r}/commits/{sha}`. Refs: `git ls-remote`, `git clone --mirror`, `git for-each-ref --contains`. Timeline: `gh api repos/{r}/issues/1/timeline`. GraphQL: `pullRequest(number:1){timelineItems(itemTypes:[HEAD_REF_FORCE_PUSHED_EVENT]){nodes{... on HeadRefForcePushedEvent{beforeCommit{oid} afterCommit{oid}}}}}`.
