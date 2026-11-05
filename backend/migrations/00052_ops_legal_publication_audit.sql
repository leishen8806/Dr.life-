-- +goose Up
ALTER TABLE ops.legal_doc ADD COLUMN approval_ref text;
ALTER TABLE ops.legal_doc
    ADD CONSTRAINT fk_legal_doc_published_by_admin_user
    FOREIGN KEY (published_by) REFERENCES iam.admin_user(id);
ALTER TABLE ops.legal_doc
    ADD CONSTRAINT ck_legal_doc_approval_ref
    CHECK (published_at IS NULL OR (approval_ref IS NOT NULL AND length(btrim(approval_ref)) > 0));

-- +goose Down
ALTER TABLE ops.legal_doc DROP CONSTRAINT ck_legal_doc_approval_ref;
ALTER TABLE ops.legal_doc DROP CONSTRAINT fk_legal_doc_published_by_admin_user;
ALTER TABLE ops.legal_doc DROP COLUMN approval_ref;
