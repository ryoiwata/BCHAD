# BCHAD justfile — task runner for local development
# Requires: just, docker, docker compose, go, golangci-lint, gofumpt

set dotenv-load

# Default environment variables for local dev
export BCHAD_DATABASE_URL := env_var_or_default("BCHAD_DATABASE_URL", "postgres://bchad:bchad@localhost:5433/bchad?sslmode=disable")
export BCHAD_VALKEY_URL := env_var_or_default("BCHAD_VALKEY_URL", "localhost:6379")
export BCHAD_S3_ENDPOINT := env_var_or_default("BCHAD_S3_ENDPOINT", "http://localhost:9000")
export BCHAD_TEMPORAL_HOST := env_var_or_default("BCHAD_TEMPORAL_HOST", "localhost:7233")
export BCHAD_TEMPORAL_NAMESPACE := env_var_or_default("BCHAD_TEMPORAL_NAMESPACE", "bchad")

# Show available targets
default:
    @just --list

# Start local infrastructure (Postgres, Valkey, MinIO, Temporal)
dev-up:
    docker compose up -d
    @echo "Waiting for services to be healthy..."
    @docker compose ps
    @echo "Registering bchad Temporal namespace (idempotent)..."
    @sleep 3
    @docker exec bchad-temporal sh -c 'IP=$(hostname -i | tr -d " "); tctl --address $IP:7233 --namespace bchad namespace register --retention 72h 2>&1 | grep -v DEPRECATION; true'
    @echo "Registering custom search attributes (idempotent)..."
    @docker exec bchad-temporal sh -c 'IP=$(hostname -i | tr -d " "); echo y | tctl --address $IP:7233 admin cluster add-search-attributes --name product --type Keyword --name engineer --type Keyword --name trust_phase --type Keyword 2>&1 | grep -v DEPRECATION; true'

# Stop local infrastructure
dev-down:
    docker compose down

# Run database migrations (up)
migrate:
    go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest \
        -path migrations \
        -database "$BCHAD_DATABASE_URL" \
        up

# Rollback last database migration
migrate-down:
    go run -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest \
        -path migrations \
        -database "$BCHAD_DATABASE_URL" \
        down 1

# Seed test codebase profile (payments-dashboard-test)
seed:
    go run ./scripts/seed/main.go

# Run all tests
test:
    go test ./...

# Run unit tests only (no integration or e2e)
test-unit:
    go test -short ./...

# Run integration tests (requires local stack via dev-up)
test-int:
    go test -tags=integration ./...

# Run end-to-end tests (requires local stack + API keys)
test-e2e:
    go test -tags=e2e ./...

# Run snapshot tests
test-snapshot:
    go test -run TestSnapshot ./...

# Update snapshot test fixtures
snapshot-update:
    UPDATE_SNAPSHOTS=true go test -run TestSnapshot ./...

# Lint with golangci-lint
lint:
    golangci-lint run

# Format Go and frontend code
fmt:
    gofumpt -w .
    cd web && npx prettier --write . || true

# Build CLI and worker binaries
build:
    go build -o bin/bchad ./cmd/bchad
    go build -o bin/worker ./cmd/worker

# Remove built binaries
clean:
    rm -rf bin/

# Run e2e smoke script (tests all integration points)
smoke:
    go run ./scripts/e2e-smoke/main.go

# Index the test target repository into pgvector (requires dev-up + VOYAGE_API_KEY)
# Usage: just index-repo
index-repo:
    go run ./cmd/bchad index \
        --repo ~/Documents/projects/ai_engineering/gauntlet-curriculum/projects/node-express-prisma-v1-official-app \
        --product node-express-prisma-v1

# Validate embedding quality after indexing (requires dev-up + VOYAGE_API_KEY)
# Prints top-3 retrieval results for each stage type — engineer manually evaluates quality
# Usage: just validate-embeddings
validate-embeddings:
    go run ./scripts/validate-embeddings/main.go --product node-express-prisma-v1
