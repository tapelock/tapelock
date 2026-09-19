#!/usr/bin/env bash
# Records the dogfood cassette for examples/chatbot against a local, fixed
# fake OpenAI server -- never against the real API, so this needs no
# secret. Run this again whenever chatbot's own request changes on
# purpose; commit the resulting cassette.
set -euo pipefail

DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$DIR/../.." && pwd)"
CASSETTE="$DIR/testdata/cassette.jsonl"

addr_from_log() {
  sed -n 's/.*listening on \(http:\/\/[^[:space:]]*\).*/\1/p' "$1" | head -n1
}

wait_for_addr() {
  local log_file="$1"
  for _ in $(seq 1 50); do
    local addr
    addr="$(addr_from_log "$log_file")"
    if [ -n "$addr" ]; then
      echo "$addr"
      return 0
    fi
    sleep 0.1
  done
  echo "timed out waiting for $log_file to report a listen address" >&2
  cat "$log_file" >&2
  return 1
}

WORKDIR="$(mktemp -d)"
cleanup() {
  [ -n "${RECORD_PID:-}" ] && kill "$RECORD_PID" 2>/dev/null || true
  [ -n "${FAKE_PID:-}" ] && kill "$FAKE_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

rm -f "$CASSETTE"

# Build once and exec the real binaries directly, rather than `go run` in
# the background: `go run` is a wrapper process, and signals sent to it
# (SIGINT below) are not reliably forwarded to the actual compiled child.
go build -o "$WORKDIR/tapelock" "$REPO_ROOT/cmd/tapelock"
go build -o "$WORKDIR/fakeopenai" "$DIR/fakeopenai"
go build -o "$WORKDIR/chatbot" "$DIR"

"$WORKDIR/fakeopenai" --listen 127.0.0.1:0 >"$WORKDIR/fakeopenai.log" 2>&1 &
FAKE_PID=$!
FAKE_ADDR="$(wait_for_addr "$WORKDIR/fakeopenai.log")"

"$WORKDIR/tapelock" record \
  --cassette "$CASSETTE" \
  --upstream "$FAKE_ADDR" \
  --listen 127.0.0.1:0 >"$WORKDIR/record.log" 2>&1 &
RECORD_PID=$!
RECORD_ADDR="$(wait_for_addr "$WORKDIR/record.log")"

echo "Recording via $RECORD_ADDR (fake upstream $FAKE_ADDR)"
OPENAI_BASE_URL="$RECORD_ADDR" "$WORKDIR/chatbot"

kill -INT "$RECORD_PID"
wait "$RECORD_PID"
RECORD_PID=""

echo
echo "Recorded $CASSETTE:"
cat "$CASSETTE"
