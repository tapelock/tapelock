package assertion

import (
	"context"
	"errors"
	"testing"

	"github.com/tapelock/tapelock/internal/cassette"
)

func usageInteraction(body string) cassette.Interaction {
	return cassette.Interaction{Response: cassette.ResponseSnapshot{Body: body}}
}

func TestUsageAssertionCheckWithinLimits(t *testing.T) {
	a := UsageAssertion{MaxPromptTokens: 100, MaxCompletionTokens: 50, MaxTotalTokens: 150}
	it := usageInteraction(`{"usage":{"prompt_tokens":80,"completion_tokens":40,"total_tokens":120}}`)

	if err := a.Check(context.Background(), it); err != nil {
		t.Fatalf("Check: unexpected error: %v", err)
	}
}

func TestUsageAssertionCheckExceedsLimit(t *testing.T) {
	a := UsageAssertion{MaxCompletionTokens: 50}
	it := usageInteraction(`{"usage":{"prompt_tokens":10,"completion_tokens":51,"total_tokens":61}}`)

	if err := a.Check(context.Background(), it); err == nil {
		t.Fatal("Check: want error when completion_tokens exceeds the max, got nil")
	}
}

func TestUsageAssertionCheckNoLimitsConfiguredAlwaysPasses(t *testing.T) {
	a := UsageAssertion{}
	it := usageInteraction(`{"usage":{"prompt_tokens":999999}}`)

	if err := a.Check(context.Background(), it); err != nil {
		t.Fatalf("Check: unexpected error with no configured limits: %v", err)
	}
}

func TestUsageAssertionCheckMissingUsageIsUnavailableNotPass(t *testing.T) {
	a := UsageAssertion{MaxTotalTokens: 100}
	it := usageInteraction(`{"choices":[]}`) // no "usage" key at all

	err := a.Check(context.Background(), it)
	if err == nil {
		t.Fatal("Check: want an error when usage data is missing (must never silently pass), got nil")
	}
	var unavailable *ErrUsageUnavailable
	if !errors.As(err, &unavailable) {
		t.Fatalf("Check: want *ErrUsageUnavailable, got %T: %v", err, err)
	}
}

func TestUsageAssertionCheckStreamedResponseIsUnavailable(t *testing.T) {
	a := UsageAssertion{MaxTotalTokens: 100}
	it := cassette.Interaction{Response: cassette.ResponseSnapshot{Stream: true}}

	err := a.Check(context.Background(), it)
	var unavailable *ErrUsageUnavailable
	if !errors.As(err, &unavailable) {
		t.Fatalf("Check: want *ErrUsageUnavailable for a streamed response, got %T: %v", err, err)
	}
}

func TestUsageAssertionCheckMalformedBodyIsUnavailable(t *testing.T) {
	a := UsageAssertion{MaxTotalTokens: 100}
	it := usageInteraction(`not json`)

	err := a.Check(context.Background(), it)
	var unavailable *ErrUsageUnavailable
	if !errors.As(err, &unavailable) {
		t.Fatalf("Check: want *ErrUsageUnavailable for a non-JSON body, got %T: %v", err, err)
	}
}
