package typesense

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"typesense-poc/internal/config"
	"typesense-poc/internal/model"
)

func TestUpsertDocumentsSendsJSONLToTypesenseImportAPI(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotQuery string
	var gotAPIKey string
	var gotContentType string
	var gotBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAPIKey = r.Header.Get("X-TYPESENSE-API-KEY")
		gotContentType = r.Header.Get("Content-Type")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		gotBody = string(body)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{\"success\":true,\"id\":\"product-1\"}\n{\"success\":true,\"id\":\"product-2\"}\n"))
	}))
	defer server.Close()

	client := NewClient(config.Config{
		TypesenseHost:       server.URL,
		TypesenseAPIKey:     "debug-key",
		TypesenseCollection: "products",
	})

	docs := []model.ProductDocument{
		{
			ID:          "product-1",
			Name:        "iPhone 15",
			Description: "Apple smartphone",
			Category:    "phone",
			Price:       54000,
			Stock:       10,
			UpdatedAt:   1780905600,
		},
		{
			ID:        "product-2",
			Name:      "MacBook Pro",
			Category:  "laptop",
			Price:     95000,
			Stock:     5,
			UpdatedAt: 1780905700,
		},
	}

	if err := client.UpsertDocuments(context.Background(), docs); err != nil {
		t.Fatalf("UpsertDocuments returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want %s", gotMethod, http.MethodPost)
	}

	if gotPath != "/collections/products/documents/import" {
		t.Fatalf("path = %s, want %s", gotPath, "/collections/products/documents/import")
	}

	if gotQuery != "action=upsert" {
		t.Fatalf("query = %s, want action=upsert", gotQuery)
	}

	if gotAPIKey != "debug-key" {
		t.Fatalf("api key = %s, want debug-key", gotAPIKey)
	}

	if !strings.HasPrefix(gotContentType, "text/plain") {
		t.Fatalf("content type = %s, want text/plain", gotContentType)
	}

	lines := strings.Split(strings.TrimSpace(gotBody), "\n")
	if len(lines) != 2 {
		t.Fatalf("body line count = %d, want 2; body=%s", len(lines), gotBody)
	}

	var first map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("first JSONL line is not valid JSON: %v; line=%s", err, lines[0])
	}

	if first["id"] != "product-1" {
		t.Fatalf("first document id = %v, want product-1", first["id"])
	}

	var second map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("second JSONL line is not valid JSON: %v; line=%s", err, lines[1])
	}

	if _, ok := second["description"]; ok {
		t.Fatalf("second document includes empty description, want it omitted; body=%s", gotBody)
	}
}

func TestUpsertDocumentsWithSameIDUpdatesExistingTypesenseDocument(t *testing.T) {
	indexedDocuments := make(map[string]map[string]any)
	var importCallCount int
	var lastImportBody string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		importCallCount++

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		lastImportBody = string(body)

		lines := strings.Split(strings.TrimSpace(lastImportBody), "\n")
		for _, line := range lines {
			var document map[string]any
			if err := json.Unmarshal([]byte(line), &document); err != nil {
				t.Fatalf("import body line is not valid JSON: %v; line=%s", err, line)
			}

			id, ok := document["id"].(string)
			if !ok || id == "" {
				t.Fatalf("document id is missing or not a string: %s", line)
			}

			// This mimics Typesense upsert behavior: same id means overwrite/update
			// the existing document instead of creating a second product.
			indexedDocuments[id] = document
		}

		w.WriteHeader(http.StatusOK)
		for id := range indexedDocuments {
			_, _ = w.Write([]byte(`{"success":true,"id":"` + id + `"}` + "\n"))
		}
	}))
	defer server.Close()

	client := NewClient(config.Config{
		TypesenseHost:       server.URL,
		TypesenseAPIKey:     "debug-key",
		TypesenseCollection: "products",
	})

	productID := "f4a1982b-4512-417d-938f-86965aba2a77"

	originalDocument := model.ProductDocument{
		ID:          productID,
		Name:        "Logitech MX Master 3S",
		Description: "Wireless mouse",
		Category:    "accessory",
		Price:       3900,
		Stock:       20,
		UpdatedAt:   1780918835,
	}

	if err := client.UpsertDocuments(context.Background(), []model.ProductDocument{originalDocument}); err != nil {
		t.Fatalf("first UpsertDocuments returned error: %v", err)
	}

	updatedDocument := originalDocument
	updatedDocument.Name = "Logitech MX Master 3S Black"
	updatedDocument.UpdatedAt = 1780920000

	if err := client.UpsertDocuments(context.Background(), []model.ProductDocument{updatedDocument}); err != nil {
		t.Fatalf("second UpsertDocuments returned error: %v", err)
	}

	if importCallCount != 2 {
		t.Fatalf("import call count = %d, want 2", importCallCount)
	}

	if len(indexedDocuments) != 1 {
		t.Fatalf("indexed document count = %d, want 1; documents=%v", len(indexedDocuments), indexedDocuments)
	}

	gotDocument := indexedDocuments[productID]
	if gotDocument["name"] != "Logitech MX Master 3S Black" {
		t.Fatalf("indexed name = %v, want %s", gotDocument["name"], "Logitech MX Master 3S Black")
	}

	if gotDocument["id"] != productID {
		t.Fatalf("indexed id = %v, want %s", gotDocument["id"], productID)
	}

	if !strings.Contains(lastImportBody, `"id":"`+productID+`"`) {
		t.Fatalf("last import body does not include product id; body=%s", lastImportBody)
	}

	if !strings.Contains(lastImportBody, `"name":"Logitech MX Master 3S Black"`) {
		t.Fatalf("last import body does not include updated name; body=%s", lastImportBody)
	}
}
