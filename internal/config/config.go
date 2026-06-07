package config

import (
	"os"
	"strconv"
	"time"
)

// Config keeps all runtime settings in one small struct.
// Environment variables win; defaults make the POC runnable without setup.
type Config struct {
	PostgresURL         string
	TypesenseHost       string
	TypesenseAPIKey     string
	TypesenseCollection string
	PollingInterval     time.Duration
	OutboxInterval      time.Duration
}

// Load reads environment variables and falls back to local Docker defaults.
func Load() Config {
	return Config{
		PostgresURL:         getEnv("POSTGRES_URL", "postgres://postgres:postgres@localhost:5432/appdb?sslmode=disable"),
		TypesenseHost:       getEnv("TYPESENSE_HOST", "http://localhost:8108"),
		TypesenseAPIKey:     getEnv("TYPESENSE_API_KEY", "xyz"),
		TypesenseCollection: getEnv("TYPESENSE_COLLECTION", "products"),
		PollingInterval:     time.Duration(getEnvInt("POLLING_INTERVAL_SECONDS", 5)) * time.Second,
		OutboxInterval:      time.Duration(getEnvInt("OUTBOX_INTERVAL_SECONDS", 3)) * time.Second,
	}
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	return value
}

func getEnvInt(key string, defaultValue int) int {
	rawValue := os.Getenv(key)
	if rawValue == "" {
		return defaultValue
	}

	value, err := strconv.Atoi(rawValue)
	if err != nil || value <= 0 {
		return defaultValue
	}

	return value
}
