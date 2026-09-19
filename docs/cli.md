# CLI reference

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Assertion failure (`check`) |
| 2 | Cassette miss occurred during a `replay` run |
| 3 | Configuration error (bad or missing flags, invalid cassette, invalid upstream) |
| 4 | Internal error (an unexpected failure, not caused by your input) |

## `tapelock record`

Proxies an application's requests to a real upstream API and durably records every interaction to a cassette.

```bash
tapelock record --cassette <path> --upstream <url> [--listen <addr>]
```

| Flag | Required | Default | Description |
| --- | --- | --- | --- |
| `--cassette` | yes | | Path to the cassette file to write |
| `--upstream` | yes | | Upstream API URL, for example `https://api.openai.com` |
| `--listen` | no | `127.0.0.1:0` | Address to listen on. The default picks a free port and prints the actual address once bound |

Point your application at the printed address (for OpenAI SDKs, set `OPENAI_BASE_URL`). The command runs until it receives `SIGINT` or `SIGTERM`, then shuts down cleanly.

Auth headers (`Authorization`, `X-Api-Key`, `Openai-Organization`) are stripped from what gets written to the cassette. They are still forwarded to the real upstream so the request actually authenticates.

## `tapelock replay`

Serves recorded responses from a cassette. It never makes a network call: a request that does not match anything recorded is a deterministic miss, not a fallback to a live API.

```bash
tapelock replay --cassette <path> [--listen <addr>]
```

| Flag | Required | Default | Description |
| --- | --- | --- | --- |
| `--cassette` | yes | | Path to the cassette file to read |
| `--listen` | no | `127.0.0.1:0` | Address to listen on |

On a miss, the HTTP response is `502` with an `X-Tapelock-Miss: 1` header. If any request misses during the process's lifetime, the process exits with code `2` once it is stopped (`SIGINT`/`SIGTERM`), even though the miss itself only failed that one HTTP response.

## `tapelock check`

Validates every interaction in a cassette against the assertions passed as flags. See [assertions.md](assertions.md) for details on each one.

```bash
tapelock check --cassette <path> [--status <code>] [--schema <path>] \
  [--max-prompt-tokens <n>] [--max-completion-tokens <n>] [--max-total-tokens <n>]
```

`--cassette` can point to a single `.jsonl` file. To check a directory of cassette files in one command, use the [GitHub Action](https://github.com/tapelock/action), which loops over every `.jsonl` file in the given directory.
