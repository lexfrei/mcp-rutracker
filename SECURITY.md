# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities privately via [GitHub Security Advisories](https://github.com/lexfrei/mcp-rutracker/security/advisories/new). Do not open a public issue for security problems.

I aim to acknowledge a report within a few days and will keep you updated on the fix.

## Scope

This server accepts untrusted input from rutracker (or a mirror, or anything on the connection) and parses HTML and `.torrent` files. Parsing-related crashes (panics, resource exhaustion), credential leakage, and authentication bypasses are in scope. Issues that require a malicious local environment or a compromised host are generally out of scope.

When the HTTP transport is enabled (`MCP_HTTP_PORT`), the server also exposes `GET /artifacts/{id}`, an unauthenticated endpoint that serves a downloaded `.torrent` once per unguessable token. Its only access control is the token's secrecy (256 bits from `crypto/rand`, one-time use, TTL-bounded); there is no per-request authentication by design. Binding a routable interface (`MCP_HTTP_HOST`) without network-level access control is the operator's responsibility — serve artifact URLs only on a trusted network. Token-guessing, token leakage from the server's own logs or responses, and failures of the one-time/expiry guarantees are in scope; the absence of endpoint authentication when an operator deliberately exposes it to an untrusted network is not.

## Handling credentials

Never include credentials, session cookies, or `bb_session` tokens in issues, pull requests, or logs attached to a report.
