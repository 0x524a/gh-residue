# 04. Output and data model

## Data model

All JSON carries `schema_version`.

- `Run`: `schema_version`, `tool_version`, `repo`, `started_at`, `inputs` (source type, count), `coverage`, `findings[]`.
- `Finding`: `old_sha`, `surface` (`base_object`, `branch` (any non-PR, non-tag ref, which Support's precondition cares about), `pr_ref`, `tag`, `fork_object`, `fork_ref`, `web_view`), `location` (ref name, PR number or fork full name), `status` (`confirmed` | `not_found` | `unknown`), `evidence` (one line: HTTP status or git result), `checked_at`.
- `Coverage`: probes run/failed, forks total vs checked, `forks_incomplete_reason` (set when the fork listing was truncated or forks of forks exist; non-empty means fork coverage is incomplete), per-surface `complete` flag.

Only SHAs, ref names, repo names and PR numbers are stored. Never file contents, diffs or secrets. Output ordering is deterministic so two runs diff cleanly.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Every probe completed; nothing exposed |
| 1 | At least one `confirmed` exposure (wins over incomplete: an exposure is already proven) |
| 2 | Incomplete: any `unknown`, or sampled coverage, with nothing confirmed. Treat as **not clean** |

## Table

One row per (SHA, surface), grouped by SHA, plus a summary line such as "3 SHAs, 2 still exposed via 5 PR refs, 1 via 12 forks (checked 200/4312)". Footer always says "rotate the credential first" and lists unprobeable surfaces.

## Support draft

Markdown, printed or written to a file, never auto-submitted. The field list is **confirmed** against GitHub's "Removing sensitive data from a repository" page (fetched 2026-10-10). Support asks for:

1. Owner and repository name.
2. The number of affected pull requests (used by Support to verify the requester understands the scope).
3. The "First Changed Commit(s)" reported by `git-filter-repo` (its output line `NOTE: First Changed Commit(s)`). The `commit-map` file does not carry it and its order is sorted by SHA, so it cannot be inferred. The tool reads `.git/filter-repo/first-changed-commits` (first column of each line, which is what the NOTE line prints), accepts `--first-changed`, and otherwise leaves the field blank with a note. It never guesses.
4. If filter-repo printed `NOTE: There were LFS Objects Orphaned by this rewrite`, say so and attach the named file.

The draft also carries the old SHAs, and a **blank section the user must fill** explaining why rotation cannot mitigate the risk: GitHub says Support only assists where the risk can't be mitigated by rotating credentials, and the tool never writes that claim for the user. Fork owners' usernames are omitted by default.

**Support's precondition (documented):** Support acts once "all references other than PRs" are cleaned up and "no forks have references to the sensitive data". The report therefore has a *Ready for Support?* line that is `no` while any ref-holding fork or non-PR ref is confirmed, and `unknown` while those checks are incomplete. After acting, Support dereferences or deletes affected PRs, runs server garbage collection, and removes cached views.

## Diff mode

`verify --against previous.json` classifies each (SHA, surface, location) as `resolved`, `still-exposed`, `new` or `now-unknown`. Support acting should appear as `confirmed` to `not_found`. `now-unknown` is flagged loudly.
