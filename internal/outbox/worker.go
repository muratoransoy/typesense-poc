package outbox

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"typesense-poc/internal/typesense"
)

const batchSize = 100

// Worker reads search_outbox events and applies them to Typesense.
type Worker struct {
	pool      *pgxpool.Pool
	search    searchClient
	interval  time.Duration
	batchSize int
}

type searchClient interface {
	UpsertRawJSON(ctx context.Context, rawJSON []byte) error
	DeleteDocument(ctx context.Context, id string) error
}

type Event struct {
	ID             int64
	CollectionName string
	RecordID       string
	OperationType  string
	Payload        string
	RetryCount     int
}

// NewWorker creates an outbox sync worker.
func NewWorker(pool *pgxpool.Pool, search *typesense.Client, interval time.Duration) *Worker {
	return &Worker{
		pool:      pool,
		search:    search,
		interval:  interval,
		batchSize: batchSize,
	}
}

// Run starts the outbox loop until the context is cancelled.
func (w *Worker) Run(ctx context.Context) error {
	log.Printf("outbox worker started, interval=%s", w.interval)

	if err := w.syncOnce(ctx); err != nil {
		log.Printf("initial outbox sync failed: %v", err)
	}

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("outbox worker stopped")
			return nil
		case <-ticker.C:
			if err := w.syncOnce(ctx); err != nil {
				log.Printf("outbox sync failed: %v", err)
			}
		}
	}
}

// syncOnce reads unprocessed outbox events and handles them one by one.
func (w *Worker) syncOnce(ctx context.Context) error {
	events, err := w.fetchEvents(ctx)
	if err != nil {
		return err
	}

	if len(events) == 0 {
		log.Println("outbox sync: no pending events")
		return nil
	}

	for _, event := range events {
		err := w.processEvent(ctx, event)
		if err == nil {
			if markErr := w.markProcessed(ctx, event.ID); markErr != nil {
				return markErr
			}

			log.Printf("outbox sync: processed event id=%d operation=%s", event.ID, event.OperationType)
			continue
		}

		if ctx.Err() != nil {
			return err
		}

		if markErr := w.markFailed(ctx, event.ID, err); markErr != nil {
			return markErr
		}

		log.Printf("outbox sync: failed event id=%d operation=%s error=%v", event.ID, event.OperationType, err)
	}

	return nil
}

// fetchEvents returns events that have not succeeded and have retry budget left.
func (w *Worker) fetchEvents(ctx context.Context) ([]Event, error) {
	// Production workers commonly add transactions and FOR UPDATE SKIP LOCKED here
	// so multiple worker instances do not process the same event.
	rows, err := w.pool.Query(ctx, `
SELECT
    id,
    collection_name,
    record_id,
    operation_type,
    COALESCE(payload, '{}'::jsonb)::text,
    retry_count
FROM search_outbox
WHERE processed = false
  AND retry_count < 5
ORDER BY id ASC
LIMIT $1
`, w.batchSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]Event, 0)
	for rows.Next() {
		var event Event
		if err := rows.Scan(
			&event.ID,
			&event.CollectionName,
			&event.RecordID,
			&event.OperationType,
			&event.Payload,
			&event.RetryCount,
		); err != nil {
			return nil, err
		}

		events = append(events, event)
	}

	return events, rows.Err()
}

// processEvent sends one outbox event to Typesense.
func (w *Worker) processEvent(ctx context.Context, event Event) error {
	switch event.OperationType {
	case "upsert":
		return w.search.UpsertRawJSON(ctx, []byte(event.Payload))
	case "delete":
		return w.search.DeleteDocument(ctx, event.RecordID)
	default:
		return fmt.Errorf("unknown operation_type=%s", event.OperationType)
	}
}

// markProcessed records a successful event.
func (w *Worker) markProcessed(ctx context.Context, eventID int64) error {
	_, err := w.pool.Exec(ctx, `
UPDATE search_outbox
SET processed = true,
    processed_at = now(),
    error_message = NULL
WHERE id = $1
`, eventID)

	return err
}

// markFailed increments retry_count and stores the last error for debugging.
func (w *Worker) markFailed(ctx context.Context, eventID int64, eventErr error) error {
	_, err := w.pool.Exec(ctx, `
UPDATE search_outbox
SET retry_count = retry_count + 1,
    error_message = $2
WHERE id = $1
`, eventID, truncateError(eventErr.Error()))

	return err
}

func truncateError(message string) string {
	const maxLength = 1000
	if len(message) <= maxLength {
		return message
	}

	return message[:maxLength]
}
