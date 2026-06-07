package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"typesense-poc/internal/config"
	"typesense-poc/internal/polling"
	"typesense-poc/internal/postgres"
	"typesense-poc/internal/typesense"
)

// main wires config, PostgreSQL, Typesense, and the polling worker together.
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

	worker := polling.NewWorker(pool, search, cfg.PollingInterval)
	if err := worker.Run(ctx); err != nil {
		log.Fatalf("run polling worker: %v", err)
	}
}
