package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateRequest_Serialization(t *testing.T) {
	req := GenerateRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 8192,
		System:    "You are a code generator.",
		Messages: []Message{
			{Role: "user", Content: "Generate a migration"},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model: got %v", decoded["model"])
	}
	if decoded["max_tokens"].(float64) != 8192 {
		t.Errorf("max_tokens: got %v", decoded["max_tokens"])
	}
	if decoded["system"] != "You are a code generator." {
		t.Errorf("system: got %v", decoded["system"])
	}
	msgs := decoded["messages"].([]interface{})
	if len(msgs) != 1 {
		t.Fatalf("messages: expected 1, got %d", len(msgs))
	}
	msg := msgs[0].(map[string]interface{})
	if msg["role"] != "user" {
		t.Errorf("messages[0].role: got %v", msg["role"])
	}
}

func TestGenerate_Headers(t *testing.T) {
	var capturedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		// Return a minimal valid response.
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": "msg_test",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "hello"}],
			"model": "claude-sonnet-4-20250514",
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 5}
		}`)
	}))
	defer server.Close()

	// Override the base URL for testing.
	client := &Client{
		apiKey: "test-api-key",
		httpClient: &http.Client{},
	}

	// Swap the URL by sending to the test server directly.
	req := GenerateRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 100,
		Messages:  []Message{{Role: "user", Content: "hi"}},
	}
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader(string(body)))
	httpReq.Header.Set("x-api-key", client.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := client.httpClient.Do(httpReq)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if capturedHeaders.Get("x-api-key") != "test-api-key" {
		t.Errorf("x-api-key header: got %q", capturedHeaders.Get("x-api-key"))
	}
	if capturedHeaders.Get("anthropic-version") != anthropicVersion {
		t.Errorf("anthropic-version header: got %q", capturedHeaders.Get("anthropic-version"))
	}
	if capturedHeaders.Get("content-type") != "application/json" {
		t.Errorf("content-type header: got %q", capturedHeaders.Get("content-type"))
	}
}

func TestGenerate_SuccessfulResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"id": "msg_01xyz",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "Generated code here"}],
			"model": "claude-sonnet-4-20250514",
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 100, "output_tokens": 50}
		}`)
	}))
	defer server.Close()

	client := newTestClientWithURL(server.URL)
	resp, err := client.Generate(context.Background(), GenerateRequest{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1000,
		Messages:  []Message{{Role: "user", Content: "generate"}},
	})
	if err != nil {
		t.Fatalf("Generate(): %v", err)
	}

	if resp.Usage.InputTokens != 100 {
		t.Errorf("input_tokens: got %d, want 100", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 50 {
		t.Errorf("output_tokens: got %d, want 50", resp.Usage.OutputTokens)
	}
	if resp.Text() != "Generated code here" {
		t.Errorf("Text(): got %q", resp.Text())
	}
}

func TestGenerate_401Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"Invalid API key"}}`)
	}))
	defer server.Close()

	client := newTestClientWithURL(server.URL)
	_, err := client.Generate(context.Background(), GenerateRequest{
		Model:    "claude-sonnet-4-20250514",
		MaxTokens: 100,
		Messages: []Message{{Role: "user", Content: "hi"}},
	})

	if err == nil {
		t.Fatal("Generate() expected error for 401, got nil")
	}

	var apiErr *APIError
	if !isAPIError(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("status code: got %d, want 401", apiErr.StatusCode)
	}
}

func TestGenerate_429RateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limit exceeded"}}`)
	}))
	defer server.Close()

	client := newTestClientWithURL(server.URL)
	_, err := client.Generate(context.Background(), GenerateRequest{
		Model:    "claude-sonnet-4-20250514",
		MaxTokens: 100,
		Messages: []Message{{Role: "user", Content: "hi"}},
	})

	if err == nil {
		t.Fatal("Generate() expected error for 429, got nil")
	}

	var apiErr *APIError
	if !isAPIError(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if !apiErr.IsRateLimit() {
		t.Errorf("expected IsRateLimit()=true, got false (status %d)", apiErr.StatusCode)
	}
}

func TestGenerate_500ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"Internal server error"}}`)
	}))
	defer server.Close()

	client := newTestClientWithURL(server.URL)
	_, err := client.Generate(context.Background(), GenerateRequest{
		Model:    "claude-sonnet-4-20250514",
		MaxTokens: 100,
		Messages: []Message{{Role: "user", Content: "hi"}},
	})

	if err == nil {
		t.Fatal("Generate() expected error for 500, got nil")
	}

	var apiErr *APIError
	if !isAPIError(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if !apiErr.IsServerError() {
		t.Errorf("expected IsServerError()=true, got false (status %d)", apiErr.StatusCode)
	}
}

// newTestClientWithURL creates a Client that sends requests to the given base URL.
// This overrides the http.Client.Transport to redirect to the test server.
func newTestClientWithURL(baseURL string) *Client {
	return &Client{
		apiKey: "test-key",
		httpClient: &http.Client{
			Transport: &testRoundTripper{baseURL: baseURL},
		},
	}
}

// testRoundTripper redirects all requests to a fixed base URL for testing.
type testRoundTripper struct {
	baseURL string
}

func (t *testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL.Host = strings.TrimPrefix(strings.TrimPrefix(t.baseURL, "https://"), "http://")
	req2.URL.Scheme = "http"
	return http.DefaultTransport.RoundTrip(req2)
}

// isAPIError checks if err is an *APIError and sets the out pointer.
func isAPIError(err error, out **APIError) bool {
	if e, ok := err.(*APIError); ok {
		*out = e
		return true
	}
	return false
}
