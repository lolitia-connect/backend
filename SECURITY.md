# Security Policy

PPanel handles payments, sessions and the credentials of proxy nodes, so we
treat security reports as a priority. Thank you for taking the time to report
one responsibly.

This repository is a fork: it is maintained at
[lolitia-connect/backend](https://github.com/lolitia-connect/backend), which
tracks upstream [perfect-panel/server](https://github.com/perfect-panel/server)
and merges its release tags periodically (see `.ai/sync-diffs/`). Report
against this fork at the link below. If the finding also reproduces on upstream
and is not fork-specific, it is worth reporting there too — say so in the
report and we will not duplicate the work.

## Supported versions

| Line | Branch | Where it ships | Security fixes |
|---|---|---|---|
| Stable | `master` | Tagged releases (`v*`) | Yes, as a patch release |
| Integration | `develop` | Not released; built by CI on every push | Yes, in the next merge to `master` |
| Older tagged releases | | | No: upgrade to the latest release |

The fork's releases are published at
<https://github.com/lolitia-connect/backend/releases>. Version tags are shared
with upstream's history; the fork's own releases are the ones attached to this
repository.

The frontend, node backend and edge renderer live in their own repositories and
have their own policies; reports about them belong there.

## Reporting a vulnerability

Please do not report security vulnerabilities through public GitHub issues,
discussions or pull requests, and do not attach exploit details to an existing
issue.

Report them privately through GitHub Security Advisories:
[Report a vulnerability](https://github.com/lolitia-connect/backend/security/advisories/new)
(the "Security" tab of this repository). The report is visible to the
maintainers only, and the advisory is where the fix is coordinated and the
credit recorded.

A useful report contains:

- the version or commit (`ppanel-server version`, the release tag, or the
  container image digest) and how it is deployed (binary, Docker, behind which
  proxy);
- the affected component (API endpoint, payment provider, node protocol,
  subscription output, plugin, configuration);
- steps to reproduce, or a proof of concept, and the impact you see (what an
  attacker gains, which role they need);
- whether the issue is already public, known upstream, or known to you to be
  exploited.

If you cannot use GitHub's form, open an issue that only asks for a private
contact, without any details of the finding, and a maintainer will reply with
one.

## What to expect

- Acknowledgement within 3 business days of the report.
- An assessment (confirmed, not a vulnerability, duplicate, out of scope) and a
  severity within 7 days, with questions if we cannot reproduce it.
- A fix released on `master` as a patch version. Where the finding is not
  fork-specific it is also reported upstream and, when upstream ships a fix,
  the fork merges it.
- Coordinated disclosure: the advisory is published with the fix, and you are
  credited unless you prefer not to be. Please give us up to 90 days before
  disclosing on your own.

## Scope

In scope: this repository, the release binaries, the container image and the
install script and documentation shipped with them.

Out of scope, please report upstream instead (and tell us, so we can update the
dependency): vulnerabilities in third-party dependencies that this server does
not expose, and in the databases, Redis and reverse proxies it runs with.
Reports about instances operated by third parties belong to their operators.

Testing against your own installation is welcome. Do not test against
installations you do not own, do not access or modify other people's data, and
do not run denial-of-service tests against public instances.

## Hardening a deployment

[doc/config.md](doc/config.md) documents the settings that matter for security:
a long random `JwtAuth.AccessSecret`, `TrustedProxies` and `AllowedOrigins` for
an installation behind a reverse proxy, TLS to the database (`sslmode`), and the
request limits of the `HTTP` section. Keep the configuration file
(`config.yaml`, or whatever `--config` names) readable by the service user only
(`0600`): it holds the JWT secret and the database credentials. The setup wizard
and the environment-variable installation write it with that mode.

Administrator-facing settings that hold secrets are masked in the API responses
and preserve the stored value when the mask is submitted unchanged
(`internal/logic/admin/system/secrets.go`): the Turnstile secret, the currency
provider access key, and node outbound passwords and UUIDs.

The GeoIP database is optional: `GeoIP.Required` defaults to `false`, and the
file is only downloaded when it does not exist and `GeoIP.Download` is on. Set
`GeoIP.SHA256` when you point `GeoIP.DownloadURL` at a copy you control: the
digest is checked on a temporary file before it is renamed into place, so a
tampered download cannot replace the file. Set `GeoIP.Required` to make a
missing database fatal instead of a warning.
