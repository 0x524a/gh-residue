# 02. Architecture

Three layers, communicating through small typed interfaces.

```
inputs  ->  probe engine  ->  reporters
(adapters)  (independent       (table, JSON,
             probes)            Support draft, diff)
```

## Layer 1: input adapters

Each yields `[]OldCommit{sha, newSha?}`.

- `filter-repo` directory: reads `.git/filter-repo/commit-map` (old to new SHA; sorted by old SHA, so its order carries no meaning). `first-changed-commits` is read only for the Support draft. `changed-refs` is not read yet.
- SHA list: one full 40-char SHA per line. Used for BFG, manual rewrites, or a `git push --mirror` log.

Short SHAs are rejected; ambiguity would hide findings.

## Layer 2: probe engine

One probe per exposure surface, each behind:

```
Probe.Run(ctx, repo, shas) -> []Finding
```

Probes are independent. One failing or being rate-limited never discards the others. Every finding carries a status of `confirmed`, `not_found` or `unknown` (see ADR-0002). Probes are described in doc 03.

## Layer 3: reporters

Pure functions from a `Run` to output: table, JSON, Support draft, diff. No network.

## Shell

- A standalone Go CLI, packaged as a `gh` extension (`gh-residue`).
- Org mode is a thin batch loop over the same library (doc 05).

## Why this shape

- Probes can be added without touching reporters.
- Reporters are testable with golden files and no network.
- The "unknown is not clean" rule lives in one place (the status type), not in every probe.
