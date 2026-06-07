package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool creates one PostgreSQL connection pool for the whole worker.
// Workers should not open/close a database connection on every tick.
// A small pool is enough for this local POC.
func NewPool(ctx context.Context, postgresURL string) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(postgresURL)
	if err != nil {
		return nil, err
	}

	poolConfig.MaxConns = 4
	poolConfig.MinConns = 0
	poolConfig.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return pool, nil
}
