# Assertions

`tapelock check` runs a small, fixed set of deterministic assertions against every interaction in a cassette. There is no configuration file yet, so each assertion is a CLI flag; see [cli.md](cli.md) for the full command.

## `--status <code>`

Every interaction's response status must equal `<code>` exactly. Omit the flag to skip this check.

## `--schema <path>`

Validates every non-streamed response body against the JSON Schema (draft 2020-12) at `<path>`.

- Schema compilation never touches the network. An external `$ref` that is not already part of the schema document fails to compile instead of being fetched, which keeps validation deterministic and closed to SSRF.
- A streamed interaction's response is a sequence of raw SSE frames, not a single JSON document, so schema validation is not supported for it yet.

## `--max-prompt-tokens`, `--max-completion-tokens`, `--max-total-tokens`

Each reads the corresponding field from the response body's top-level `usage` object (`usage.prompt_tokens`, `usage.completion_tokens`, `usage.total_tokens`), matching the shape OpenAI-compatible APIs return, and fails if the recorded value exceeds the limit.

If a limit is set but the response has no `usage` object at all (for example, a streamed response, or an upstream that didn't return usage data), the check fails rather than silently passing. A missing value is never treated as within limits.

## Output

`check` reports one line per failing assertion, prefixed with the interaction's `id`, and exits with code `1` if any assertion failed across any interaction. See [cli.md](cli.md#exit-codes) for the full exit code table.
