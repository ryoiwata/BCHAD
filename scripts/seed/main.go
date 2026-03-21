// Command seed populates the local database with a test codebase profile
// for the payments-dashboard-test product. Used for local development and
// integration testing.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()

	dbURL := os.Getenv("BCHAD_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://bchad:bchad@localhost:5433/bchad?sslmode=disable"
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("database ping failed", "error", err)
		os.Exit(1)
	}

	slog.Info("connected to database, seeding test data...")

	// Insert a test trust score record for local development.
	_, err = pool.Exec(ctx, `
		INSERT INTO bchad_trust_scores (engineer_id, product_id, score, phase)
		VALUES ('engineer-test-001', 'payments-dashboard-test', 0, 'supervised')
		ON CONFLICT (engineer_id, product_id) DO NOTHING
	`)
	if err != nil {
		slog.Error("failed to seed trust score", "error", err)
		os.Exit(1)
	}

	fmt.Println("Seed complete: payments-dashboard-test profile loaded")
}
