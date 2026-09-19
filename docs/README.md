# Tapelock docs

- [CLI reference](cli.md): `record`, `replay`, `check`, flags, exit codes
- [Cassette format](cassette-format.md): the JSONL v1 schema, redaction, streaming
- [Request matching](matching.md): how a request becomes a fingerprint (sanitize, then JCS, then SHA-256)
- [Assertions](assertions.md): the `check` flags, `--status`, `--schema`, `--max-*-tokens`

For the project's status and a 30-second usage example, see the [README](../README.md).
For what's planned next, see [ROADMAP.md](../ROADMAP.md).
For the GitHub Action, see [tapelock/action](https://github.com/tapelock/action).
