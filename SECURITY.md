# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities **privately** — not through a public
issue.

- **Preferred:** use GitHub's [private vulnerability reporting](https://github.com/Amirhat/ChitHub/security/advisories/new).
- Or email **a.h.amani.t@gmail.com**.

Include steps to reproduce and the impact. You'll get an acknowledgement as soon
as possible, and we'll coordinate a fix and disclosure with you.

## Scope

ChitHub runs a local server bound to `127.0.0.1` and drives your own `git`.
Especially relevant reports include: anything reachable from another process on
the machine via the local port, how git commands are constructed from
user/repo-controlled input, and handling of remote URLs or repository contents.

## Supported versions

The latest release receives security fixes.
