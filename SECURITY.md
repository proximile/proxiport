# Security policy

## Reporting a vulnerability

Please report security vulnerabilities through GitHub's private
vulnerability reporting: open the **Security** tab on this repository
and choose **"Report a vulnerability"**. That channel is end-to-end
private between the reporter and the maintainers and creates a tracked
advisory once a fix lands.

Do not file vulnerabilities as public issues.

Include:

- a description of the vulnerability,
- the affected version (commit SHA or release tag),
- a proof-of-concept or reproducer, if you have one, and
- whether you intend to publish your own write-up, and on what
  timeline.

We will acknowledge receipt within five business days, and aim to
publish a fix and a coordinated disclosure within ninety days of the
report. If the issue is in a third-party component that ProxiPort
bundles (chisel, noVNC, Apache Guacamole, a Go module), we will route
the report upstream and link your write-up to the upstream advisory.

## Scope

In scope:

- the ProxiPort Go server (`proxiportd`)
- the ProxiPort agent (`proxiport`)
- the SvelteKit frontend served by the server
- the REST API and the chisel tunnel transport as ProxiPort uses them

Out of scope:

- vulnerabilities in third-party components that are not specific to
  how ProxiPort uses them (report those upstream; we will track and
  patch promptly)
- weaknesses that require pre-existing administrator access to the
  server (operational security is the deploying party's responsibility,
  not a code defect)
- self-hosted deployments operated by third parties — Proximile LLC is
  not responsible for those

## What we are not interested in

- automated scanner output without a working PoC
- "username enumeration" reports on endpoints documented to leak the
  fact of a username's existence (e.g. the login flow)
- TLS or operational findings against any third-party host happening to
  run ProxiPort — only the source in this repository is in scope

## Coordinated disclosure

We follow standard coordinated disclosure: the reporter holds public
disclosure until either the fix has shipped, or 90 days have passed,
whichever comes first. If you need a faster timeline because the bug
is being actively exploited, say so in the initial report.

We will credit you in the advisory unless you ask us not to.
