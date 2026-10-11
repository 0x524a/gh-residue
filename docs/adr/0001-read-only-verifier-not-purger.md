# ADR-0001: Read-only verifier, not a purger

**Status:** accepted (design stage)

## Context

GitHub's documentation says cached views and PR references to rewritten commits are removed only by GitHub Support. A tool cannot purge them. A tool that claimed to "clean up" would overpromise.

## Decision

`gh-residue` is read-only. It verifies reachability, builds evidence, and drafts the Support request. It does not delete, push, rewrite, rotate or revoke anything.

## Consequences

- Read-only token scopes suffice.
- Smaller trust and safety surface.
- Users still depend on Support for actual purging.
- The name and README say "verify", never "clean".
