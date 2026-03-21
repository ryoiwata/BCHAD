// Command e2e-smoke is a throwaway integration probe that verifies all BCHAD
// infrastructure integration points work before implementing any component properly.
//
// Steps 1–4 require only the local Docker stack (just dev-up).
// Steps 5–6 require ANTHROPIC_API_KEY and GITHUB_TOKEN respectively.
//
// Run with: just smoke
//
// This is NOT production code. It is a one-time smoke test to surface
// infrastructure issues (API key scoping, git auth, connection strings) before
// they bite mid-Phase 2.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

func main() {
	ctx := context.Background()

	slog.Info("BCHAD e2e smoke test starting")

	step1Postgres(ctx)
	step2Valkey(ctx)
	step3MinIO(ctx)
	step4Temporal(ctx)
	step5Anthropic(ctx)
	// step6 (git commit) requires GITHUB_TOKEN and a test repo — skipped if not configured
}

// step1Postgres connects to Postgres, runs SELECT 1, and verifies pgvector is available.
func step1Postgres(ctx context.Context) {
	slog.Info("step 1: Postgres connection")

	dbURL := os.Getenv("BCHAD_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://bchad:bchad@localhost:5432/bchad?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("step 1 FAIL: connect to postgres", "error", err)
		return
	}
	defer pool.Close()

	var result int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&result); err != nil {
		slog.Error("step 1 FAIL: SELECT 1", "error", err)
		return
	}

	// Verify pgvector is available.
	var extName string
	err = pool.QueryRow(ctx,
		"SELECT extname FROM pg_extension WHERE extname = 'vector'",
	).Scan(&extName)
	if err != nil {
		slog.Warn("step 1 PARTIAL: pgvector extension not installed (run migrations first)", "error", err)
	} else {
		slog.Info("step 1 PASS: pgvector available", "extension", extName)
	}

	slog.Info("step 1 PASS: Postgres connected", "result", result)
}

// step2Valkey connects to Valkey, writes a key, reads it back, and deletes it.
func step2Valkey(ctx context.Context) {
	slog.Info("step 2: Valkey connection")

	valkeyURL := os.Getenv("BCHAD_VALKEY_URL")
	if valkeyURL == "" {
		valkeyURL = "localhost:6379"
	}

	c, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{valkeyURL},
	})
	if err != nil {
		slog.Error("step 2 FAIL: connect to Valkey", "error", err)
		return
	}
	defer c.Close()

	if err := c.Do(ctx, c.B().Set().Key("bchad:smoke:test").Value("hello-bchad").Ex(60*time.Second).Build()).Error(); err != nil {
		slog.Error("step 2 FAIL: SET", "error", err)
		return
	}

	val, err := c.Do(ctx, c.B().Get().Key("bchad:smoke:test").Build()).ToString()
	if err != nil {
		slog.Error("step 2 FAIL: GET", "error", err)
		return
	}

	if val != "hello-bchad" {
		slog.Error("step 2 FAIL: unexpected value", "got", val, "want", "hello-bchad")
		return
	}

	_ = c.Do(ctx, c.B().Del().Key("bchad:smoke:test").Build())

	slog.Info("step 2 PASS: Valkey SET/GET/DEL round-trip", "value", val)
}

// step3MinIO connects to MinIO via the S3 client, puts an object, reads it back, and deletes it.
func step3MinIO(ctx context.Context) {
	slog.Info("step 3: MinIO/S3 connection")

	endpoint := os.Getenv("BCHAD_S3_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:9000"
	}
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	if accessKey == "" {
		accessKey = "minioadmin"
	}
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
	}
	bucket := os.Getenv("BCHAD_S3_BUCKET_ARTIFACTS")
	if bucket == "" {
		bucket = "bchad-artifacts"
	}

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		),
	)
	if err != nil {
		slog.Error("step 3 FAIL: load S3 config", "error", err)
		return
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	key := "smoke/test-object.txt"
	body := "Hello from BCHAD smoke test"

	// Put object.
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader([]byte(body)),
	})
	if err != nil {
		slog.Error("step 3 FAIL: PutObject", "bucket", bucket, "key", key, "error", err)
		return
	}

	// Get object.
	out, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		slog.Error("step 3 FAIL: GetObject", "error", err)
		return
	}
	defer func() { _ = out.Body.Close() }()

	got, err := io.ReadAll(out.Body)
	if err != nil {
		slog.Error("step 3 FAIL: read GetObject body", "error", err)
		return
	}

	if string(got) != body {
		slog.Error("step 3 FAIL: body mismatch", "got", string(got), "want", body)
		return
	}

	// Clean up.
	_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	slog.Info("step 3 PASS: MinIO PutObject/GetObject round-trip", "bucket", bucket)
}

