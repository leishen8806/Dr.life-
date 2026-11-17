-- +goose Up
-- Record when a product entered the administrative review queue. This is
-- distinct from created_at because a draft can be submitted much later.
ALTER TABLE catalog.product ADD COLUMN submitted_at timestamptz;
CREATE INDEX ix_product_review_submitted_at
    ON catalog.product (submitted_at DESC, id DESC)
    WHERE status = 'review';

-- +goose Down
DROP INDEX IF EXISTS catalog.ix_product_review_submitted_at;
ALTER TABLE catalog.product DROP COLUMN submitted_at;
