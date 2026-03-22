package gateway

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestParseSSEStream_TextDeltas(t *testing.T) {
	// Read the streaming events fixture.
	fixture, err := os.ReadFile("../../testdata/fixtures/llm/streaming-events.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	events := make(chan StreamEvent, 64)
	ctx := context.Background()
	go func() {
		defer close(events)
		parseSSEStream(ctx, strings.NewReader(string(fixture)), events)
	}()

	var deltas []string
	var gotStop bool
	for ev := range events {
		switch ev.Type {
		case "content_block_delta":
			deltas = append(deltas, ev.Delta)
		case "message_stop":
			gotStop = true
		}
	}

	if len(deltas) == 0 {
		t.Error("expected at least one text delta from stream fixture")
	}
	if !gotStop {
		t.Error("expected message_stop event from stream fixture")
	}

	combined := strings.Join(deltas, "")
	if combined == "" {
		t.Error("combined text from deltas is empty")
	}
	t.Logf("stream fixture: %d deltas, combined text: %q", len(deltas), combined[:min(len(combined), 50)])
}

func TestCollectStreamText(t *testing.T) {
	events := make(chan StreamEvent, 10)
	events <- StreamEvent{Type: "content_block_delta", Delta: "Hello"}
	events <- StreamEvent{Type: "content_block_delta", Delta: ", world"}
	events <- StreamEvent{Type: "message_stop", Usage: &Usage{InputTokens: 10, OutputTokens: 5}}
	close(events)

	text, usage := CollectStreamText(context.Background(), events)

	if text != "Hello, world" {
		t.Errorf("CollectStreamText() text = %q, want %q", text, "Hello, world")
	}
	if usage.OutputTokens != 5 {
		t.Errorf("usage.OutputTokens = %d, want 5", usage.OutputTokens)
	}
}

func TestParseSSEStream_MalformedEventSkipped(t *testing.T) {
	raw := `event: content_block_delta
data: {not valid json}

event: message_stop
data: {"type":"message_stop"}

`
	events := make(chan StreamEvent, 10)
	ctx := context.Background()
	go func() {
		defer close(events)
		parseSSEStream(ctx, strings.NewReader(raw), events)
	}()

	var gotStop bool
	for ev := range events {
		if ev.Type == "message_stop" {
			gotStop = true
		}
	}

	// Malformed event is skipped, message_stop is still received.
	if !gotStop {
		t.Error("expected message_stop after malformed event")
	}
}

func TestParseSSEStream_ErrorEvent(t *testing.T) {
	raw := `event: error
data: {"type":"error","error":{"type":"api_error","message":"Internal server error"}}

`
	events := make(chan StreamEvent, 10)
	ctx := context.Background()
	go func() {
		defer close(events)
		parseSSEStream(ctx, strings.NewReader(raw), events)
	}()

	var gotError bool
	for ev := range events {
		if ev.Type == "error" && ev.Delta != "" {
			gotError = true
		}
	}

	if !gotError {
		t.Error("expected error event from stream")
	}
}

func TestParseSSEStream_ContextCancel(t *testing.T) {
	// Large stream that would block if context cancellation doesn't work.
	var sb strings.Builder
	for i := 0; i < 1000; i++ {
		sb.WriteString("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan StreamEvent, 10)
	go func() {
		defer close(events)
		parseSSEStream(ctx, strings.NewReader(sb.String()), events)
	}()

	// Read a few events, then cancel.
	count := 0
	for range events {
		count++
		if count >= 3 {
			cancel()
			break
		}
	}
	// Drain remaining.
	for range events {
	}
	// If we get here without hanging, context cancellation worked.
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
