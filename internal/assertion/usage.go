package assertion

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tapelock/tapelock/internal/cassette"
)

// UsageAssertion checks token usage read from an OpenAI-compatible response
// body's top-level "usage" object. A limit left at 0 is unset and skipped;
// leaving all three at 0 makes Check always pass.
type UsageAssertion struct {
	MaxPromptTokens     int
	MaxCompletionTokens int
	MaxTotalTokens      int
}

type usageBody struct {
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// ErrUsageUnavailable means a limit was configured but the response has no
// usage data to check it against. It is a distinct type from an exceeded
// limit so a caller can render it as "unknown" — the PRD is explicit that
// missing usage data must never be treated as a silent pass.
type ErrUsageUnavailable struct {
	Reason string
}

func (e *ErrUsageUnavailable) Error() string {
	return "assertion: usage: unavailable: " + e.Reason
}

func (a UsageAssertion) Check(ctx context.Context, it cassette.Interaction) error {
	if a.MaxPromptTokens == 0 && a.MaxCompletionTokens == 0 && a.MaxTotalTokens == 0 {
		return nil
	}

	if it.Response.Stream {
		// usage, when present at all, is a JSON payload buried in one SSE
		// frame (stream_options.include_usage) — extracting it needs
		// provider-specific SSE parsing this package doesn't do.
		return &ErrUsageUnavailable{Reason: "response is streamed"}
	}

	var body usageBody
	if err := json.Unmarshal([]byte(it.Response.Body), &body); err != nil {
		return &ErrUsageUnavailable{Reason: fmt.Sprintf("response body is not valid JSON: %v", err)}
	}
	if body.Usage == nil {
		return &ErrUsageUnavailable{Reason: `response has no "usage" object`}
	}

	if a.MaxPromptTokens > 0 && body.Usage.PromptTokens > a.MaxPromptTokens {
		return fmt.Errorf("assertion: usage: prompt_tokens %d exceeds max %d", body.Usage.PromptTokens, a.MaxPromptTokens)
	}
	if a.MaxCompletionTokens > 0 && body.Usage.CompletionTokens > a.MaxCompletionTokens {
		return fmt.Errorf("assertion: usage: completion_tokens %d exceeds max %d", body.Usage.CompletionTokens, a.MaxCompletionTokens)
	}
	if a.MaxTotalTokens > 0 && body.Usage.TotalTokens > a.MaxTotalTokens {
		return fmt.Errorf("assertion: usage: total_tokens %d exceeds max %d", body.Usage.TotalTokens, a.MaxTotalTokens)
	}
	return nil
}
