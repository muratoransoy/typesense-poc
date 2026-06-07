package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	postgresURL         = "postgres://postgres:postgres@localhost:5432/appdb?sslmode=disable"
	typesenseHost       = "http://localhost:8108"
	typesenseAPIKey     = "typesense123"
	typesenseCollection = "products"
)

type Product struct {
	ID          string
	Name        string
	Description *string
	Category    string
	Price       float64
	Stock       int
	IsDeleted   bool
	UpdatedAt   time.Time
}

type ProductDocument struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Category    string  `json:"category"`
	Price       float64 `json:"price"`
	Stock       int     `json:"stock"`
	UpdatedAt   int64   `json:"updated_at"`
}

func main() {
	ctx := context.Background()

	db, err := pgxpool.New(ctx, postgresURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := createTypesenseCollection(ctx); err != nil {
		log.Fatal(err)
	}

	log.Println("Polling worker started...")

	lastSyncedAt := time.Time{}

	for {
		if err := syncProducts(ctx, db, &lastSyncedAt); err != nil {
			log.Println("sync error:", err)
		}

		time.Sleep(5 * time.Second)
	}
}

func syncProducts(ctx context.Context, db *pgxpool.Pool, lastSyncedAt *time.Time) error {
	now := time.Now().UTC()

	rows, err := db.Query(ctx, `
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
		LIMIT 100
	`, *lastSyncedAt, now)
	if err != nil {
		return err
	}
	defer rows.Close()

	var products []Product

	for rows.Next() {
		var p Product

		err := rows.Scan(
			&p.ID,
			&p.Name,
			&p.Description,
			&p.Category,
			&p.Price,
			&p.Stock,
			&p.IsDeleted,
			&p.UpdatedAt,
		)
		if err != nil {
			return err
		}

		products = append(products, p)
	}

	if len(products) == 0 {
		return nil
	}

	var docs []ProductDocument

	for _, p := range products {
		if p.IsDeleted {
			if err := deleteDocument(ctx, p.ID); err != nil {
				return err
			}

			continue
		}

		description := ""
		if p.Description != nil {
			description = *p.Description
		}

		docs = append(docs, ProductDocument{
			ID:          p.ID,
			Name:        p.Name,
			Description: description,
			Category:    p.Category,
			Price:       p.Price,
			Stock:       p.Stock,
			UpdatedAt:   p.UpdatedAt.Unix(),
		})
	}

	if len(docs) > 0 {
		if err := upsertDocuments(ctx, docs); err != nil {
			return err
		}
	}

	*lastSyncedAt = products[len(products)-1].UpdatedAt

	log.Printf("synced product count=%d lastSyncedAt=%s\n", len(products), lastSyncedAt.Format(time.RFC3339))

	return nil
}

func createTypesenseCollection(ctx context.Context) error {
	body := `{
		"name": "products",
		"fields": [
			{ "name": "name", "type": "string" },
			{ "name": "description", "type": "string", "optional": true },
			{ "name": "category", "type": "string", "facet": true },
			{ "name": "price", "type": "float" },
			{ "name": "stock", "type": "int32" },
			{ "name": "updated_at", "type": "int64" }
		],
		"default_sorting_field": "updated_at"
	}`

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		typesenseHost+"/collections",
		strings.NewReader(body),
	)
	if err != nil {
		return err
	}

	req.Header.Set("X-TYPESENSE-API-KEY", typesenseAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		log.Println("Typesense collection already exists")
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("create collection failed status=%d", resp.StatusCode)
	}

	log.Println("Typesense collection created")

	return nil
}

func upsertDocuments(ctx context.Context, docs []ProductDocument) error {
	jsonl, err := toJSONL(docs)
	if err != nil {
		return err
	}

	url := fmt.Sprintf(
		"%s/collections/%s/documents/import?action=upsert",
		typesenseHost,
		typesenseCollection,
	)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewBufferString(jsonl),
	)
	if err != nil {
		return err
	}

	req.Header.Set("X-TYPESENSE-API-KEY", typesenseAPIKey)
	req.Header.Set("Content-Type", "text/plain")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upsert failed status=%d", resp.StatusCode)
	}

	return nil
}

func deleteDocument(ctx context.Context, id string) error {
	url := fmt.Sprintf(
		"%s/collections/%s/documents/%s",
		typesenseHost,
		typesenseCollection,
		id,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("X-TYPESENSE-API-KEY", typesenseAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("delete failed status=%d", resp.StatusCode)
	}

	return nil
}

func toJSONL(docs []ProductDocument) (string, error) {
	var builder strings.Builder

	for i, doc := range docs {
		raw, err := json.Marshal(doc)
		if err != nil {
			return "", err
		}

		if i > 0 {
			builder.WriteString("\n")
		}

		builder.Write(raw)
	}

	return builder.String(), nil
}
