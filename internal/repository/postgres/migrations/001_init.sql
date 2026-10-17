CREATE TABLE products (
    id TEXT PRIMARY KEY,
    sku TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    price_cents BIGINT NOT NULL,
    on_hand INTEGER NOT NULL,
    reserved INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT products_sku_len CHECK (char_length(sku) BETWEEN 1 AND 64),
    CONSTRAINT products_name_len CHECK (char_length(name) BETWEEN 1 AND 200),
    CONSTRAINT products_description_len CHECK (char_length(description) <= 2000),
    CONSTRAINT products_price_non_negative CHECK (price_cents >= 0),
    CONSTRAINT products_on_hand_non_negative CHECK (on_hand >= 0),
    CONSTRAINT products_reserved_non_negative CHECK (reserved >= 0),
    CONSTRAINT products_reserved_lte_on_hand CHECK (reserved <= on_hand)
);

CREATE TABLE reservations (
    id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    product_id TEXT NOT NULL REFERENCES products (id),
    quantity INTEGER NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT reservations_key_len CHECK (char_length(idempotency_key) BETWEEN 1 AND 128),
    CONSTRAINT reservations_quantity_positive CHECK (quantity > 0),
    CONSTRAINT reservations_status_known CHECK (status IN ('held', 'committed', 'released'))
);

CREATE INDEX reservations_product_id_idx ON reservations (product_id);
