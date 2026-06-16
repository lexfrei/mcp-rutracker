# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities privately via [GitHub Security Advisories](https://github.com/lexfrei/mcp-rutracker/security/advisories/new). Do not open a public issue for security problems.

I aim to acknowledge a report within a few days and will keep you updated on the fix.

## Scope

This server accepts untrusted input from rutracker (or a mirror, or anything on the connection) and parses HTML and `.torrent` files. Parsing-related crashes (panics, resource exhaustion), credential leakage, and authentication bypasses are in scope. Issues that require a malicious local environment or a compromised host are generally out of scope.

## Handling credentials

Never include credentials, session cookies, or `bb_session` tokens in issues, pull requests, or logs attached to a report.
