# Cassette format (JSONL v1)

A cassette is a plain text file: one JSON object per line, one line per recorded interaction. This makes it diffable in code review and safe to commit to git.

## Interaction shape

```json
{
  "version": 1,
  "id": "01J8Z3K1QYVXZ7F5T9N2H6C4R8",
  "request": {
    "method": "POST",
    "url": "/v1/chat/completions",
    "headers": {"content-type": ["application/json"]},
    "body": "{\"model\":\"gpt-4o-mini\",\"messages\":[{\"role\":\"user\",\"content\":\"hi\"}]}"
  },
  "request_hash": "sha256:...",
  "response": {
    "status": 200,
    "headers": {"content-type": ["application/json"]},
    "body": "{\"id\":\"chatcmpl-abc\",\"choices\":[{\"finish_reason\":\"stop\"}]}"
  }
}
```

| Field | Description |
| --- | --- |
| `version` | Cassette format version. Currently always `1`. Any other value fails to decode, so an unreadable cassette never falls back silently |
| `id` | A random identifier for the interaction, not used for matching |
| `request.headers` | The request headers as sent, minus `authorization`, `x-api-key`, and `openai-organization`, which are stripped before this line is ever written |
| `request.body` | The exact bytes sent to the upstream, before sanitization. Sanitization only affects what gets hashed, never what gets stored here or forwarded |
| `request_hash` | `sha256:<hex>`. See [matching.md](matching.md) for how it's computed |
| `response` | The upstream's response, verbatim for a non-streamed interaction |

A missing or malformed field, or an unknown top-level field, makes the whole cassette fail to load rather than being silently ignored.

## Streaming responses

A streamed (SSE) response sets `response.stream: true` and `response.chunks` instead of `response.body`:

```json
{
  "response": {
    "status": 200,
    "headers": {"content-type": ["text/event-stream"]},
    "stream": true,
    "chunks": [
      {"data": "data: {\"delta\":\"Hi\"}\n\n", "delay_ms": 0},
      {"data": "data: {\"delta\":\" there!\"}\n\n", "delay_ms": 42},
      {"data": "data: [DONE]\n\n", "delay_ms": 5}
    ]
  }
}
```

Each chunk is the raw bytes of one read from the upstream, in order, with `delay_ms` measured relative to the previous chunk (or to the start of the response, for the first one). There is no SSE-specific parsing: Tapelock's proxy is byte-oriented, so a chunk boundary reflects whatever the network happened to deliver during recording, not a parsed `event:`/`data:` frame.

On replay, chunks are concatenated and served with no artificial delay between them ("instant" timing). A streamed interaction is only ever written once the upstream response ends cleanly. If the client disconnects mid-stream or the upstream fails partway through, nothing is recorded for that interaction, since a partial recording would be a corrupt, unreplayable fixture.
