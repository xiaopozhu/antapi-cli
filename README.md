# AntAPI CLI

[English](README.md) | [简体中文](README.zh-CN.md)

The official AntAPI command-line client for terminal users, scripts, and [AntAPI Skills](https://github.com/xiaopozhu/antapi-skills). It uses your platform API key and Credits through the public Gateway.

## Status

WHOIS lookup, TLD coverage, profiles, JSON output, and five-platform release-candidate packaging are implemented. The first candidate is `v0.1.0`; a stable release has not been published yet. Development builds report `dev`. Batch jobs, login, and automatic retries are not implemented.

## Usage

```bash
antapi --help
antapi version --json
antapi --profile local whois lookup example.com --json
antapi --profile local whois lookup 例子.中国 --json
antapi --profile local whois tlds --json
```

Root flags `--config PATH`, `--profile NAME`, `--gateway-url ORIGIN`, and `--core-url ORIGIN` override non-secret settings. Put them before `whois`. API keys are never command-line arguments.

WHOIS commands call only `/whois/v1/domains/{domain}` and `/whois/v1/tlds` through the public Gateway. They do not access databases or the internal Own API. The optional `core_url` setting is reserved for future public Catalog checks; WHOIS does not call Core API or send keys there.

Unicode and Punycode domains are supported, including Unicode dot separators (`。`, `．`, `｡`). The API performs full IDNA normalization. The CLI checks input safety and bare-domain structure, rejecting URL schemes, paths, empty labels, spaces, control characters, ports, percent escapes, and queries. Help, version, and argument validation run before configuration, credentials, or networking.

## Configuration

The default configuration is `$XDG_CONFIG_HOME/antapi/config.json` when set, otherwise `antapi/config.json` under Go's `os.UserConfigDir()`: typically `~/.config` on Linux, `~/Library/Application Support` on macOS, and `%AppData%` on Windows. Use `--config` to override the path.

Example local development profile:

```json
{
  "default_profile": "local",
  "profiles": {
    "local": {
      "gateway_url": "http://127.0.0.1:9000",
      "core_url": "http://127.0.0.1:9001",
      "development": true,
      "timeout_seconds": 30
    }
  }
}
```

Production profiles require your deployment's verified HTTPS origin. HTTP is allowed only for localhost or loopback IPs with explicit `development: true`. Origins require a hostname, an optional port in 1–65535, and no user information, query, fragment, or path other than `/`.

Provision `ANTAPI_API_KEY` through your private local environment; do not put the real value in shell history or chat. It takes precedence over `credentials.json` beside the configuration file:

```json
{"profiles":{"local":{"api_key":"<platform-ak>"}}}
```

Replace the placeholder locally. The credentials file must be a regular, non-symlink file with POSIX mode `0600`; never commit it. Windows requires environment credentials because it does not provide this POSIX permission contract.

Requests have a 30-second default timeout (configurable from 1–120 seconds) and a 1 MiB response limit. They do not follow redirects or automatically retry. HTTP/1 and disabled connection reuse prevent transparent transport retries. A timeout or disconnect does not establish whether execution or billing completed; check existing Console request records.

## Output

`--json` writes one JSON object to stdout on success. Omitting it currently produces the same structure. Success requires HTTP 2xx and platform `code: 0`. Simplified illustrative response:

```json
{"code":0,"data":{"domain":{"queried":"example.com"},"registration":{"registered":true}},"metadata":{"http_status":200,"request_id":"req-123","receipt_id":"123","credits_debit_atoms":"100"}}
```

`data` preserves the public API result. IDs, `*_id`/`*_ids`, and `*_atoms` are strings to preserve precision. Metadata includes only available, allowed Request ID, Receipt ID, and Credits atoms. The CLI does not calculate prices or expose the server's raw `error` body.

On failure, stdout is empty. Stderr uses `CLI_CONFIGURATION_ERROR`, `CLI_NETWORK_ERROR`, `CLI_INVALID_RESPONSE`, `CLI_PLATFORM_ERROR`, or `CLI_EXECUTION_FAILED`, optionally with a numeric `api_code` and safe Request ID. Raw exceptions, arguments, keys, and response bodies are excluded.

| Exit code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Configuration, network, or platform error |
| `2` | Invalid arguments |

## Development and release candidates

Requires Go 1.27.2 and Make. Packaging also requires Git, Bash, and Python 3.9+. The CLI uses `urfave/cli/v3` and does not import private server packages. User-facing messages live in `internal/i18n/`. Code comments and engineering notes are Chinese; README files are English and Chinese.

```bash
make verify
make build
bash scripts/build-release.sh v0.1.0 <full-source-commit> /absolute/path/outside-checkout/candidate
```

`make verify` runs formatting checks, tests, vet, and compilation. Tests use local HTTP/TLS servers and temporary Git fixtures, never production keys or paid API calls. `go test -short ./...` skips only the slower five-target cross-compilation test.

The builder requires a clean tracked source tree at the exact 40-character lowercase commit, a canonical `vX.Y.Z` version, and an output directory outside the checkout. It builds from `git archive`, excluding untracked files, with `CGO_ENABLED=0`, Go 1.27.2, `-trimpath`, and the commit's UTC timestamp. It injects `main.version`, `main.commit`, and `main.builtAt`.

Targets: macOS and Linux on amd64/arm64, plus Windows on amd64. Archives are named `antapi_vX.Y.Z_OS_ARCH.tar.gz` (Windows: `.zip`) and contain `antapi` or `antapi.exe` at the root. `SHA256SUMS` covers all five archives. `manifest.json` records the version, commit, timestamp, toolchain, and artifact hashes/targets.

Repeating the same inputs verifies and reuses the existing files; mismatched inputs, hashes, or file sets fail. Packaging does not create tags or publish releases. See the [Chinese README](README.zh-CN.md) for the complete artifact layout.

## Coordinated releases

[AntAPI](https://github.com/xiaopozhu/antapi) coordinates independently versioned API, CLI, and Skills releases: API/Gateway acceptance → CLI → Skills → website. The WHOIS Skill supports stable CLI `>=0.1.0 <1.0.0`. CI is read-only and does not publish on push or tag.

- [Release design](https://github.com/xiaopozhu/antapi/blob/main/docs/design/coordinated-product-release.md)
- [Implementation plan](https://github.com/xiaopozhu/antapi/blob/main/docs/superpowers/plans/2026-10-22-coordinated-product-release.md)
