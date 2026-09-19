package assertion

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/tapelock/tapelock/internal/cassette"
)

// schemaResourceURL is a synthetic, never-dereferenced name under which the
// compiled document is registered with the compiler. It only needs to be a
// stable key for Compiler.AddResource/Compile. It is never fetched.
const schemaResourceURL = "tapelock://schema"

// SchemaAssertion validates a response body against a compiled JSON Schema
// (draft 2020-12 by default). The compiler is given no URLLoader, so an
// external $ref that isn't already part of the schema document fails to
// compile instead of being fetched over the network, so resolution stays
// deterministic and immune to SSRF (see the PRD's note on this).
type SchemaAssertion struct {
	schema *jsonschema.Schema
}

// NewSchemaAssertion compiles schemaJSON once, so Check can reuse it for
// every interaction without recompiling.
func NewSchemaAssertion(schemaJSON []byte) (*SchemaAssertion, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaJSON))
	if err != nil {
		return nil, fmt.Errorf("assertion: schema: parse: %w", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaResourceURL, doc); err != nil {
		return nil, fmt.Errorf("assertion: schema: add resource: %w", err)
	}

	sch, err := c.Compile(schemaResourceURL)
	if err != nil {
		return nil, fmt.Errorf("assertion: schema: compile: %w", err)
	}

	return &SchemaAssertion{schema: sch}, nil
}

// Check validates the interaction's response body against the schema.
// Streamed interactions are not supported: their body is a sequence of raw
// SSE frames, not a single JSON document (see cassette.ResponseSnapshot).
func (a *SchemaAssertion) Check(ctx context.Context, it cassette.Interaction) error {
	if it.Response.Stream {
		return fmt.Errorf("assertion: schema: streamed responses are not supported yet")
	}

	instance, err := jsonschema.UnmarshalJSON(strings.NewReader(it.Response.Body))
	if err != nil {
		return fmt.Errorf("assertion: schema: response body is not valid JSON: %w", err)
	}

	if err := a.schema.Validate(instance); err != nil {
		return fmt.Errorf("assertion: schema: %w", err)
	}
	return nil
}
