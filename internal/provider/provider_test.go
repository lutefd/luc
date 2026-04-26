package provider

import "testing"

func TestIsToolLimitReasonRecognizesProviderVariants(t *testing.T) {
	cases := []string{
		"exceeded_tool_limits",
		"exceeded tool limits",
		"exceeded tool loop limit",
		"tool loop limit exceeded",
		"maximum tool calls reached",
		"too many function calls",
		"TOOL-CALL-LIMIT-EXCEEDED",
	}
	for _, tc := range cases {
		if !IsToolLimitReason(tc) {
			t.Fatalf("expected %q to be recognized as tool limit", tc)
		}
	}
}

func TestIsToolLimitReasonRejectsUnrelatedErrors(t *testing.T) {
	cases := []string{
		"",
		"context deadline exceeded",
		"rate limit exceeded",
		"maximum tokens reached",
		"tool read failed",
		"tool failed because context limit exceeded",
		"context limit exceeded while calling tool",
	}
	for _, tc := range cases {
		if IsToolLimitReason(tc) {
			t.Fatalf("expected %q to be ignored", tc)
		}
	}
}
