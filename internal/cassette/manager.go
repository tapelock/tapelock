// Package cassette stores and retrieves recorded interactions as JSONL
// files (one file per cassette, one line per interaction). No database, no
// external index — Append/Lookup/Read over a git-friendly append-only file.
package cassette
