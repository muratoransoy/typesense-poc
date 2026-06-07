-- pgcrypto gives us gen_random_uuid(), which is handy for simple UUID ids.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- PostgreSQL is the primary database in this POC.
-- Typesense is only the search index, so product data starts here.
CREATE TABLE IF NOT EXISTS products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    category TEXT NOT NULL,
    price NUMERIC(18,2) NOT NULL,
    stock INT NOT NULL DEFAULT 0,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Polling sync uses updated_at to find rows that changed after the last scan.
CREATE OR REPLACE FUNCTION set_products_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_products_updated_at ON products;

CREATE TRIGGER trg_products_updated_at
BEFORE UPDATE ON products
FOR EACH ROW
EXECUTE FUNCTION set_products_updated_at();

-- Seed data is inserted only when the table is empty, so manual reruns stay safe.
INSERT INTO products(name, description, category, price, stock)
SELECT name, description, category, price, stock
FROM (
    VALUES
        ('iPhone 15', 'Apple smartphone', 'phone', 54000, 10),
        ('Samsung S24', 'Android smartphone', 'phone', 42000, 15),
        ('MacBook Pro', 'Apple laptop', 'laptop', 95000, 5)
) AS seed(name, description, category, price, stock)
WHERE NOT EXISTS (SELECT 1 FROM products);
