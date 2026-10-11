# 07. Legal and licensing

Not legal advice. A design-stage checklist.

## License

Proposed: Apache-2.0 (patent grant). Confirm it matches the owner's other projects.

## Trademark and naming

- Referential use of "GitHub" ("works with GitHub") is the intended use.
- `gh-` is the required naming convention for `gh` extension repos; it is a convention, not an endorsement. The README states the project is unaffiliated with GitHub.
- No GitHub or Octocat logo until accepted into the GitHub Developer Program, whose documented benefit includes permission to use them for integrations (docs.github.com, fetched 2026-10; no other documented perks found).
- Name-collision check, 2026-10: no `gh-residue` repo found; `gh-residue` free on PyPI, npm, crates.io. Bare `residue` is taken on PyPI and npm; `abumq/residue` (40 stars) is an unrelated logging server. **Not a trademark search.**

## Terms of service

Read 2026-10-10: GitHub Acceptable Use Policies and Terms of Service section H (API Terms). Not legal advice; clause numbers may move.

- **API use** is governed by ToS section H: abuse or excessively frequent requests may lead to suspension of API access, in GitHub's sole discretion, and tokens may not be shared to exceed rate limits. The tool must respect rate limits (doc 05) and never pool tokens.
- **Scraping** is defined in the AUP as extracting information via an automated process such as a bot or web crawler, and explicitly *not* collection through the API. The AUP also prohibits "excessive automated bulk activity" and placing "undue burden" on servers via automated means. It restricts uses of scraped information to listed purposes (researchers, archivists, others); a status-code check on one's own repo isn't clearly among them.
- **Conclusion for probe 5 (web view):** it is automated access to web pages rather than the API, so it sits in the scraping definition and the permitted-use list doesn't clearly cover it. It stays opt-in, limited to the user's own repo and the user's own SHAs, with a hard request cap and a README note. Default-on remains not recommended.
- The `gh` tool and the REST/GraphQL probes (1 to 4) use the API and fall under section H, not the scraping rules.

## Data protection

Fork owners' usernames are other people's data: local output only, omitted from the default Support draft (doc 06).

## Developer Program

Re-read 2026-10-11: membership is open to individuals and companies with (1) an integration in production or development using the GitHub API and (2) an email address where GitHub users can contact them for support. The documented benefits are permission to use the Octocat or GitHub logo to show the product works with GitHub, and promoting the integration with a video or blog post. Eligibility is an integration using the GitHub API plus a public support email. Registration needs a support email address, which is the owner's call (not decided); until then no GitHub or Octocat logo is used anywhere. This tool qualifies once it exists. Registration is optional and carries no legal bearing on the licence.
