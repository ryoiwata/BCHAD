package gateway

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tiktoken "github.com/pkoukk/tiktoken-go"
	"github.com/valkey-io/valkey-go"
)

const (
	// tokenCacheTTL is how long a token count is cached in Valkey.
	tokenCacheTTL = 24 * time.Hour

	// Pricing per million tokens (USD) — current Anthropic rates.
	haiku35InputPricePerM  = 0.80
	haiku35OutputPricePerM = 4.00
	sonnet4InputPricePerM  = 3.00
	sonnet4OutputPricePerM = 15.00
)

// ErrCostGuardrail is returned when accumulated cost exceeds 2× the projected cost.
type ErrCostGuardrail struct {
	Accumulated float64
	Projected   float64
}

func (e *ErrCostGuardrail) Error() string {
	return fmt.Sprintf("cost guardrail: accumulated $%.4f exceeds 2× projected $%.4f — workflow paused", e.Accumulated, e.Projected)
}

// CostTracker tracks token usage and LLM costs per pipeline run using Valkey.
type CostTracker struct {
	valkey  valkey.Client
	encoder *tiktoken.Tiktoken
}

// NewCostTracker creates a CostTracker backed by Valkey for caching and accumulation.
func NewCostTracker(valkeyClient valkey.Client) (*CostTracker, error) {
	enc, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return nil, fmt.Errorf("tiktoken encoding: %w", err)
	}
	return &CostTracker{valkey: valkeyClient, encoder: enc}, nil
}

// CountTokens counts tokens in the given text, with a 24h Valkey cache keyed by SHA-256.
func (t *CostTracker) CountTokens(ctx context.Context, text string) int {
	hash := sha256.Sum256([]byte(text))
	key := fmt.Sprintf("tokens:%x", hash)

	result := t.valkey.Do(ctx, t.valkey.B().Get().Key(key).Build())
	if result.Error() == nil {
		if n, err := result.AsInt64(); err == nil {
			return int(n)
		}
	}

	tokens := t.encoder.Encode(text, nil, nil)
	count := len(tokens)
	_ = t.valkey.Do(ctx, t.valkey.B().Set().Key(key).Value(fmt.Sprintf("%d", count)).Ex(tokenCacheTTL).Build())

	return count
}

// AccumulateCost adds the cost of one LLM call to the run's running total.
// Returns the new total and an ErrCostGuardrail if it exceeds 2× projectedCost.
func (t *CostTracker) AccumulateCost(ctx context.Context, runID, model string, usage Usage, projectedCost float64) (float64, error) {
	callCost := ComputeCost(model, usage)
	key := fmt.Sprintf("cost:%s", runID)

	result := t.valkey.Do(ctx, t.valkey.B().Incrbyfloat().Key(key).Increment(callCost).Build())
	if result.Error() != nil {
		return 0, fmt.Errorf("accumulate cost in valkey: %w", result.Error())
	}

	total, err := result.AsFloat64()
	if err != nil {
		return 0, fmt.Errorf("parse accumulated cost: %w", err)
	}

	slog.Info("gateway: cost accumulated",
		"run_id", runID,
		"model", model,
		"call_cost_usd", callCost,
		"total_cost_usd", total,
		"projected_cost_usd", projectedCost,
	)

	if projectedCost > 0 && total > 2*projectedCost {
		return total, &ErrCostGuardrail{Accumulated: total, Projected: projectedCost}
	}

	return total, nil
}

// GetAccumulatedCost returns the current accumulated cost for a run, or 0 if not started.
func (t *CostTracker) GetAccumulatedCost(ctx context.Context, runID string) (float64, error) {
	key := fmt.Sprintf("cost:%s", runID)
	result := t.valkey.Do(ctx, t.valkey.B().Get().Key(key).Build())
	if result.Error() != nil {
		return 0, nil // key not set = no cost yet
	}
	v, err := result.AsFloat64()
	if err != nil {
		return 0, nil
	}
	return v, nil
}

// ComputeCost calculates the USD cost of a single LLM API call based on the model and usage.
func ComputeCost(model string, usage Usage) float64 {
	var inputPPM, outputPPM float64
	if isHaiku35(model) {
		inputPPM = haiku35InputPricePerM
		outputPPM = haiku35OutputPricePerM
	} else {
		// Default to Sonnet 4 pricing for any unknown model too.
		inputPPM = sonnet4InputPricePerM
		outputPPM = sonnet4OutputPricePerM
	}
	inputCost := float64(usage.InputTokens) / 1_000_000 * inputPPM
	outputCost := float64(usage.OutputTokens) / 1_000_000 * outputPPM
	return inputCost + outputCost
}

// isHaiku35 reports whether the model identifier refers to Claude Haiku 3.5.
func isHaiku35(model string) bool {
	return strings.Contains(model, "haiku")
}
