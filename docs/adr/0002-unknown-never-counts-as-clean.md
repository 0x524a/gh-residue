# ADR-0002: Unknown never counts as clean

**Status:** accepted (design stage)

## Context

The product's value is trust in "exposure closed". Rate limits, private repos, truncated fork lists and network errors are all common. A single mislabelled failure would produce a false all-clear.

## Decision

Every finding is `confirmed`, `not_found` or `unknown`. Any error, timeout, rate limit, truncation or missing permission yields `unknown`. Exit code 2 means "incomplete, treat as not clean". Sampled fork coverage is always disclosed.

## Consequences

- A property test must show no error path can produce `not_found`.
- Some runs will end in exit 2 where a looser tool would print "ok". That is intended.
- Diff mode flags `now-unknown` loudly.
