package model

import "time"

// Product is the PostgreSQL row model.
// PostgreSQL is the primary database, so this struct mirrors the products table.
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

// ProductDocument is the Typesense document model.
// The PostgreSQL id is reused as the Typesense document id so upsert/delete
// operations always target the same product.
type ProductDocument struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Category    string  `json:"category"`
	Price       float64 `json:"price"`
	Stock       int     `json:"stock"`
	UpdatedAt   int64   `json:"updated_at"`
}

// ToDocument maps a database row into the JSON shape expected by Typesense.
func (p Product) ToDocument() ProductDocument {
	description := ""
	if p.Description != nil {
		description = *p.Description
	}

	return ProductDocument{
		ID:          p.ID,
		Name:        p.Name,
		Description: description,
		Category:    p.Category,
		Price:       p.Price,
		Stock:       p.Stock,
		UpdatedAt:   p.UpdatedAt.Unix(),
	}
}
