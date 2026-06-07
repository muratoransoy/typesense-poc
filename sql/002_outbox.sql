-- The outbox table stores search-index work that still needs to be sent to Typesense.
CREATE TABLE IF NOT EXISTS search_outbox (
    id BIGSERIAL PRIMARY KEY,
    collection_name TEXT NOT NULL,
    record_id TEXT NOT NULL,
    operation_type TEXT NOT NULL,
    payload JSONB,
    processed BOOLEAN NOT NULL DEFAULT FALSE,
    retry_count INT NOT NULL DEFAULT 0,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);

-- This trigger function converts product changes into simple search events.
-- processed=false means the Go worker has not handled the event yet.
-- retry_count and error_message make failed attempts visible while keeping the POC small.
CREATE OR REPLACE FUNCTION create_product_search_outbox_event()
RETURNS TRIGGER AS $$
BEGIN
    -- Hard delete: remove the Typesense document that uses the PostgreSQL id.
    IF TG_OP = 'DELETE' THEN
        INSERT INTO search_outbox(collection_name, record_id, operation_type, payload)
        VALUES ('products', OLD.id::text, 'delete', NULL);

        RETURN OLD;
    END IF;

    -- Soft delete: products stay in PostgreSQL but disappear from the search index.
    IF NEW.is_deleted = true THEN
        INSERT INTO search_outbox(collection_name, record_id, operation_type, payload)
        VALUES ('products', NEW.id::text, 'delete', NULL);

        RETURN NEW;
    END IF;

    -- Active insert/update: build the same document shape that Typesense expects.
    INSERT INTO search_outbox(collection_name, record_id, operation_type, payload)
    VALUES (
        'products',
        NEW.id::text,
        'upsert',
        jsonb_strip_nulls(jsonb_build_object(
            'id', NEW.id::text,
            'name', NEW.name,
            'description', NEW.description,
            'category', NEW.category,
            'price', NEW.price::float8,
            'stock', NEW.stock,
            'updated_at', extract(epoch from NEW.updated_at)::bigint
        ))
    );

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_products_search_outbox ON products;

CREATE TRIGGER trg_products_search_outbox
AFTER INSERT OR UPDATE OR DELETE ON products
FOR EACH ROW
EXECUTE FUNCTION create_product_search_outbox_event();
