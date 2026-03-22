package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// StreamEvent is a single parsed SSE event from the Anthropic streaming API.
type StreamEvent struct {
	// Type is the event type: "content_block_delta", "message_stop", "error".
	Type string
	// Delta is the text delta for content_block_delta events.
	Delta string
	// Usage is populated on message_stop events when usage data is available.
	Usage *Usage
}

// sseEventData covers the JSON shapes of SSE data lines from the Anthropic API.
type sseEventData struct {
	Type  string          `json:"type"`
	Index int             `json:"index"`
	Delta *sseDeltaBlock  `json:"delta"`
	Usage *Usage          `json:"usage"`
	Error *sseErrorDetail `json:"error"`
}

type sseDeltaBlock struct {
	Type string `json:"type"` // "text_delta"
	Text string `json:"text"`
}

type sseErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// GenerateStream sends a streaming request to the Anthropic Messages API.
// Returns a channel of StreamEvents. The channel is closed when the stream ends
// or the context is cancelled.
func (c *Client) GenerateStream(ctx context.Context, req GenerateRequest) (<-chan StreamEvent, error) {
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal stream request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicBaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create stream request: %w", err)
	}
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("accept", "text/event-stream")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("stream request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		var errResp apiErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		apiErr := &APIError{StatusCode: resp.StatusCode}
		apiErr.Body.Type = errResp.Error.Type
		apiErr.Body.Message = errResp.Error.Message
		return nil, apiErr
	}

	events := make(chan StreamEvent, 32)
	go func() {
		defer func() { _ = resp.Body.Close() }()
		defer close(events)
		parseSSEStream(ctx, resp.Body, events)
	}()

	return events, nil
}

// CollectStreamText reads all text deltas from a stream channel and returns the
// concatenated text and final usage. Blocks until the channel is closed.
func CollectStreamText(ctx context.Context, events <-chan StreamEvent) (string, Usage) {
	var b strings.Builder
	var usage Usage
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return b.String(), usage
			}
			if ev.Type == "content_block_delta" {
				b.WriteString(ev.Delta)
			}
			if ev.Type == "message_stop" && ev.Usage != nil {
				usage = *ev.Usage
			}
		case <-ctx.Done():
			return b.String(), usage
		}
	}
}

// parseSSEStream reads SSE lines from r and sends StreamEvents to the channel.
func parseSSEStream(ctx context.Context, r io.Reader, events chan<- StreamEvent) {
	scanner := bufio.NewScanner(r)
	var eventType string

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "event: "):
			eventType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				return
			}
			emitSSEEvent(eventType, data, events)
			eventType = ""
		}
	}
}

// emitSSEEvent parses a single SSE data JSON string and emits to the events channel.
func emitSSEEvent(eventType, dataStr string, events chan<- StreamEvent) {
	var ev sseEventData
	if err := json.Unmarshal([]byte(dataStr), &ev); err != nil {
		return // skip malformed events
	}

	switch ev.Type {
	case "content_block_delta":
		if ev.Delta != nil && ev.Delta.Type == "text_delta" {
			events <- StreamEvent{Type: "content_block_delta", Delta: ev.Delta.Text}
		}
	case "message_stop":
		se := StreamEvent{Type: "message_stop"}
		if ev.Usage != nil {
			se.Usage = ev.Usage
		}
		events <- se
	case "error":
		if ev.Error != nil {
			events <- StreamEvent{Type: "error", Delta: ev.Error.Message}
		}
	default:
		// message_start, content_block_start, content_block_stop — informational only
		_ = eventType
	}
}
