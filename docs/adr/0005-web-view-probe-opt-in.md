# ADR-0005: Web-view probe is opt-in

**Status:** accepted (design stage), pending terms review

## Context

The cached commit view on github.com is what Support removes, so a probe of it is valuable. But it means automated requests to web pages, and GitHub's terms on that have not been reviewed (doc 10, item 3). The REST commit probe covers most of the same signal.

## Decision

Probe 5 is off by default and enabled with an explicit flag, limited to the user's own repo and SHAs, with a hard request cap.

Update 2026-10-10: the terms were read (doc 07) and conclude that web access counts as scraping under the AUP with a permitted-use list that doesn't clearly cover it. Gate 0 run 2 showed the web view and the REST commit endpoint agree (both HTTP 200 for rewritten commits on a public repo), so the web probe has not been shown to catch anything the REST probe misses. Its remaining value is confirming the cached view that Support removes, e.g. a before/after check once Support has acted. Default-on is not recommended.

## Consequences

- The default run stays within documented API use.
- Some cached-view exposure may be undetected by default; the report says so.
