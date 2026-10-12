# 08. Testing strategy

## Gate 0 status (2026-10-10)

Facts 1 and 2 confirmed (private repo), and a public-repo run done (web view confirmed, Events API unreliable). Fact 3 (fork object store) was left untested at the time for want of a second account; it was closed on 2026-10-12 from GitHub's documentation and a live check (doc 10 item 6). Results: [`spike/gate0-results.md`](spike/gate0-results.md). Both throwaway repos have been deleted (the private one on 2026-10-11).

## Gate 0: empirical spike (before any code)

A throwaway repo on the owner's account with a **fake placeholder string** (not secret-shaped, to avoid push-protection noise). Steps:

1. Commit the placeholder, open a PR, and fork the repo from a second account.
2. Force-push a rewrite that removes the commit.
3. Record what each mechanism returns: REST commit endpoint, `git ls-remote`, mirror clone, web view.
4. Keep the results as documented fixtures.

It must validate three facts the design depends on:

1. A dangling SHA returns 200 on the REST commit endpoint after a force-push.
2. `git clone --mirror` carries `refs/pull/*`.
3. A fork's URL resolves a SHA that only the parent holds.

Claims that Gate 0 has not confirmed stay tagged **(unverified)**. Fact 3 was closed on 2026-10-12 from GitHub's own documentation plus a live check on a public network (doc 10 item 6); only private and detached forks remain untested (item 19). If any fails, doc 03 is revised before implementation.

## Test layers (after Gate 0)

1. Unit tests for the probe engine using recorded HTTP fixtures.
2. A property test for the key invariant: **no error path can produce `not_found`**.
3. Git-based tests against local bare repos built to mimic `refs/pull/*`, using real `git`.
4. Golden tests for table, JSON, Support draft and diff output.
5. Exit-code contract tests for 0, 1, 2.
6. Diff-mode tests (`resolved`, `still-exposed`, `new`, `now-unknown`).
7. Live integration tests: opt-in, manual, need a token and the test repo. Not in default CI.
