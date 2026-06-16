# mcp-rutracker

MCP server for [RuTracker](https://rutracker.org). Search torrents, inspect topics, list the files inside a torrent without downloading it, resolve magnet links, and download `.torrent` files — all from any MCP-compatible client.

## Why another one?

There are already several RuTracker MCP servers. This one exists because I wanted a particular set of features, wanted it in Go, and — honestly — a fair bit of not-invented-here. So it leans into a few things the alternatives don't.

## Highlights

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
- **rutracker_download** — download the `.torrent` as base64 (ready to hand to a BitTorrent client), enriched with the contained file list and info-hash decoded from the bytes; optionally saved to disk.
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
| `RUTRACKER_DOWNLOAD_DIR` | Directory for `saveToDisk` downloads | — |
| `MCP_HTTP_PORT` | Enable the HTTP transport on this port | stdio only |
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

The base64 returned by `rutracker_download` is directly compatible with the `metainfo` parameter of a sibling Transmission MCP server, so a search result can be downloaded and added to a torrent client in one chain.

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

## See also

Other RuTracker MCP servers worth knowing about: [Zhurik/rutracker-mcp](https://github.com/Zhurik/rutracker-mcp), [carrysauce/rutracker-mcp-server](https://github.com/carrysauce/rutracker-mcp-server), [wildcar/rutracker-torrent-mcp](https://github.com/wildcar/rutracker-torrent-mcp), and [pgagarinov/cc-rutracker-mcp](https://github.com/pgagarinov/cc-rutracker-mcp).

## License

BSD 3-Clause. See [LICENSE](LICENSE).
