# ADR-0004: Fork sampling with disclosed coverage

**Status:** accepted (design stage)

## Context

Popular repos have thousands of forks. Probing every fork for every SHA exceeds rate limits.

## Decision

Probe a capped, prioritized sample (default 200, ordered by recent push and stars). Always print and record coverage ("checked 200 of 4,312"). A sampled-only result with nothing confirmed exits 2, not 0. Complete coverage is available with an explicit flag and a printed request estimate.

## Consequences

- Honest partial answers instead of silent truncation.
- Default numbers are a guess; tuned after M1 (doc 10, item 11).
- Because forks may share an object store **(unverified)**, a hit on the parent already implies network-wide resolvability; the fork probe mainly identifies which forks hold refs.
- Update 2026-10-10: GitHub documents that Support acts only when no forks hold references to the data. A sampled result therefore can never report *Ready for Support?* as `yes`; with an incomplete fork sample the answer is `unknown`.
