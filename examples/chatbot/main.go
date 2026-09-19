// Command chatbot is the "real project" for Tapelock's own dogfood loop:
// a tiny OpenAI Chat Completions client that
// changes nothing about its own code to be recorded or replayed, only
// OPENAI_BASE_URL points it at the Tapelock proxy instead of the real API.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
}

func main() {
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com"
	}
	base = strings.TrimSuffix(base, "/")

	reqBody, err := json.Marshal(chatRequest{
		Model: "gpt-4o-mini",
		Messages: []message{
			{Role: "user", Content: "Say hello to Tapelock in one short sentence."},
		},
	})
	if err != nil {
		log.Fatalf("chatbot: build request: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, base+"/v1/chat/completions", strings.NewReader(string(reqBody)))
	if err != nil {
		log.Fatalf("chatbot: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-not-a-real-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("chatbot: request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("chatbot: read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("chatbot: upstream returned %d: %s", resp.StatusCode, body)
	}

	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Fatalf("chatbot: parse response: %v", err)
	}
	if len(parsed.Choices) == 0 {
		log.Fatalf("chatbot: response had no choices: %s", body)
	}

	fmt.Println(parsed.Choices[0].Message.Content)
}
