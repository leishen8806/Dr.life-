-- +goose Up
-- Product moderation emits a catalog-local outbox row in the same transaction
-- as the status decision. A worker can later deliver it to platform audit.
CREATE TABLE catalog.product_review_audit_outbox (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    occurred_at timestamptz NOT NULL,
    actor_id bigint NOT NULL,
    actor_role text NOT NULL,
    action text NOT NULL CHECK (action = 'product.moderated'),
    target text NOT NULL,
    detail jsonb NOT NULL,
    ip inet,
    request_id text NOT NULL,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error text,
    delivered_at timestamptz
);

CREATE INDEX ix_product_review_audit_outbox_pending
    ON catalog.product_review_audit_outbox (id)
    WHERE delivered_at IS NULL;

CREATE INDEX ix_product_review_audit_outbox_delivered
    ON catalog.product_review_audit_outbox (delivered_at)
    WHERE delivered_at IS NOT NULL;

-- +goose Down
DROP TABLE catalog.product_review_audit_outbox;
