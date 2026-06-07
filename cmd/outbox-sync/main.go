package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"typesense-poc/internal/config"
	"typesense-poc/internal/outbox"
	"typesense-poc/internal/postgres"
	"typesense-poc/internal/typesense"
)

// main wires config, PostgreSQL, Typesense, and the outbox worker together.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.Load()

	pool, err := postgres.NewPool(ctx, cfg.PostgresURL)
	if err != nil {
		log.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	search := typesense.NewClient(cfg)
	if err := search.CreateProductsCollection(ctx); err != nil {
		log.Fatalf("create typesense collection: %v", err)
	}

	worker := outbox.NewWorker(pool, search, cfg.OutboxInterval)
	if err := worker.Run(ctx); err != nil {
		log.Fatalf("run outbox worker: %v", err)
	}
}
