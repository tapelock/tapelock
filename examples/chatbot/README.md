# chatbot: Tapelock's own dogfood loop

This is Tapelock testing itself : a minimal
OpenAI-style client (`main.go`) that only ever knows about
`OPENAI_BASE_URL` (the same way a real app would use Tapelock without
changing its own code).

* `record.sh` builds a fixed, fake OpenAI-compatible server
  (`fakeopenai/`), records one interaction through `tapelock record`, and
  overwrites `testdata/cassette.jsonl`. No API key needed, nothing here
  ever calls the real OpenAI API. Run it again and commit the result
  whenever you change chatbot's own request on purpose.
* `replay_check.sh` is the actual regression test: it replays
  `testdata/cassette.jsonl` and runs chatbot against it. No upstream, real
  or fake, is started for this. If chatbot's request no longer matches
  what's recorded (its prompt changed, for instance), this exits non-zero
  with a cassette miss instead of silently drifting. `.github/workflows/dogfood.yml`
  runs this on every push and pull request.

To see it fail on purpose: edit the prompt string in `main.go`, run
`./replay_check.sh`, watch it report a cassette miss, then revert (or run
`./record.sh` again if the change was intentional).
