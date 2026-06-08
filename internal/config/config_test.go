package config

import (
	"testing"
	"time"
)

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("POSTGRES_URL", "")
	t.Setenv("TYPESENSE_HOST", "")
	t.Setenv("TYPESENSE_API_KEY", "")
	t.Setenv("TYPESENSE_COLLECTION", "")
	t.Setenv("POLLING_INTERVAL_SECONDS", "")
	t.Setenv("OUTBOX_INTERVAL_SECONDS", "")

	cfg := Load()

	if cfg.TypesenseHost != "http://localhost:8108" {
		t.Fatalf("TypesenseHost = %q", cfg.TypesenseHost)
	}

	if cfg.PollingInterval != 5*time.Second {
		t.Fatalf("PollingInterval = %s, want 5s", cfg.PollingInterval)
	}
}

func TestLoadReadsEnvOverrides(t *testing.T) {
	t.Setenv("TYPESENSE_HOST", "http://typesense:8108")
	t.Setenv("TYPESENSE_API_KEY", "secret")
	t.Setenv("TYPESENSE_COLLECTION", "custom_products")
	t.Setenv("POLLING_INTERVAL_SECONDS", "10")

	cfg := Load()

	if cfg.TypesenseHost != "http://typesense:8108" {
		t.Fatalf("TypesenseHost = %q", cfg.TypesenseHost)
	}

	if cfg.PollingInterval != 10*time.Second {
		t.Fatalf("PollingInterval = %s, want 10s", cfg.PollingInterval)
	}
}
