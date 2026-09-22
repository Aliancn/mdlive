# Security Policy

## Supported Versions

Only the latest release is supported with security updates.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please use [GitHub Private Vulnerability Reporting](https://github.com/k1LoW/mo/security/advisories/new) to submit a report.

You should receive a response within 7 days. If the vulnerability is confirmed, a fix will be released as soon as possible.

## Scope

Reports related to the following are in scope:

- Unauthorized file access or path traversal
- Cross-site scripting (XSS) via rendered Markdown
- Authentication or authorization bypass on the local server
- Remote code execution

### Threat model

`mo` is a local development tool. By default it binds to `localhost` and serves files the user has explicitly opened to that same user's browser. The user already has OS-level read access to anything `mo` can read, so file access by the user (or by the user's browser) over the loopback interface is not considered "unauthorized" and does not by itself constitute path traversal in the security sense. Reports of this shape are out of scope.

In-scope path traversal requires a path to access from a party that does not already have equivalent filesystem access (for example, via cross-origin requests that bypass browser protections, or via a vector that does not rely on `--dangerously-allow-remote-access`).

The `--dangerously-allow-remote-access` flag intentionally disables access restrictions. Vulnerabilities that require this flag to be enabled are generally out of scope, as the flag name itself signals the associated risk.
