package model

import (
	"testing"
	"time"
)

func TestProductToDocument(t *testing.T) {
	desc := "A sample product description."
	updatedAt := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)

	product := Product{
		ID:          "p1",
		Name:        "Laptop",
		Description: &desc,
		Category:    "electronics",
		Price:       1250.50,
		Stock:       7,
		UpdatedAt:   updatedAt,
	}

	got := product.ToDocument()

	if got.ID != "p1" {
		t.Fatalf("ID = %q, want %q", got.ID, "p1")
	}

	if got.Description != desc {
		t.Fatalf("Description = %q, want %q", got.Description, desc)
	}

	if got.UpdatedAt != updatedAt.Unix() {
		t.Fatalf("UpdatedAt = %d, want %d", got.UpdatedAt, updatedAt.Unix())
	}
}

func TestProductToDocumentWithNilDescription(t *testing.T) {
	product := Product{ID: "p2"}

	got := product.ToDocument()

	if got.Description != "" {
		t.Fatalf("Description = %q, want empty string", got.Description)
	}
}
