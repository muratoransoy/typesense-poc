package typesense

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"typesense-poc/internal/config"
	"typesense-poc/internal/model"
)

// Client is a tiny HTTP wrapper around the Typesense API.
// The official SDK is not needed for this POC; net/http keeps the flow visible.
type Client struct {
	host       string
	apiKey     string
	collection string
	httpClient *http.Client
}

// NewClient creates a Typesense client from config.
func NewClient(cfg config.Config) *Client {
	return &Client{
		host:       strings.TrimRight(cfg.TypesenseHost, "/"),
		apiKey:     cfg.TypesenseAPIKey,
		collection: cfg.TypesenseCollection,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// CreateProductsCollection creates the products collection if it does not exist.
func (c *Client) CreateProductsCollection(ctx context.Context) error {
	schema := map[string]any{
		"name": c.collection,
		"fields": []map[string]any{
			{"name": "name", "type": "string"},
			{"name": "description", "type": "string", "optional": true},
			{"name": "category", "type": "string", "facet": true},
			{"name": "price", "type": "float"},
			{"name": "stock", "type": "int32"},
			{"name": "updated_at", "type": "int64"},
		},
		"default_sorting_field": "updated_at",
	}

	body, err := json.Marshal(schema)
	if err != nil {
		return err
	}

	req, err := c.newRequest(ctx, http.MethodPost, "/collections", bytes.NewReader(body), "application/json")
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		return nil
	}

	// 409 means the collection already exists, which is fine for repeated POC runs.
	if resp.StatusCode == http.StatusConflict {
		return nil
	}

	return c.responseError(resp, "create collection")
}

// UpsertDocuments sends PostgreSQL products to Typesense using the import API.
func (c *Client) UpsertDocuments(ctx context.Context, docs []model.ProductDocument) error {
	if len(docs) == 0 {
		return nil
	}

	var jsonl bytes.Buffer
	for _, doc := range docs {
		line, err := json.Marshal(doc)
		if err != nil {
			return err
		}

		// Typesense import expects JSONL: one JSON document per line.
		// It does not accept a JSON array like [{"id":"1"}, {"id":"2"}].
		jsonl.Write(line)
		jsonl.WriteByte('\n')
	}

	return c.importJSONL(ctx, jsonl.Bytes())
}

// UpsertRawJSON imports one JSON object that came from the outbox payload.
func (c *Client) UpsertRawJSON(ctx context.Context, rawJSON []byte) error {
	rawJSON = bytes.TrimSpace(rawJSON)
	if len(rawJSON) == 0 {
		return nil
	}

	// The outbox stores one JSON object. Add a newline to turn it into JSONL.
	return c.importJSONL(ctx, append(rawJSON, '\n'))
}

// DeleteDocument removes one document from Typesense by PostgreSQL product id.
func (c *Client) DeleteDocument(ctx context.Context, id string) error {
	path := fmt.Sprintf(
		"/collections/%s/documents/%s",
		url.PathEscape(c.collection),
		url.PathEscape(id),
	)

	req, err := c.newRequest(ctx, http.MethodDelete, path, nil, "")
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	// Delete events are idempotent in this POC: a missing document is already gone.
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}

	return c.responseError(resp, "delete document")
}

func (c *Client) importJSONL(ctx context.Context, jsonl []byte) error {
	path := fmt.Sprintf(
		"/collections/%s/documents/import?action=upsert",
		url.PathEscape(c.collection),
	)

	req, err := c.newRequest(ctx, http.MethodPost, path, bytes.NewReader(jsonl), "text/plain")
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("import documents: status=%d body=%s", resp.StatusCode, string(body))
	}

	return checkImportResult(body)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader, contentType string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("X-TYPESENSE-API-KEY", c.apiKey)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	return req, nil
}

func (c *Client) responseError(resp *http.Response, action string) error {
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("%s: status=%d body=%s", action, resp.StatusCode, string(body))
}

type importResult struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
	Error   string `json:"error"`
}

func checkImportResult(body []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var result importResult
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			return fmt.Errorf("parse import result: %w; line=%s", err, line)
		}

		if !result.Success {
			return fmt.Errorf("import document id=%s failed: %s", result.ID, result.Error)
		}
	}

	return scanner.Err()
}
