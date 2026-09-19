package assertion

import (
	"context"
	"testing"

	"github.com/tapelock/tapelock/internal/cassette"
)

const testSchema = `{
	"type": "object",
	"required": ["choices"],
	"properties": {
		"choices": {
			"type": "array",
			"minItems": 1
		}
	}
}`

func TestSchemaAssertionCheckValid(t *testing.T) {
	a, err := NewSchemaAssertion([]byte(testSchema))
	if err != nil {
		t.Fatalf("NewSchemaAssertion: %v", err)
	}

	it := cassette.Interaction{Response: cassette.ResponseSnapshot{
		Body: `{"choices":[{"finish_reason":"stop"}]}`,
	}}
	if err := a.Check(context.Background(), it); err != nil {
		t.Fatalf("Check: unexpected error: %v", err)
	}
}

func TestSchemaAssertionCheckInvalid(t *testing.T) {
	a, err := NewSchemaAssertion([]byte(testSchema))
	if err != nil {
		t.Fatalf("NewSchemaAssertion: %v", err)
	}

	it := cassette.Interaction{Response: cassette.ResponseSnapshot{
		Body: `{"choices":[]}`, // violates minItems: 1
	}}
	if err := a.Check(context.Background(), it); err == nil {
		t.Fatal("Check: want error for a response violating the schema, got nil")
	}
}

func TestSchemaAssertionCheckMalformedBody(t *testing.T) {
	a, err := NewSchemaAssertion([]byte(testSchema))
	if err != nil {
		t.Fatalf("NewSchemaAssertion: %v", err)
	}

	it := cassette.Interaction{Response: cassette.ResponseSnapshot{Body: `not json`}}
	if err := a.Check(context.Background(), it); err == nil {
		t.Fatal("Check: want error for a non-JSON body, got nil")
	}
}

func TestSchemaAssertionCheckRejectsStreamedResponse(t *testing.T) {
	a, err := NewSchemaAssertion([]byte(testSchema))
	if err != nil {
		t.Fatalf("NewSchemaAssertion: %v", err)
	}

	it := cassette.Interaction{Response: cassette.ResponseSnapshot{Stream: true}}
	if err := a.Check(context.Background(), it); err == nil {
		t.Fatal("Check: want error for a streamed response, got nil")
	}
}

func TestNewSchemaAssertionRejectsInvalidSchema(t *testing.T) {
	if _, err := NewSchemaAssertion([]byte(`not json`)); err == nil {
		t.Fatal("NewSchemaAssertion: want error for a malformed schema document, got nil")
	}
}

// TestNewSchemaAssertionNeverFetchesRemoteRefs is the OWASP requirement
// from the PRD: an unresolved external $ref must be a compile-time error,
// never a network fetch (determinism + SSRF).
func TestNewSchemaAssertionNeverFetchesRemoteRefs(t *testing.T) {
	schema := `{"$ref": "https://example.invalid/never-fetched.json"}`
	if _, err := NewSchemaAssertion([]byte(schema)); err == nil {
		t.Fatal("NewSchemaAssertion: want a compile error for an unresolved external $ref (it must never attempt a network fetch), got nil")
	}
}
