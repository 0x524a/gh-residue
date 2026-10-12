# ADR-0004: Fork sampling with disclosed coverage

**Status:** accepted (design stage)

## Context

Popular repos have thousands of forks. Probing every fork for every SHA exceeds rate limits.

## Decision

Probe a capped, prioritized sample (default 200, ordered by recent push and stars). Always print and record coverage ("checked 200 of 4,312"). A sampled-only result with nothing confirmed exits 2, not 0. Complete coverage means raising `--max-forks` above the number of listed forks, with a printed request estimate. Listing is hard-capped at 1000 forks (10 pages), and that cap is not a flag.

## Consequences

- Honest partial answers instead of silent truncation.
- Default numbers are a guess; tuned after M1 (doc 10, item 11).
- Forks share Git data with their network (GitHub docs, verified 2026-10-12: doc 10 item 6), so a hit on the parent already implies network-wide resolvability; the fork object probe adds little by itself, and the useful fork information is which forks hold refs (`--fork-refs`).
- Update 2026-10-12: completeness is judged from the listing, not from `forks_count`, which undercounts (1014 reported for 1056 real forks). A truncated listing, or any listed fork that has forks of its own, is recorded as `forks_incomplete_reason` and makes fork coverage incomplete; forks of forks are not enumerated.
- Update 2026-10-12: ref-holding forks are now checked with `--fork-refs` (doc 03). *Ready for Support?* can be `yes`, but only with that flag, full fork coverage and no unknown results.
- Update 2026-10-10: GitHub documents that Support acts only when no forks hold references to the data. A sampled result therefore can never report *Ready for Support?* as `yes`; with an incomplete fork sample the answer is `unknown`.
