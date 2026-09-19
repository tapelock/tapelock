# Tapelock

**Deterministic HTTP proxy and CLI for testing LLM applications.**

Tapelock records HTTP interactions with LLM APIs into JSONL cassettes and
replays them locally without making live API calls.

This makes LLM requests reproducible in tests and CI, without API costs or
provider availability becoming part of the test suite.

> Tapelock doesn't understand LLMs. It understands HTTP, JSON, and a few assertions.

**Status:** early development. [v0.1.0](https://github.com/tapelock/tapelock/releases/tag/v0.1.0)
covers OpenAI Chat Completions only; the CLI and config format will still
change without notice before v1.

## Design principles

* **Local-first**: no backend or database required.
* **Deterministic**: assertions are based on recorded interactions and
  explicit configuration, not model-generated evaluations.
* **Fail-closed**: replay misses and invalid cassettes fail the build.
  There is no silent fallback to a live API.
* **Versioned cassettes**: JSONL, one interaction per line, designed to
  live alongside your tests.

## Features

* Record LLM API interactions as JSONL cassettes
* Replay recorded responses without contacting the provider
* Deterministic request matching with sanitization, JCS, and SHA-256
* Record and replay streaming responses deterministically
* Validate responses against JSON Schema
* Assert token usage and response properties
* Run locally and in CI without a backend

## Quick start

### Install

```bash
go install github.com/tapelock/tapelock/cmd/tapelock@v0.1.0
```

### Record

Point your application's LLM client at the Tapelock proxy:

```bash
tapelock record \
  --cassette cassettes/completion.jsonl \
  --upstream https://api.openai.com
```

The request is forwarded to the upstream API and the interaction is stored
in the cassette.

### Replay

Use the cassette in your tests:

```bash
tapelock replay \
  --cassette cassettes/completion.jsonl
```

Replay never falls back to the upstream API. A cassette miss fails with
exit code `2`.

### Validate

Run assertions against recorded interactions:

```bash
tapelock check \
  --cassette cassettes/completion.jsonl
```

## How it works

```text
Application
     │
     ▼
Tapelock Proxy
     │
     ├──────────────► LLM API
     │
     ▼
JSONL Cassette
     │
     ▼
Deterministic Replay
```

Requests can be sanitized before matching, then canonicalized using
RFC 8785 (JCS) and hashed with SHA-256.

During replay, Tapelock matches the incoming request against the cassette
and returns the recorded response. No live API call is made.

## Documentation

See [docs/](docs/) for the CLI reference, the cassette format, how request
matching works, and the `check` assertions. See [ROADMAP.md](ROADMAP.md)
for what's next.

## Contributing

Contributions are welcome.

```bash
go test ./...
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development and contribution
guidelines.

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
