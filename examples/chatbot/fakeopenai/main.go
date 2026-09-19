// Command fakeopenai is a minimal, fixed-response stand-in for the OpenAI
// Chat Completions API, used only to record the dogfood cassette in
// examples/chatbot without a real API key. It is never used during replay:
// see examples/chatbot/replay_check.sh, which never starts this.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
)

const fixedReply = "Hello from Tapelock! I am a fixed, fake response used only to record the dogfood cassette."

func main() {
	addr := flag.String("listen", "127.0.0.1:0", "address to listen on")
	flag.Parse()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("fakeopenai: listen on %s: %v", *addr, err)
	}

	fmt.Printf("fakeopenai listening on http://%s\n", ln.Addr())

	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{
  "id": "chatcmpl-fakeopenai-dogfood",
  "object": "chat.completion",
  "model": "gpt-4o-mini",
  "choices": [
    {
      "index": 0,
      "finish_reason": "stop",
      "message": {"role": "assistant", "content": %q}
    }
  ],
  "usage": {"prompt_tokens": 12, "completion_tokens": 18, "total_tokens": 30}
}`, fixedReply)
	})

	log.Fatal(http.Serve(ln, nil))
}
