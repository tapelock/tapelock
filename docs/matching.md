# Request matching

Tapelock decides whether two requests are "the same" by hashing them, not by comparing bytes directly. Record and replay must compute this hash identically, or nothing recorded would ever replay.

## Pipeline

```
request body
     |
     v
sanitize (UUIDs, timestamps -> placeholders)
     |
     v
JCS canonicalization (RFC 8785)
     |
     v
SHA-256 of method + path + selected headers + canonical body
     |
     v
sha256:<hex>
```

## Sanitization

Before hashing, the request body's string values are rewritten so that volatile data does not defeat matching:

- UUIDs (the standard 8-4-4-4-12 hyphenated form) become `<UUID>`.
- ISO 8601 / RFC 3339 timestamps become `<TIMESTAMP>`.

Sanitization only ever affects what gets hashed. The bytes actually sent to the upstream, and the bytes stored in the cassette, are never touched.

This means two requests that differ only by an embedded request ID or timestamp hash to the same key and are treated as the same recorded interaction; requests that differ in any other way (different message content, different model) hash differently.

## Canonicalization

Two JSON documents that mean the same thing can be written as different bytes: keys in a different order, or a number written as `1` versus `1.0`. Tapelock canonicalizes the sanitized body with [RFC 8785 (JCS)](https://www.rfc-editor.org/rfc/rfc8785) before hashing, so key order and number formatting never affect the hash. This uses a tested RFC 8785 implementation rather than `encoding/json`, which does not produce a canonical form on its own.

## What's included in the hash

- The HTTP method and path.
- A small, fixed header allowlist (currently just `content-type`). Headers like `Authorization`, `Date`, `User-Agent`, and `Content-Length` are deliberately excluded: hashing them would make replay fragile across harmless client differences.
- The sanitized, canonicalized request body.

## Occurrence

If the same request (same hash) is recorded more than once in a session, for example an agent loop that repeats an identical call, each recording is kept in order. On replay, each `Lookup` for that hash serves the next not-yet-served recording, so a repeated call gets the sequence of responses it was originally recorded with, not just the first one forever.
