package cassette

import (
	"bufio"
	"bytes"
	"os"
	"reflect"
	"testing"
)

func sampleInteraction() Interaction {
	return Interaction{
		Version: CurrentVersion,
		ID:      "01J8Z3K1QYVXZ7F5T9N2H6C4R8",
		Request: RequestSnapshot{
			Method:  "POST",
			URL:     "/v1/chat/completions",
			Headers: Headers{"content-type": {"application/json"}},
			Body:    `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Say hi"}]}`,
		},
		RequestHash: "sha256:b98c5ee7682b720897ee0192d6a669fcc9df23634cbebd5f4021d8d077fde364",
		Response: ResponseSnapshot{
			Status:  200,
			Headers: Headers{"content-type": {"application/json"}},
			Body:    `{"id":"chatcmpl-abc","choices":[{"finish_reason":"stop"}]}`,
		},
	}
}

func sampleStreamInteraction() Interaction {
	it := sampleInteraction()
	it.ID = "01J8Z3K1R2E8G6M4P0S7T3V9WB"
	it.Response = ResponseSnapshot{
		Status:  200,
		Headers: Headers{"content-type": {"text/event-stream"}},
		Stream:  true,
		Chunks: []ResponseChunk{
			{Data: "data: {\"delta\":\"Hi\"}\n\n", DelayMS: 0},
			{Data: "data: {\"delta\":\" there!\"}\n\n", DelayMS: 42},
			{Data: "data: [DONE]\n\n", DelayMS: 5},
		},
	}
	return it
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	for name, it := range map[string]Interaction{
		"non-stream": sampleInteraction(),
		"stream":     sampleStreamInteraction(),
	} {
		t.Run(name, func(t *testing.T) {
			line, err := EncodeLine(it)
			if err != nil {
				t.Fatalf("EncodeLine: %v", err)
			}

			got, err := DecodeLine(line)
			if err != nil {
				t.Fatalf("DecodeLine: %v", err)
			}

			if !reflect.DeepEqual(got, it) {
				t.Fatalf("round trip mismatch:\n got  %+v\n want %+v", got, it)
			}
		})
	}
}

func TestEncodeIsDeterministic(t *testing.T) {
	it := sampleInteraction()

	a, err := EncodeLine(it)
	if err != nil {
		t.Fatalf("EncodeLine: %v", err)
	}
	b, err := EncodeLine(it)
	if err != nil {
		t.Fatalf("EncodeLine: %v", err)
	}

	if !bytes.Equal(a, b) {
		t.Fatalf("EncodeLine is not deterministic:\n a: %s\n b: %s", a, b)
	}
}

func TestEncodeRejectsUnsupportedVersion(t *testing.T) {
	it := sampleInteraction()
	it.Version = CurrentVersion + 1

	if _, err := EncodeLine(it); err == nil {
		t.Fatal("EncodeLine: want error for unsupported version, got nil")
	}
}

func TestDecodeRejectsUnsupportedVersion(t *testing.T) {
	line := []byte(`{"version":999,"id":"x","request":{"method":"GET","url":"/"},"request_hash":"sha256:x","response":{"status":200}}`)

	if _, err := DecodeLine(line); err == nil {
		t.Fatal("DecodeLine: want error for unsupported version, got nil")
	}
}

func TestDecodeRejectsUnknownField(t *testing.T) {
	line := []byte(`{"version":1,"id":"x","request":{"method":"GET","url":"/"},"request_hash":"sha256:x","response":{"status":200},"unexpected":true}`)

	if _, err := DecodeLine(line); err == nil {
		t.Fatal("DecodeLine: want error for unknown field, got nil")
	}
}

func TestDecodeRejectsMalformedJSON(t *testing.T) {
	if _, err := DecodeLine([]byte(`not json`)); err == nil {
		t.Fatal("DecodeLine: want error for malformed JSON, got nil")
	}
}

func TestDecodeGoldenFixture(t *testing.T) {
	f, err := os.Open("../../testdata/cassettes/example.jsonl")
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()

	var got []Interaction
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		it, err := DecodeLine(line)
		if err != nil {
			t.Fatalf("DecodeLine: %v", err)
		}
		got = append(got, it)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d interactions, want 2", len(got))
	}
	if got[0].Response.Stream {
		t.Fatalf("interaction 0: want non-stream response")
	}
	if !got[1].Response.Stream || len(got[1].Response.Chunks) != 3 {
		t.Fatalf("interaction 1: want stream response with 3 chunks, got stream=%v chunks=%d",
			got[1].Response.Stream, len(got[1].Response.Chunks))
	}
}
