package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	anthropicBaseURL = "https://api.anthropic.com/v1/messages"
	anthropicVersion = "2023-06-01"
	defaultTimeout   = 120 * time.Second
)

// Client is the in-process LLM API client. It is instantiated once and shared
// across the control plane. It calls the Anthropic Messages API via direct HTTP.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new Client with the given Anthropic API key.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// GenerateRequest is the request body sent to the Anthropic Messages API.
// All fields are explicit Go structs — no map[string]interface{}.
type GenerateRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	Stream    bool      `json:"stream,omitempty"`
}

// Message is a single message in the conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// GenerateResponse is the parsed response from the Anthropic Messages API.
type GenerateResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Content      []ContentBlock `json:"content"`
	Model        string         `json:"model"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        Usage          `json:"usage"`
}

// Text returns the concatenated text from all text-type content blocks.
func (r *GenerateResponse) Text() string {
	var b strings.Builder
	for _, block := range r.Content {
		if block.Type == "text" {
			b.WriteString(block.Text)
		}
	}
	return b.String()
}

// ContentBlock is a single content block in a response.
type ContentBlock struct {
	Type  string `json:"type"`            // "text" or "tool_use"
	Text  string `json:"text,omitempty"`  // populated for "text" blocks
	ID    string `json:"id,omitempty"`    // populated for "tool_use" blocks
	Name  string `json:"name,omitempty"`  // populated for "tool_use" blocks
	Input any    `json:"input,omitempty"` // populated for "tool_use" blocks
}

// Usage tracks token consumption for a single API call.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// APIError represents an error response from the Anthropic API.
type APIError struct {
	StatusCode int
	Body       struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return fmt.Sprintf("anthropic API error %d: %s: %s", e.StatusCode, e.Body.Type, e.Body.Message)
}

// IsRateLimit reports whether this is a 429 (rate limit) error.
func (e *APIError) IsRateLimit() bool { return e.StatusCode == 429 }

// IsServerError reports whether this is a 5xx server error.
func (e *APIError) IsServerError() bool { return e.StatusCode >= 500 }

// apiErrorResponse is used to parse the error body from the Anthropic API.
type apiErrorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// Generate sends a non-streaming request to the Anthropic Messages API.
// It does not handle rate limiting or transient retries — use GenerateWithRetry for that.
func (c *Client) Generate(ctx context.Context, req GenerateRequest) (*GenerateResponse, error) {
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal generate request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicBaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("content-type", "application/json")

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp apiErrorResponse
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		apiErr := &APIError{StatusCode: resp.StatusCode}
		apiErr.Body.Type = errResp.Error.Type
		apiErr.Body.Message = errResp.Error.Message
		return nil, apiErr
	}

	var genResp GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return nil, fmt.Errorf("decode generate response: %w", err)
	}

	slog.Info("gateway: generate complete",
		"model", genResp.Model,
		"input_tokens", genResp.Usage.InputTokens,
		"output_tokens", genResp.Usage.OutputTokens,
		"stop_reason", genResp.StopReason,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	return &genResp, nil
}
