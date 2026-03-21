package gateway

import (
	"testing"
)

func TestComputeCost_Haiku(t *testing.T) {
	usage := Usage{InputTokens: 25000, OutputTokens: 5000}
	cost := ComputeCost("claude-haiku-3-5-sonnet-latest", usage)

	// Expected: (25000/1M * $0.80) + (5000/1M * $4.00) = $0.02 + $0.02 = $0.04
	const want = 0.04
	const epsilon = 0.001
	if diff := cost - want; diff < -epsilon || diff > epsilon {
		t.Errorf("ComputeCost(haiku, 25k/5k) = $%.5f, want ~$%.5f", cost, want)
	}
}

func TestComputeCost_Sonnet(t *testing.T) {
	usage := Usage{InputTokens: 60000, OutputTokens: 15000}
	cost := ComputeCost("claude-sonnet-4-20250514", usage)

	// Expected: (60000/1M * $3.00) + (15000/1M * $15.00) = $0.18 + $0.225 = $0.405
	const wantMin, wantMax = 0.38, 0.42
	if cost < wantMin || cost > wantMax {
		t.Errorf("ComputeCost(sonnet, 60k/15k) = $%.5f, want $%.3f–$%.3f", cost, wantMin, wantMax)
	}
}

func TestComputeCost_ZeroUsage(t *testing.T) {
	cost := ComputeCost("claude-sonnet-4-20250514", Usage{})
	if cost != 0 {
		t.Errorf("ComputeCost with zero usage = $%.5f, want $0.00", cost)
	}
}

func TestComputeCost_HaikuDetection(t *testing.T) {
	// Any model containing "haiku" should use haiku pricing.
	haikuModels := []string{
		"claude-haiku-3-5-sonnet-latest",
		"claude-haiku-4-5-20251001",
		"haiku",
	}
	for _, model := range haikuModels {
		if !isHaiku35(model) {
			t.Errorf("isHaiku35(%q) = false, want true", model)
		}
	}

	// Non-haiku models should not match.
	nonHaikuModels := []string{
		"claude-sonnet-4-20250514",
		"claude-opus-4-20250514",
	}
	for _, model := range nonHaikuModels {
		if isHaiku35(model) {
			t.Errorf("isHaiku35(%q) = true, want false", model)
		}
	}
}

func TestErrCostGuardrail_Error(t *testing.T) {
	err := &ErrCostGuardrail{Accumulated: 5.50, Projected: 2.00}
	msg := err.Error()
	if msg == "" {
		t.Error("ErrCostGuardrail.Error() returned empty string")
	}
	// Should mention the values.
	if len(msg) < 10 {
		t.Errorf("ErrCostGuardrail.Error() too short: %q", msg)
	}
}
