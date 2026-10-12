# 05. Auth, rate limits, org mode

## Auth

- Token order: `GH_TOKEN` / `GITHUB_TOKEN`, then `gh auth token`.
- Read-only is enough: fine-grained token with Contents:read and Metadata:read, or classic `repo` for private repos. README states minimum scopes.
- No GitHub App in v1 (hosting and install flow for little gain).
- The token is never logged, never written to JSON, never passed on a command line. Git uses a credential helper or `GIT_ASKPASS`.

## API version

Requests pin `X-GitHub-Api-Version: 2022-11-28`, supported until 2028-03-10 (docs fetched 2026-10-11; latest is 2026-03-10). Checked live on 2026-10-11: the commits endpoint returns identical 200 and 422 for both versions. Re-verify the 422 "No commit found" behaviour before moving versions, since ADR-0002 depends on it.

## Why not `GET /git/commits/{sha}`

It returns about half the payload (no file patches) but answers **404** for an absent SHA, which a hidden repo also returns, so it can never support a safe `not_found`. `GET /repos/{r}/commits/{sha}` returns a distinct 422 and stays.

## Rate limits

Confirmed against docs.github.com "Rate limits for the REST API" (fetched 2026-10-10):

- Unauthenticated: 60 requests/hour. Personal token: 5,000/hour. `GITHUB_TOKEN` in Actions: 1,000/hour per repository (matters if this runs in CI).
- GitHub App installations: 5,000/hour, scaling with repos and users up to 12,500; 15,000 on Enterprise Cloud.
- Secondary limits (can change without notice, and can trigger for undisclosed reasons): at most 100 concurrent requests (shared REST and GraphQL); 900 points/minute per REST endpoint, 2,000 points/minute for GraphQL; 90 seconds of CPU time per 60 seconds of real time (60 s of that for GraphQL).
- On 403/429: if `retry-after` is present wait that long; if `x-ratelimit-remaining` is 0 wait until `x-ratelimit-reset`; otherwise wait at least one minute, then back off exponentially and give up after a bounded number of retries. `GET /rate_limit` doesn't count against the primary limit.

Design consequence: GitHub's best-practices page (fetched 2026-10-11) says to make requests **serially** to avoid secondary limits, so default concurrency is **1** (`--concurrency N` to opt in). CPU-time limits are not observable, so higher values are the user's risk. Rate-limit handling follows the same page: `retry-after`, then `x-ratelimit-reset`; a plain 403/429 with neither header is reported `unknown` rather than retried after a minute (stricter than the page, never a false clear).

- Up-front estimate: SHAs x (1 base + N forks), printed before starting; above a threshold require `--yes`.
- Fork probing is the expensive part: capped (default 200) and prioritized by recent push and stars.
- Honor `Retry-After` and `X-RateLimit-Reset`. Default concurrency 1. Use `ETag` conditional requests on re-runs where supported.
- Budget exhausted mid-run: remaining checks become `unknown`, exit code 2.
- A resumable checkpoint file lets a rate-limited run continue (`--checkpoint FILE`, implemented). It is the JSON Run. Resuming carries over only `confirmed` and `not_found`, never `unknown`, and refuses results older than `--checkpoint-max-age`, because an old `not_found` must not pass as current. Written at the end of a run, not incrementally.

## Org mode (v1.1)

- Input is a manifest (YAML/JSON) mapping each repo to its SHA source (`filter-repo` dir or SHA list).
- It is a batch of single-repo runs. It does **not** discover leaks or old SHAs; without the rewrite input that is not reliably possible.
- Output is aggregated JSON: per-repo exit code, rolled-up summary; the worst code is the overall result.
- One global request budget shared across repos. Sequential or low concurrency; one repo failing does not stop the rest.

## Candidate input, not v1: PR force-push discovery

Gate 0 found that GraphQL `HeadRefForcePushedEvent.beforeCommit.oid` returns the pre-rewrite head of a force-pushed PR branch (REST timeline exposes only the new head). That could seed old SHAs for PR branches in repos with no filter-repo directory. It does not cover rewrites of branches without a PR (the `main` case in Gate 0 left no trace). Treat as a v1.1 input adapter candidate; it is discoverable by anyone who can read the PR, which matters for doc 06.

## Candidate input, not v1: Events API discovery

The repository Events API records force-pushes as `PushEvent` with `before` set to the pre-push SHA (doc 10 item 15, reviewed 2026-10-12: 25 of 25 sampled PR force-pushes). Limits: latency of 30 s to 6 h (documented), at most 300 events from the past 30 days, so a busy repository's window can be a few days. Gate 0's empty result was latency, not absence. It could seed old SHAs when no filter-repo directory exists, within that window. Like the GraphQL adapter above, anyone who can read the repository can read these events, which matters for doc 06.

## Failure behavior

| Case | Result |
|---|---|
| No token | Warn and continue: public repositories work at 60 requests/hour; a private repository is `unknown`. `--fork-refs` needs a token and exits 2 without one |
| Wrong scope | The affected checks are `unknown`, never `not_found` |
| Private repo the token cannot read | `unknown`, never `not_found` |
| Fork list truncated, or a listed fork has forks of its own | Fork coverage incomplete with a stated reason (`forks_incomplete_reason`); exit 2 unless something is confirmed |
| Fork deleted mid-run | `unknown` for that fork |