// noopWorkflow is a trivial workflow that returns immediately. Used for step 4.
func noopWorkflow(ctx workflow.Context) (string, error) {
	return "hello from BCHAD", nil
}

// step4Temporal connects to the Temporal dev server, starts a no-op workflow,
// and waits for it to complete.
func step4Temporal(ctx context.Context) {
	slog.Info("step 4: Temporal connection")

	temporalHost := os.Getenv("BCHAD_TEMPORAL_HOST")
	if temporalHost == "" {
		temporalHost = "localhost:7233"
	}
	namespace := os.Getenv("BCHAD_TEMPORAL_NAMESPACE")
	if namespace == "" {
		namespace = "default" // dev server uses "default" namespace by default
	}

	c, err := client.Dial(client.Options{
		HostPort:  temporalHost,
		Namespace: namespace,
	})
	if err != nil {
		slog.Error("step 4 FAIL: connect to Temporal", "host", temporalHost, "error", err)
		return
	}
	defer c.Close()

	// Start a worker to execute the no-op workflow.
	taskQueue := "bchad-smoke"
	w := worker.New(c, taskQueue, worker.Options{})
	w.RegisterWorkflow(noopWorkflow)
	if err := w.Start(); err != nil {
		slog.Error("step 4 FAIL: start worker", "error", err)
		return
	}
	defer w.Stop()

	// Start the workflow.
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		TaskQueue: taskQueue,
		ID:        fmt.Sprintf("smoke-noop-%d", time.Now().UnixNano()),
	}, noopWorkflow)
	if err != nil {
		slog.Error("step 4 FAIL: execute workflow", "error", err)
		return
	}

	// Wait for completion.
	var result string
	if err := run.Get(ctx, &result); err != nil {
		slog.Error("step 4 FAIL: workflow result", "error", err)
		return
	}

	slog.Info("step 4 PASS: Temporal no-op workflow completed", "result", result, "runID", run.GetRunID())
}

// anthropicRequest is the request body for the Anthropic Messages API.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	Messages  []anthropicMessage `json:"messages"`
}

// anthropicMessage is a single message in the Anthropic conversation.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the response body from the Anthropic Messages API.
type anthropicResponse struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// step5Anthropic calls the Anthropic API with a minimal prompt to prove connectivity.
// Requires ANTHROPIC_API_KEY.
func step5Anthropic(ctx context.Context) {
	slog.Info("step 5: Anthropic API")

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		slog.Info("step 5 SKIP: ANTHROPIC_API_KEY not set")
		return
	}

	reqBody := anthropicRequest{
		Model:     "claude-haiku-4-5",
		MaxTokens: 256,
		Messages: []anthropicMessage{
			{
				Role:    "user",
				Content: `Generate a single Go file that prints "Hello from BCHAD". Output only the file contents, no explanation.`,
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		slog.Error("step 5 FAIL: marshal request", "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages",
		bytes.NewReader(body),
	)
	if err != nil {
		slog.Error("step 5 FAIL: create request", "error", err)
		return
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		slog.Error("step 5 FAIL: HTTP request", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Error("step 5 FAIL: unexpected status", "status", resp.StatusCode, "body", string(respBody))
		return
	}

	var apiResp anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		slog.Error("step 5 FAIL: decode response", "error", err)
		return
	}

	if len(apiResp.Content) == 0 {
		slog.Error("step 5 FAIL: empty response content")
		return
	}

	generatedText := apiResp.Content[0].Text
	slog.Info("step 5 PASS: Anthropic API responded",
		"input_tokens", apiResp.Usage.InputTokens,
		"output_tokens", apiResp.Usage.OutputTokens,
		"response_preview", generatedText[:min(80, len(generatedText))],
	)

	// Write the generated text to a temp file for inspection.
	tmpFile := "/tmp/bchad-smoke-generated.go"
	if err := os.WriteFile(tmpFile, []byte(generatedText), 0600); err != nil {
		slog.Warn("step 5: could not write generated file", "path", tmpFile, "error", err)
	} else {
		slog.Info("step 5: generated file written", "path", tmpFile)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
