# Security policy

## Reporting a vulnerability

Please report privately, not in a public issue.

1. Use **Security → Report a vulnerability** on the [repository](https://github.com/0x524a/gh-residue/security/advisories/new). The report stays between you and the maintainer.
2. If you cannot use that, email [0x524a@gmail.com](mailto:0x524a@gmail.com).

Include the version (`gh residue --version`), your operating system, the command you ran, and what you expected against what happened. **Do not include credentials, or the SHAs of a leak you are still cleaning up.** A made-up value or a throwaway repository is enough to reproduce almost anything here.

This is a one-person project, so reports are handled on a best-effort basis. You will get a reply in the report thread, and a fix ships as a new release with a note in the release page.

## Supported versions

Only the latest release receives fixes. Check yours with `gh residue --version` and update with `gh extension upgrade residue`.

## What counts as a security issue

`gh-residue` handles a GitHub token and the identifiers of leaked commits, and its job is to tell you whether an exposure is closed. These are in scope:

- **A false all-clear.** Any path where a failed, skipped, rate-limited or partial check is reported as `not_found` or exits `0`. The tool's central promise is that an unknown result is never reported as clean.
- **Token exposure.** The token appearing in output, JSON, checkpoints, drafts, logs, error messages, process arguments, or a temporary file that other users can read.
- **Unsafe file handling.** Reports, checkpoints or drafts written with broader permissions than intended, a temporary clone that is not removed, or a path that escapes where it should write.
- **Injection.** A repository name, ref name, SHA or input file that makes the tool run a command, write a file or contact a host it should not.
- **Exposure of commit contents.** The tool is meant to read and store only SHAs, ref names, repository names and pull request numbers.

## Out of scope

- GitHub itself, including how long it keeps old commits. Only GitHub Support can purge them; report problems with that to GitHub.
- Secrets leaked in your own repositories. Rotate the credential first; this tool does not detect or revoke secrets.
- Findings that need an attacker who already controls your machine or your token.
- Missing hardening that does not lead to one of the issues above.

## Checking a download

Release binaries carry build-provenance attestations. With a recent GitHub CLI:

```bash
gh attestation verify <downloaded-file> --repo 0x524a/gh-residue
```
