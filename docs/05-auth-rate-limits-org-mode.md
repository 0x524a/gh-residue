# 05. Auth, rate limits, org mode

## Auth

- Token order: `GH_TOKEN` / `GITHUB_TOKEN`, then `gh auth token`.
- Read-only is enough: fine-grained token with Contents:read and Metadata:read, or classic `repo` for private repos. README states minimum scopes.
- No GitHub App in v1 (hosting and install flow for little gain).
- The token is never logged, never written to JSON, never passed on a command line. Git uses a credential helper or `GIT_ASKPASS`.

## Rate limits

Confirmed against docs.github.com "Rate limits for the REST API" (fetched 2026-10-10):

- Unauthenticated: 60 requests/hour. Personal token: 5,000/hour. `GITHUB_TOKEN` in Actions: 1,000/hour per repository (matters if this runs in CI).
- GitHub App installations: 5,000/hour, scaling with repos and users up to 12,500; 15,000 on Enterprise Cloud.
- Secondary limits (can change without notice, and can trigger for undisclosed reasons): at most 100 concurrent requests (shared REST and GraphQL); 900 points/minute per REST endpoint, 2,000 points/minute for GraphQL; 90 seconds of CPU time per 60 seconds of real time (60 s of that for GraphQL).
- On 403/429: if `retry-after` is present wait that long; if `x-ratelimit-remaining` is 0 wait until `x-ratelimit-reset`; otherwise wait at least one minute, then back off exponentially and give up after a bounded number of retries. `GET /rate_limit` doesn't count against the primary limit.

Design consequence: default concurrency 4 is well under the 100 cap; keep it low because CPU-time limits are not observable.

- Up-front estimate: SHAs x (1 base + N forks), printed before starting; above a threshold require `--yes`.
- Fork probing is the expensive part: capped (default 200) and prioritized by recent push and stars.
- Honor `Retry-After` and `X-RateLimit-Reset`. Default concurrency 4. Use `ETag` conditional requests on re-runs where supported.
- Budget exhausted mid-run: remaining checks become `unknown`, exit code 2.
- A resumable checkpoint file lets a rate-limited run continue (`--checkpoint FILE`, implemented). It is the JSON Run. Resuming carries over only `confirmed` and `not_found`, never `unknown`, and refuses results older than `--checkpoint-max-age`, because an old `not_found` must not pass as current. Written at the end of a run, not incrementally.

## Org mode (v1.1)

- Input is a manifest (YAML/JSON) mapping each repo to its SHA source (`filter-repo` dir or SHA list).
- It is a batch of single-repo runs. It does **not** discover leaks or old SHAs; without the rewrite input that is not reliably possible.
- Output is aggregated JSON: per-repo exit code, rolled-up summary; the worst code is the overall result.
- One global request budget shared across repos. Sequential or low concurrency; one repo failing does not stop the rest.

## Candidate input, not v1: PR force-push discovery

Gate 0 found that GraphQL `HeadRefForcePushedEvent.beforeCommit.oid` returns the pre-rewrite head of a force-pushed PR branch (REST timeline exposes only the new head). That could seed old SHAs for PR branches in repos with no filter-repo directory. It does not cover rewrites of branches without a PR (the `main` case in Gate 0 left no trace). Treat as a v1.1 input adapter candidate; it is discoverable by anyone who can read the PR, which matters for doc 06.

## Future idea: Events API discovery

The repo Events API documents `PushEvent.before`. Gate 0 saw no force-push events in either a private or a public test repo within minutes of the rewrite (run 2 recorded a single `PushEvent`, for a normal push). Treat the Events API as an **unreliable discovery source**; retention and any delay are **(unverified)** (doc 10, items 8 and 15).

## Failure behavior

| Case | Result |
|---|---|
| No token or wrong scope | Stop early, clear message, exit 2 |
| Private repo the token cannot read | `unknown`, never `not_found` |
| Fork list truncated or fork deleted mid-run | `unknown` |
