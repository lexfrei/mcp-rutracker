# mcp-rutracker

[![CI](https://github.com/lexfrei/mcp-rutracker/actions/workflows/ci.yml/badge.svg)](https://github.com/lexfrei/mcp-rutracker/actions/workflows/ci.yml) [![Release](https://img.shields.io/github/v/release/lexfrei/mcp-rutracker?sort=semver)](https://github.com/lexfrei/mcp-rutracker/releases) [![Go Report Card](https://goreportcard.com/badge/github.com/lexfrei/mcp-rutracker)](https://goreportcard.com/report/github.com/lexfrei/mcp-rutracker) [![Go](https://img.shields.io/github/go-mod/go-version/lexfrei/mcp-rutracker)](go.mod) [![License](https://img.shields.io/github/license/lexfrei/mcp-rutracker)](LICENSE)

MCP server for [RuTracker](https://rutracker.org). Search torrents, inspect topics, list the files inside a torrent without downloading it, resolve magnet links, and download `.torrent` files — all from any MCP-compatible client.

## Why another one?

There are already several RuTracker MCP servers. This one exists because I wanted a particular set of features, wanted it in Go, and — honestly — a fair bit of not-invented-here. So it leans into a few things the alternatives don't.

## Highlights

- **Agent-friendly downloads** — `rutracker_download` returns a one-time, expiring download URL (or compact metadata) instead of dumping a giant base64 blob into the model's context. The `.torrent` is held in memory and served once over an internal URL.
- File listing **without downloading the `.torrent`** — the cheap `viewtorrent.php` endpoint, with exact per-file sizes.
- Mirror round-robin over `rutracker.org` / `rutracker.net` with automatic failover on network and `5xx` errors.
- Canonical info-hash computed from the torrent's own bencode (and parsed from magnet links).
- windows-1251 handling in both directions, and a small self-contained bencode decoder.
- Distroless multi-arch container image, signed with cosign.

## Features

- **rutracker_search** — search torrents by keywords, optionally restricted to a forum and sorted by seeders, size, date, or downloads. Each result carries the topic ID, title, forum, exact size, seeders/leechers, downloads, author, and date.
- **rutracker_topic_info** — detailed topic view: title, exact size, seeders/leechers, info-hash, magnet link, and description.
- **rutracker_files** — list the files inside a torrent with exact per-file sizes, read from the topic page **without downloading the `.torrent`**.
- **rutracker_magnet** — resolve the magnet link and info-hash for a topic.
- **rutracker_download** — fetch a topic's `.torrent`, enriched with its file list, info-hash, and sha256. Pick how it is delivered with `mode`: `metadata` (info only), `base64` (inline content for piping to a torrent client), or `artifact` (a one-time, expiring download URL). The default is `artifact` when the HTTP transport is enabled, otherwise `metadata` — never a giant base64 blob unless you ask for it.
- **rutracker_server_version** — report the server version, revision, and Go runtime.

## Mirrors and resilience

RuTracker is frequently unreachable on a single host (rutracker.org often answers `52x` from behind Cloudflare). With no `RUTRACKER_BASE_URL` set, the client round-robins between `rutracker.org` and `rutracker.net`, failing over automatically on network and `5xx` errors. Pin a single mirror by setting `RUTRACKER_BASE_URL`.

All site pages are decoded from windows-1251, and Cyrillic search queries are encoded back to windows-1251, so non-Latin searches work correctly.

## Configuration

Configuration is read from environment variables. Either username/password or a cookie override is required to authenticate.

| Variable | Description | Default |
| --- | --- | --- |
| `RUTRACKER_USERNAME` | Login username | — |
| `RUTRACKER_PASSWORD` | Login password | — |
| `RUTRACKER_COOKIE` | Raw `bb_session=...` cookie, used instead of a password login (bypasses captcha) | — |
| `RUTRACKER_COOKIE_FILE` | Path to persist the session between runs | `~/.mcp-rutracker/cookies.json` (bare process); `/home/nobody/.mcp-rutracker/cookies.json` (container, set in the image) |
| `RUTRACKER_BASE_URL` | Pin a single mirror (e.g. `https://rutracker.net/forum/`) | round-robin org/net |
| `RUTRACKER_USER_AGENT` | Override the browser User-Agent | recent Chrome |
| `RUTRACKER_PROXY` | HTTP/SOCKS5 proxy URL | — |
| `RUTRACKER_ARTIFACT_BASE_URL` | Externally reachable base for artifact download URLs (e.g. `http://mcp-rutracker.internal:9090`) | derived from the HTTP address |
| `RUTRACKER_ARTIFACT_TTL` | How long an artifact download URL stays valid (Go duration) | `15m` |
| `MCP_HTTP_PORT` | Enable the HTTP transport on this port (required for artifact downloads) | stdio only |
| `MCP_HTTP_HOST` | HTTP bind host | `127.0.0.1` |

### Captcha

When rutracker demands a captcha during login, the server returns a structured error. Obtain a `bb_session` cookie from a browser and pass it via `RUTRACKER_COOKIE` to continue.

## Usage

With Claude Code, via the bundled `.mcp.json` (Docker):

```json
{
  "mcpServers": {
    "mcp-rutracker": {
      "command": "docker",
      "args": [
        "run", "--rm", "-i",
        "-e", "RUTRACKER_USERNAME",
        "-e", "RUTRACKER_PASSWORD",
        "-e", "RUTRACKER_COOKIE",
        "-e", "RUTRACKER_BASE_URL",
        "-v", "mcp-rutracker-session:/home/nobody/.mcp-rutracker",
        "ghcr.io/lexfrei/mcp-rutracker:latest"
      ],
      "env": {
        "RUTRACKER_USERNAME": "your-username",
        "RUTRACKER_PASSWORD": "your-password"
      }
    }
  }
}
```

The bundled `.mcp.json` ships with empty `RUTRACKER_USERNAME`/`RUTRACKER_PASSWORD` values — fill them in (or pass a `RUTRACKER_COOKIE`) before use. The server exits with an error at startup when no credentials are configured. The named volume persists the session cookie across `--rm` container runs; drop it if you do not want persistence.

## Downloads

`rutracker_download` avoids putting large `.torrent` payloads into the model's context. Choose delivery with the `mode` parameter:

- `metadata` — filename, size, and sha256, plus the info-hash and file list when the `.torrent` parses (no content).
- `base64` — the above plus the inline base64 content, directly compatible with the `metainfo` parameter of a sibling Transmission MCP server.
- `artifact` — the above plus a one-time `downloadUrl` and `expiresAt`. The `.torrent` is held in memory and served once from `GET /artifacts/{id}` on the HTTP transport, so a torrent client (or a human) can fetch it by URL. The response carries an `X-Content-Sha256` header equal to the tool's `sha256`, so a fetcher can detect a corrupted or truncated transfer — it attests the bytes the server holds, not the upstream torrent's correctness. Requires `MCP_HTTP_PORT`. See the reachability note below.

The default is `artifact` when the HTTP transport is enabled, otherwise `metadata`. The download URL is an unguessable bearer capability, consumed on the first fetch attempt (a failed transfer burns it too — request a fresh download) and valid until it expires (`RUTRACKER_ARTIFACT_TTL`) — serve it only on a trusted network. The in-memory store is bounded: under an extreme burst of unfetched downloads, `artifact` mode returns an error rather than silently evicting a still-valid URL, so retry once earlier downloads are fetched or expire.

By default the URL is derived from the HTTP bind address, which is `http://127.0.0.1:<port>` — reachable only from the same host. To let another host fetch it, bind a routable interface (`MCP_HTTP_HOST`) and set `RUTRACKER_ARTIFACT_BASE_URL` to the externally reachable base (e.g. `http://mcp-rutracker.internal:9090`).

## Development

```bash
go build ./cmd/mcp-rutracker
go test -race ./...
golangci-lint run
```

An opt-in live integration test exercises the full flow against the real site:

```bash
RUTRACKER_LIVE=1 RUTRACKER_USERNAME=... RUTRACKER_PASSWORD=... \
  RUTRACKER_BASE_URL=https://rutracker.net/forum/ \
  go test -run TestLive -count=1 ./internal/rutracker/
```

## Support

If this project is useful to you, you can support its development via [GitHub Sponsors](https://github.com/sponsors/lexfrei).

## See also

Other RuTracker MCP servers worth knowing about: [Zhurik/rutracker-mcp](https://github.com/Zhurik/rutracker-mcp), [carrysauce/rutracker-mcp-server](https://github.com/carrysauce/rutracker-mcp-server), [wildcar/rutracker-torrent-mcp](https://github.com/wildcar/rutracker-torrent-mcp), and [pgagarinov/cc-rutracker-mcp](https://github.com/pgagarinov/cc-rutracker-mcp).

## License

BSD 3-Clause. See [LICENSE](LICENSE).
