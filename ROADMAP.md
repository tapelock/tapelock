# Roadmap

This tracks what Tapelock's CLI and GitHub Action actually do today, and what's planned next. It replaces the day-by-day build plan used to get to v0.1.0.

## v0.1.0 (shipped)

- Deterministic fingerprinting: sanitize (UUIDs, timestamps) then RFC 8785 (JCS) canonicalization then SHA-256.
- JSONL v1 cassette format, diffable and safe to commit.
- `tapelock record`: proxies to a real upstream and durably records every interaction, with auth headers redacted before they touch disk.
- `tapelock replay`: serves recorded responses only. A miss is a deterministic failure, never a fallback to a live API.
- Streaming (SSE) support for both record and replay, byte-oriented, no SSE-specific parsing.
- `tapelock check`: `--status`, `--schema` (JSON Schema draft 2020-12, no network `$ref` resolution), and token usage limits.
- Exit codes that map to something a CI script can act on.
- A GitHub Action (`tapelock/action`) that installs the pinned CLI and runs `tapelock check`, turning its exit code into a passing or failing job.
- `examples/chatbot`: Tapelock dogfooding itself, proving that pointing `OPENAI_BASE_URL` at the proxy needs no other code change.

See [docs/](docs/) for how each of these actually works.

## Next

Roughly in priority order, though nothing here is scheduled yet:

- **Cross-platform binary distribution.** The only install path today is
  `go install`, which needs the Go toolchain, real friction for the JS/TS
  and Python developers who make up most of the LLM app ecosystem. The
  proxy itself is already language-agnostic (it intercepts HTTP, not Go
  code), so this is a packaging problem, not an architecture one: build
  binaries per platform with goreleaser, then ship thin npm and pip
  wrapper packages that download and exec the right one, the same pattern
  tools like esbuild and wrangler use.
- **Anthropic support.** The provider boundary was designed for this from the start; the work is a second adapter and a second CI contract test.
- **More assertions.** `finish_reason` (allowlist), `tool_calls` (name allowlist plus schema-validated arguments), and `latency` (TTFB-based, since it's more stable than total time).
- **Schema and usage assertions for streamed responses.** Both currently refuse to run on a streamed interaction, since there's no single JSON document to parse yet.
- **A `tapelock.yaml` config file.** Assertions and matching rules are CLI flags for now; a config file is the natural next step once the flag surface stops changing shape every week.
- **Drift detection.** Re-run cassette requests against a live upstream on a schedule (never on every PR), compare a fingerprint (JSON shape, tool names, `finish_reason`, usage bucket) against an accepted baseline, and flag when the model's behavior has moved.
- **PR comments from the GitHub Action.** Right now the Action only turns an exit code into a pass/fail job. A sticky PR comment summarizing what failed is a real UX improvement, not part of the core path that had to ship first.

## Out of scope for now

Additional providers beyond OpenAI and Anthropic, and anything that would require a backend (a dashboard, hosted drift history, team features), are not part of this roadmap. The CLI and Action stay local-first and self-contained.
