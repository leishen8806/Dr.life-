-- +goose Up
-- V8 A1: retain the platform decision reason so merchants can see why a
-- product was rejected. The service layer still owns transition validation.
ALTER TABLE catalog.product
    ADD COLUMN review_reason text;

COMMENT ON COLUMN catalog.product.review_reason IS
    '平台审核驳回原因；status=rejected 时供商家查看，上架后清空';

-- +goose Down
ALTER TABLE catalog.product
    DROP COLUMN review_reason;
