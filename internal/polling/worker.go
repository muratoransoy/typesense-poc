package polling

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"typesense-poc/internal/model"
	"typesense-poc/internal/typesense"
)

const batchSize = 100

// Worker periodically scans PostgreSQL and mirrors product changes to Typesense.
type Worker struct {
	pool         *pgxpool.Pool
	search       *typesense.Client
	interval     time.Duration
	lastSyncedAt time.Time
}

// NewWorker creates a polling sync worker.
func NewWorker(pool *pgxpool.Pool, search *typesense.Client, interval time.Duration) *Worker {
	return &Worker{
		pool:         pool,
		search:       search,
		interval:     interval,
		lastSyncedAt: time.Unix(0, 0).UTC(),
	}
}

// Run starts the polling loop until the context is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	log.Printf("polling worker started, interval=%s", w.interval)

	if err := w.syncOnce(ctx); err != nil {
		log.Printf("initial polling sync failed: %v", err)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("polling worker stopped")
			return nil
		case <-ticker.C:
			if err := w.syncOnce(ctx); err != nil {
				log.Printf("polling sync failed: %v", err)
			}
		}
	}
}

// syncOnce reads rows changed since lastSyncedAt and applies them to Typesense.
func (w *Worker) syncOnce(ctx context.Context) error {
	// POC keeps lastSyncedAt in memory.
	// In production this value should be stored durably in a database table.
	windowEnd, err := w.databaseNow(ctx)
	if err != nil {
		return err
	}

	products, err := w.fetchChangedProducts(ctx, w.lastSyncedAt, windowEnd)
	if err != nil {
		return err
	}

	if len(products) == 0 {
		log.Printf("polling sync: no changes after %s", w.lastSyncedAt.Format(time.RFC3339))
		w.lastSyncedAt = windowEnd
		return nil
	}

	if err := w.applyProducts(ctx, products); err != nil {
		return err
	}

	w.lastSyncedAt = maxUpdatedAt(products, w.lastSyncedAt)
	log.Printf("polling sync: processed %d products, lastSyncedAt=%s", len(products), w.lastSyncedAt.Format(time.RFC3339))

	return nil
}

// databaseNow uses PostgreSQL time because products.updated_at is also set there.
func (w *Worker) databaseNow(ctx context.Context) (time.Time, error) {
	var value time.Time
	if err := w.pool.QueryRow(ctx, `SELECT now()`).Scan(&value); err != nil {
		return time.Time{}, err
	}

	return value.UTC(), nil
}

// fetchChangedProducts uses updated_at to find rows changed inside one polling window.
func (w *Worker) fetchChangedProducts(ctx context.Context, lastSyncedAt, windowEnd time.Time) ([]model.Product, error) {
	rows, err := w.pool.Query(ctx, `
SELECT
    id::text,
    name,
    description,
    category,
    price::float8,
    stock,
    is_deleted,
    updated_at
FROM products
WHERE updated_at > $1
  AND updated_at <= $2
ORDER BY updated_at ASC
LIMIT $3
`, lastSyncedAt, windowEnd, batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	products := make([]model.Product, 0)
	for rows.Next() {
		var product model.Product
		if err := rows.Scan(
			&product.ID,
			&product.Name,
			&product.Description,
			&product.Category,
			&product.Price,
			&product.Stock,
			&product.IsDeleted,
			&product.UpdatedAt,
		); err != nil {
			return nil, err
		}

		products = append(products, product)
	}

	return products, rows.Err()
}

// applyProducts upserts active products and deletes soft-deleted products.
func (w *Worker) applyProducts(ctx context.Context, products []model.Product) error {
	upserts := make([]model.ProductDocument, 0, len(products))
	deletes := make([]string, 0)

	for _, product := range products {
		// is_deleted is a soft-delete flag. PostgreSQL keeps the row, Typesense drops it.
		if product.IsDeleted {
			deletes = append(deletes, product.ID)
			continue
		}

		upserts = append(upserts, product.ToDocument())
	}

	if err := w.search.UpsertDocuments(ctx, upserts); err != nil {
		return err
	}

	for _, id := range deletes {
		if err := w.search.DeleteDocument(ctx, id); err != nil {
			return err
		}
	}

	return nil
}

func maxUpdatedAt(products []model.Product, fallback time.Time) time.Time {
	maxValue := fallback
	for _, product := range products {
		if product.UpdatedAt.After(maxValue) {
			maxValue = product.UpdatedAt
		}
	}

	return maxValue
}
