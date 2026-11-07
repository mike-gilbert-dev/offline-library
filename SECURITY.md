# Security policy

## Reporting a vulnerability

Please **don't open a public issue** for security problems. Report them
privately through GitHub's
[private vulnerability reporting](https://github.com/mike-gilbert-dev/offline-library/security/advisories/new)
(the **Security** tab → **Report a vulnerability**).

Include what you found, how to reproduce it, and which version you tested
(shown at the bottom of the library page and in **Settings**). You'll get a
reply as soon as possible; this is a small project maintained in spare time.

## Supported versions

Only the latest release receives fixes. Install updates from **Settings →
Check for updates**.

## What's in scope

- The launcher's local server: its loopback-only binding, access token and
  cookie, Host and Origin checks, and file serving from packs.
- The pack content origin and its sandboxing.
- The updater: release signature checks and file replacement.
- Message history storage.

Problems in third-party packages should also be reported upstream
(see `THIRD_PARTY_NOTICES.md`).
