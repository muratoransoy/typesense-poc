package outbox

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProductWriteCreatesOutboxEventInSameTransaction(t *testing.T) {
	postgresURL := os.Getenv("POSTGRES_URL")
	if postgresURL == "" {
		t.Skip("set POSTGRES_URL to run the transactional outbox integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, postgresURL)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}

	var productID string
	if err := tx.QueryRow(ctx, `
INSERT INTO products(name, description, category, price, stock)
VALUES ('Outbox Test Product', 'created from go test', 'test', 12.34, 3)
RETURNING id::text
`).Scan(&productID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("insert product: %v", err)
	}

	var recordID string
	var operationType string
	var payload string
	if err := tx.QueryRow(ctx, `
SELECT record_id, operation_type, payload::text
FROM search_outbox
WHERE record_id = $1
ORDER BY id DESC
LIMIT 1
`, productID).Scan(&recordID, &operationType, &payload); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("read outbox event inside transaction: %v", err)
	}

	if recordID != productID {
		_ = tx.Rollback(ctx)
		t.Fatalf("recordID = %s, want %s", recordID, productID)
	}

	if operationType != "upsert" {
		_ = tx.Rollback(ctx)
		t.Fatalf("operationType = %s, want upsert", operationType)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("decode outbox payload: %v", err)
	}

	if doc["id"] != productID {
		_ = tx.Rollback(ctx)
		t.Fatalf("payload id = %v, want %s", doc["id"], productID)
	}

	if doc["name"] != "Outbox Test Product" {
		_ = tx.Rollback(ctx)
		t.Fatalf("payload name = %v, want Outbox Test Product", doc["name"])
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback transaction: %v", err)
	}

	var committedEvents int
	if err := pool.QueryRow(ctx, `
SELECT count(*)
FROM search_outbox
WHERE record_id = $1
`, productID).Scan(&committedEvents); err != nil {
		t.Fatalf("count committed outbox events: %v", err)
	}

	if committedEvents != 0 {
		t.Fatalf("committed outbox events = %d, want 0 after rollback", committedEvents)
	}
}
