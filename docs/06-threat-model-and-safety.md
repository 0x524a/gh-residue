# 06. Threat model and safety

## Assets

The user's repo and token; third-party fork owners' identities; the old SHAs themselves (sensitive: they point at leaked material).

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| False all-clear leads the user to stop remediating | ADR-0002: failures are `unknown`; exit 2 means not clean; footer repeats "rotate first" |
| Token leakage | Never logged, never in JSON, never on a command line; read-only scopes |
| Tool used to hunt for leaked data | Needs old SHAs as input; returns reachability only, never contents. Low dual-use; scanners already exist (doc 01). Note (Gate 0): anyone who can read a PR can already learn pre-rewrite head SHAs via GraphQL `HeadRefForcePushedEvent`, so a PR-timeline input adapter (doc 05) adds no new capability, but should still be described honestly in the README |
| Fork owners' usernames are third-party data | Shown in local output; omitted from the default Support draft |
| Report file lists SHAs of a leak | Documented as sensitive; do not commit the JSON to a public repo |
| Temp clone residue | Restrictive permissions, deleted on exit including on error |
| Hammering GitHub | Request budget, low concurrency, honoring rate-limit headers |

## Honest limits (README)

No purge; no secret rotation; no guarantees about archives (GH Archive is lossy: of 25 sampled public force-pushes only 2 were present, doc 10 item 7a) or other people's clones.
