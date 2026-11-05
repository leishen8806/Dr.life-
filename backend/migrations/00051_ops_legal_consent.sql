-- +goose Up
CREATE TABLE ops.legal_doc (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('privacy', 'terms')),
    version text NOT NULL CHECK (version ~ '^v[0-9]+\.[0-9]+$'),
    effective_at timestamptz NOT NULL,
    body_zh text NOT NULL CHECK (length(btrim(body_zh)) > 0),
    body_en text NOT NULL CHECK (length(btrim(body_en)) > 0),
    body_km text NOT NULL CHECK (length(btrim(body_km)) > 0),
    published_by bigint,
    published_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT uq_legal_doc_kind_version UNIQUE (kind, version),
    CONSTRAINT ck_legal_doc_publish CHECK (published_at IS NULL OR published_by IS NOT NULL)
);
CREATE INDEX ix_legal_doc_current ON ops.legal_doc (kind, effective_at DESC) WHERE published_at IS NOT NULL;

CREATE TABLE ops.consent (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id bigint NOT NULL REFERENCES iam.account(id),
    kind text NOT NULL CHECK (kind IN ('privacy', 'terms')),
    version text NOT NULL CHECK (version ~ '^v[0-9]+\.[0-9]+$'),
    accepted_at timestamptz NOT NULL,
    channel text NOT NULL CHECK (channel IN ('mini_app')),
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX ix_consent_active ON ops.consent (account_id, kind, version) WHERE revoked_at IS NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ops.guard_legal_doc_publication() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    latest_version text;
BEGIN
    IF TG_OP IN ('UPDATE', 'DELETE') AND OLD.published_at IS NOT NULL THEN
        RAISE EXCEPTION 'published legal documents are immutable' USING ERRCODE = 'P0001';
    END IF;
    IF TG_OP <> 'DELETE' AND NEW.published_at IS NOT NULL THEN
        PERFORM pg_advisory_xact_lock(hashtext('ops.legal_doc:' || NEW.kind));
        SELECT version INTO latest_version
        FROM ops.legal_doc
        WHERE kind = NEW.kind AND published_at IS NOT NULL
        ORDER BY split_part(substring(version FROM 2), '.', 1)::int DESC, split_part(version, '.', 2)::int DESC
        LIMIT 1;
        IF latest_version IS NOT NULL AND
           (split_part(substring(NEW.version FROM 2), '.', 1)::int, split_part(NEW.version, '.', 2)::int) <=
           (split_part(substring(latest_version FROM 2), '.', 1)::int, split_part(latest_version, '.', 2)::int) THEN
            RAISE EXCEPTION 'published legal document version must increase' USING ERRCODE = 'P0001';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER trg_legal_doc_publication_guard
    BEFORE UPDATE OR DELETE ON ops.legal_doc
    FOR EACH ROW EXECUTE FUNCTION ops.guard_legal_doc_publication();
CREATE TRIGGER trg_legal_doc_publish_version
    BEFORE INSERT ON ops.legal_doc
    FOR EACH ROW EXECUTE FUNCTION ops.guard_legal_doc_publication();

-- Source: Dr.life+用户协议与隐私政策草案(三语), v1.0. The draft remains subject to legal and Khmer-language review.
INSERT INTO ops.legal_doc (kind, version, effective_at, body_zh, body_en, body_km, published_by, published_at) VALUES
('terms', 'v1.0', '2026-10-09T00:00:00Z', 'DR.LIFE+ 用户协议\n版本 1.0。平台通过 Telegram Mini App 提供商品展示、仓储配送、订单及资金记录服务；销售佣金 0%，结算服务费 0%，不向司机收费，不放贷、不担保。用户以 Telegram 账户登录；新版本发布后需要重新阅读并同意。', 'DR.LIFE+ User Agreement\nVersion 1.0. The Platform provides product listings, warehousing and delivery, order and funds records through Telegram Mini Apps. Sales commission and settlement service fees are 0%; drivers are not charged. The Platform does not lend or guarantee. New versions require renewed agreement.', 'DR.LIFE+ កិច្ចព្រមព្រៀងអ្នកប្រើប្រាស់\nកំណែ 1.0។ វេទិកាផ្តល់សេវាបង្ហាញទំនិញ ឃ្លាំង និងដឹកជញ្ជូន តាម Telegram Mini App។ កម្រៃជើងសារ និងថ្លៃសេវាទូទាត់គឺ 0%។ វេទិកាមិនផ្តល់កម្ចី ឬធានាទេ។ កំណែថ្មីត្រូវការការយល់ព្រមឡើងវិញ។', NULL, NULL),
('privacy', 'v1.0', '2026-10-09T00:00:00Z', 'DR.LIFE+ 隐私政策\n版本 1.0。平台按提供服务所需处理账户、订单和配送信息；发送外部 AI 前先去除个人信息；聊天原文不用于画像。用户可查看、更正、删除、导出数据及撤回同意。', 'DR.LIFE+ Privacy Policy\nVersion 1.0. The Platform processes account, order and delivery information needed for its services. Personal information is removed before external AI use, and chat transcripts are not used for profiles. Users may access, correct, delete or export data and withdraw consent.', 'DR.LIFE+ គោលនយោបាយឯកជនភាព\nកំណែ 1.0។ វេទិកាដំណើរការព័ត៌មានគណនី ការបញ្ជាទិញ និងការដឹកជញ្ជូនតាមតម្រូវការសេវា។ ព័ត៌មានផ្ទាល់ខ្លួនត្រូវបានដកចេញមុនផ្ញើទៅ AI ខាងក្រៅ។ អ្នកប្រើអាចចូលមើល កែ លុប នាំចេញទិន្នន័យ និងដកការយល់ព្រម។', NULL, NULL);

REVOKE ALL ON ops.legal_doc, ops.consent FROM ops_rw;
REVOKE ALL ON ops.legal_doc, ops.consent FROM report_ro;
REVOKE ALL ON SEQUENCE ops.legal_doc_id_seq, ops.consent_id_seq FROM ops_rw;
GRANT SELECT ON ops.legal_doc TO ops_rw;
GRANT SELECT, INSERT ON ops.consent TO ops_rw;
GRANT UPDATE (revoked_at) ON ops.consent TO ops_rw;
GRANT USAGE, SELECT ON SEQUENCE ops.consent_id_seq TO ops_rw;

-- +goose Down
DROP TRIGGER trg_legal_doc_publish_version ON ops.legal_doc;
DROP TRIGGER trg_legal_doc_publication_guard ON ops.legal_doc;
DROP FUNCTION ops.guard_legal_doc_publication();
DROP TABLE ops.consent;
DROP TABLE ops.legal_doc;
