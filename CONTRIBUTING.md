# Contributing to Tapelock

Tapelock is in early development (pre-v0.1.0), so expect breaking changes
to the CLI, configuration, and cassette formats.

## Project layout

```text
cmd/tapelock/          CLI entrypoint and commands
internal/engine/       record/replay orchestration
internal/proxy/        net/http adaptation and streaming
internal/cassette/     JSONL cassette storage
internal/fingerprint/  JCS canonicalization and SHA-256 hashing
internal/sanitize/     UUID, timestamp, and custom regex sanitization
internal/assertion/    response assertions
testdata/              cassettes, schemas, and test fixtures
```

The `internal/` packages are implementation details for v0.1.0. Avoid
introducing public Go APIs unless there is a clear reason to do so.

## Development

Run the usual checks before opening a pull request:

```bash
go build ./...
go vet ./...
go test ./...
gofmt -l .
```

`gofmt -l .` should produce no output.

## Design constraints

A few constraints are important to the design of Tapelock:

* Assertions must be deterministic. Tapelock does not use an LLM to
  evaluate another LLM.
* `internal/engine`, `internal/fingerprint`, and `internal/sanitize`
  should remain free of I/O so they can be tested without a network or
  filesystem.
* Replay must never fall back to a live upstream request. A cassette miss
  is an error.
* Streaming responses should be recorded and replayed without
  reconstructing the stream from parsed LLM-specific structures.
* Request matching follows the same pipeline: sanitize, canonicalize with
  RFC 8785 (JCS), then hash with SHA-256.

If a proposed change affects one of these constraints, explain the
trade-off in the pull request.

## Reporting issues

When reporting a bug, include a minimal reproduction whenever possible.

A cassette, configuration snippet, request example, or failing test is
especially useful when it helps reproduce the problem.
