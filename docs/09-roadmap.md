# 09. Roadmap

| Milestone | Content | Exit criteria |
|---|---|---|
| M0 | Gate 0 spike (doc 08) | Three facts confirmed or design revised. **Status:** two of three confirmed; fact 3 (shared fork object store) blocked without a second account. **Proposed (owner to decide):** accept M0 with fact 3 deferred, since the fork probe already labels *object resolvable* vs *ref-holding* (doc 03, ADR-0004) and nothing in M1 depends on fact 3 being true |
| M0b | Resolve open questions that block design: Support field list, rate-limit numbers, terms on automated access | **Done 2026-10-10** (doc 10 items 1 to 3 closed) |
| M1 | Single-repo CLI: `filter-repo` + SHA-list inputs, probes 1 to 4, table + JSON, exit codes | Contract and golden tests green |
| M2 | Support draft, diff mode, checkpoint/resume | Diff tests green |
| M3 | `gh` extension packaging; Developer Program registration (optional) | Install via `gh extension install` works |
| M4 | Org mode from manifest (v1.1) | Aggregated report, shared budget |
| Later | Probe 5 default-on if terms allow; ref-holding fork probe; force-push discovery research | Case by case |

Nothing in M1 or later starts before M0.

## Status log

- 2026-10-11: CI extended: lint, tests on Linux/macOS/Windows, govulncheck, and a release-style (cgo off) build-and-run smoke test per OS. It found three real problems: golden files checked out with CRLF on Windows (fixed with `.gitattributes`), a test asserting Unix file modes on Windows, and Go 1.22 test binaries aborting on macOS 26 (`missing LC_UUID`; cgo-on only, Go 1.23+ fine, shipped cgo-off binaries unaffected). `go.mod` floor raised to 1.23. README badges added.

- 2026-10-11: README redesigned around a commit-graph banner drawn from the Gate 0 findings (light, dark and stacked mobile variants, checked on github.com at 390px and 1280px). Sample output in the README is real output, trimmed, with the repository name replaced. The Developer Program page was re-read: no logos until registration.

- 2026-10-11: build-provenance attestations enabled (verified on `rc.3`: SLSA provenance v1, subject digest matches the downloaded binary); stable `v0.1.0` released. M3 is done except exercising `gh extension install` on a clean machine and Windows verification.

- 2026-10-11: repository made public under Apache-2.0 after a tree and history scan (no secrets, one author identity). README status block rewritten to match reality.

- 2026-10-11 (M3 mostly done): private repo `0x524a/gh-residue`; CI (gofmt, vet, test) and a tag-triggered release using `cli/gh-extension-precompile` (pinned by SHA) produce a prerelease `v0.1.0-rc.2` with 12 platform binaries. Fork probing is now serial by default (`--concurrency`), per GitHub's best-practices page. Not done: `gh extension install` itself not exercised on a machine, artifact attestations (need permissions and, for private repos, a supporting plan), making the repo public, a licence (open question 10), Developer Program registration (needs a support email the owner has not chosen). Windows: the mirror probe's `GIT_ASKPASS` helper is a `sh` script; unverified on Windows.

- 2026-10-11 (M2 exit criteria met): golden tests added for table, JSON, Support draft (full and minimal) and diff (changed and unchanged), regenerated with `-update` and reviewed by hand. They immediately caught a pluralisation bug ("1 results"). Contract tests for exit codes 0, 1 and 2 exist in `internal/model`. Recorded-HTTP fixture files are not used: probe tests build `httptest` servers inline instead.

- 2026-10-11 (M2 complete): checkpoint/resume implemented (`--checkpoint FILE`). The checkpoint is the `--json` Run; a resume carries over only `confirmed` and `not_found` results (original `checked_at` kept) and re-runs the rest. Refused for a different repo or when the oldest carried result is older than `--checkpoint-max-age` (default 24h; an unparseable timestamp counts as stale). Limitation: the file is written when a run ends, including Ctrl-C and rate-limit exhaustion, but not mid-run, so a hard kill loses that run's progress. M2 exit criteria still need golden-file tests.

- 2026-10-10 (M2, partial): Support draft (`--support-draft`), diff mode (`--against`) and skipped-probe handling implemented with tests. Checkpoint/resume not started. Doc 10 item 17 still open: the draft infers First Changed Commit from `commit-map` order and says so in the output. A disabled probe now reports its surfaces incomplete, so `--no-mirror` or `--no-forks` can never exit 0.

- 2026-10-10 (later): owner gave the go-ahead; M0 accepted with fact 3 deferred. M1 implemented in Go: inputs, probes 1 to 3 (fork probe is object-resolvable only), table + JSON, exit codes, `verify` command. Contract tests for exit codes and the no-error-yields-`not_found` property are in `internal/`. Golden-file tests and recorded HTTP fixtures are still to do.

- 2026-10-10: spec written; M0b done; M0 two of three facts confirmed (fact 3 deferred pending owner decision); work paused by the owner. No code written.
