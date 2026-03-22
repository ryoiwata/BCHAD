package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/valkey-io/valkey-go"
)

const (
	// maxRetriesOn429 is the maximum number of 429 retries before failing.
	maxRetriesOn429 = 5
	// maxRetriesOn5xx is the maximum number of 5xx retries before failing.
	maxRetriesOn5xx = 3
	// defaultRateLimitPerMin is the per-model request limit if none is configured.
	defaultRateLimitPerMin = 60
)

// RateLimiter enforces per-model request rate limits using Valkey sliding-window counters.
type RateLimiter struct {
	valkey      valkey.Client
	limitPerMin int
}

// NewRateLimiter creates a RateLimiter with the given per-minute limit per model.
func NewRateLimiter(valkeyClient valkey.Client, limitPerMin int) *RateLimiter {
	if limitPerMin <= 0 {
		limitPerMin = defaultRateLimitPerMin
	}
	return &RateLimiter{valkey: valkeyClient, limitPerMin: limitPerMin}
}

// Wait checks the rate limit for the given model and blocks if the limit is reached.
// Uses a 1-minute sliding window keyed by model + minute epoch.
func (r *RateLimiter) Wait(ctx context.Context, model string) error {
	window := time.Now().Unix() / 60
	key := fmt.Sprintf("rate:%s:%d", model, window)

	result := r.valkey.Do(ctx, r.valkey.B().Incr().Key(key).Build())
	if result.Error() != nil {
		// Fail open if Valkey is unavailable.
		slog.Warn("gateway: rate limit check failed, allowing request",
			"model", model,
			"error", result.Error(),
		)
		return nil
	}

	count, err := result.AsInt64()
	if err != nil {
		return nil
	}

	// Set TTL on first increment so stale keys are cleaned up.
	if count == 1 {
		_ = r.valkey.Do(ctx, r.valkey.B().Expire().Key(key).Seconds(120).Build())
	}

	if int(count) > r.limitPerMin {
		sleepUntil := time.Unix((window+1)*60, 0)
		wait := time.Until(sleepUntil)
		if wait > 0 {
			slog.Info("gateway: rate limit reached, waiting for next window",
				"model", model,
				"wait_ms", wait.Milliseconds(),
			)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return nil
}

// GenerateWithRetry wraps Client.Generate with pre-call rate limiting and automatic
// retry on 429 (rate limit) and 5xx (server error) responses.
// These retries are separate from the Temporal activity retry policies — those are
// for error taxonomy retries. This handles transient API unavailability only.
func (c *Client) GenerateWithRetry(ctx context.Context, req GenerateRequest, limiter *RateLimiter) (*GenerateResponse, error) {
	retries429 := 0
	retries5xx := 0

	for {
		if err := limiter.Wait(ctx, req.Model); err != nil {
			return nil, fmt.Errorf("rate limiter: %w", err)
		}

		resp, err := c.Generate(ctx, req)
		if err == nil {
			return resp, nil
		}

		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			return nil, err // network or parse error — don't retry
		}

		if apiErr.IsRateLimit() {
			retries429++
			if retries429 > maxRetriesOn429 {
				return nil, fmt.Errorf("rate limit persisted after %d retries: %w", retries429, err)
			}
			backoff := time.Duration(retries429) * 5 * time.Second
			slog.Warn("gateway: 429 rate limit, backing off",
				"model", req.Model,
				"retry", retries429,
				"backoff_ms", backoff.Milliseconds(),
			)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}

		if apiErr.IsServerError() {
			retries5xx++
			if retries5xx > maxRetriesOn5xx {
				return nil, fmt.Errorf("server error after %d retries: %w", retries5xx, err)
			}
			backoff := time.Duration(retries5xx) * 2 * time.Second
			slog.Warn("gateway: 5xx server error, backing off",
				"model", req.Model,
				"status", apiErr.StatusCode,
				"retry", retries5xx,
				"backoff_ms", backoff.Milliseconds(),
			)
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}

		// 4xx errors (other than 429) are not retried.
		return nil, err
	}
}
