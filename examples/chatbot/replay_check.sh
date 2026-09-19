#!/usr/bin/env bash
# The dogfood check itself: replay the committed
# cassette, run chatbot against it exactly as it would run against the
# real API, and fail if either the app errored or tapelock reports a
# cassette miss -- meaning chatbot's request no longer matches the
# recorded baseline (e.g. its prompt changed without re-recording).
#
# No fake or real upstream is started here on purpose: a real regression
# test must prove replay never needs one.
set -uo pipefail

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
cleanup() { rm -rf "$WORKDIR"; }
trap cleanup EXIT

# Build once and exec the real binaries directly, rather than `go run` in
# the background: `go run` is a wrapper process, and signals sent to it
# (SIGINT below) are not reliably forwarded to the actual compiled child.
if ! go build -o "$WORKDIR/tapelock" "$REPO_ROOT/cmd/tapelock"; then
  echo "failed to build tapelock" >&2
  exit 1
fi
if ! go build -o "$WORKDIR/chatbot" "$DIR"; then
  echo "failed to build chatbot" >&2
  exit 1
fi

"$WORKDIR/tapelock" replay \
  --cassette "$CASSETTE" \
  --listen 127.0.0.1:0 >"$WORKDIR/replay.log" 2>&1 &
REPLAY_PID=$!

if ! REPLAY_ADDR="$(wait_for_addr "$WORKDIR/replay.log")"; then
  kill "$REPLAY_PID" 2>/dev/null || true
  exit 1
fi

echo "Replaying via $REPLAY_ADDR"
if APP_OUTPUT="$(OPENAI_BASE_URL="$REPLAY_ADDR" "$WORKDIR/chatbot" 2>&1)"; then
  APP_STATUS=0
else
  APP_STATUS=$?
fi
echo "$APP_OUTPUT"

kill -INT "$REPLAY_PID"
if wait "$REPLAY_PID"; then
  REPLAY_STATUS=0
else
  REPLAY_STATUS=$?
fi

if [ "$APP_STATUS" -ne 0 ]; then
  echo "chatbot exited with $APP_STATUS" >&2
  exit 1
fi

if [ "$REPLAY_STATUS" -ne 0 ]; then
  echo "tapelock replay exited with $REPLAY_STATUS: chatbot's request no longer matches the recorded cassette (see $CASSETTE). Re-record with ./record.sh if this change was intentional." >&2
  exit "$REPLAY_STATUS"
fi

echo "OK: chatbot's output matched the recorded cassette."
