-- +goose Up
CREATE TABLE iam.person (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    tg_user_id bigint NOT NULL UNIQUE,
    tg_first_name text,
    tg_username text,
    tg_lang_code text,
    phone_enc bytea,
    phone_verified_at timestamptz,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'frozen', 'disabled')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON COLUMN iam.person.phone_enc IS '应用层加密后存放';

CREATE TABLE iam.merchant (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    name jsonb NOT NULL,
    type text,
    area text,
    svc jsonb,
    description jsonb,
    phone_enc bytea,
    address_enc bytea,
    contact_enc bytea,
    kind text NOT NULL DEFAULT 'pending' CHECK (kind IN ('self', 'verified', 'pending')),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'frozen', 'disabled')),
    owner_account_id bigint,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON COLUMN iam.merchant.phone_enc IS '应用层加密后存放';
COMMENT ON COLUMN iam.merchant.address_enc IS '应用层加密后存放';
COMMENT ON COLUMN iam.merchant.contact_enc IS '应用层加密后存放';

CREATE TABLE iam.account (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    person_id bigint NOT NULL REFERENCES iam.person(id),
    role text NOT NULL CHECK (role IN ('buyer', 'merchant_owner', 'merchant_staff', 'driver')),
    merchant_id bigint REFERENCES iam.merchant(id),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'frozen', 'disabled')),
    status_reason text,
    lang text CHECK (lang IS NULL OR lang IN ('zh', 'en', 'km')),
    lang_registered_at timestamptz,
    kyc_status text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT ck_account_status_reason CHECK (status = 'active' OR nullif(btrim(status_reason), '') IS NOT NULL),
    CONSTRAINT ux_account_person_role_merchant UNIQUE NULLS NOT DISTINCT (person_id, role, merchant_id)
);

ALTER TABLE iam.merchant
    ADD CONSTRAINT fk_merchant_owner_account FOREIGN KEY (owner_account_id) REFERENCES iam.account(id);

CREATE TABLE iam.account_device (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    account_id bigint NOT NULL REFERENCES iam.account(id),
    device_id text NOT NULL,
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, device_id)
);

CREATE TABLE iam.bot (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    "end" text NOT NULL CHECK ("end" IN ('buyer', 'merchant', 'driver')),
    username text NOT NULL UNIQUE,
    role_in_pool text NOT NULL CHECK (role_in_pool IN ('primary', 'standby')),
    status text NOT NULL CHECK (status IN ('active', 'standby', 'limited', 'banned')),
    token_enc bytea NOT NULL,
    token_last4 text CHECK (token_last4 IS NULL OR char_length(token_last4) = 4),
    webhook_secret_enc bytea NOT NULL,
    priority integer NOT NULL CHECK (priority >= 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE ("end", priority)
);
COMMENT ON COLUMN iam.bot.token_enc IS '信封加密密文；AAD 约定 bot_token:<id>';
COMMENT ON COLUMN iam.bot.webhook_secret_enc IS '信封加密密文；AAD 约定 bot_token:<id>';
CREATE UNIQUE INDEX ux_bot_end_primary ON iam.bot ("end") WHERE role_in_pool = 'primary';

CREATE TABLE iam.account_bot (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    account_id bigint NOT NULL REFERENCES iam.account(id),
    bot_id bigint NOT NULL REFERENCES iam.bot(id),
    bound_at timestamptz NOT NULL DEFAULT now(),
    write_access boolean NOT NULL DEFAULT false,
    unreachable_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, bot_id)
);

CREATE TABLE iam.login_event (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    person_id bigint REFERENCES iam.person(id),
    bot_id bigint REFERENCES iam.bot(id),
    valid_by_bot_id bigint REFERENCES iam.bot(id),
    tried_count integer NOT NULL CHECK (tried_count > 0),
    auth_date timestamptz,
    ip inet,
    device_id text,
    result text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE iam.session (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    account_id bigint NOT NULL REFERENCES iam.account(id),
    refresh_token_hash bytea NOT NULL,
    device_id text,
    ip inet,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revoked_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON COLUMN iam.session.refresh_token_hash IS '只存刷新令牌哈希';
CREATE INDEX ix_session_account_id ON iam.session (account_id);
CREATE INDEX ix_session_expires_at ON iam.session (expires_at);

CREATE TABLE iam.merchant_staff (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    merchant_id bigint NOT NULL REFERENCES iam.merchant(id),
    account_id bigint REFERENCES iam.account(id),
    name text NOT NULL,
    phone_enc bytea NOT NULL,
    phone_hash bytea NOT NULL,
    tg_username text,
    note text,
    role text NOT NULL CHECK (role IN ('manager', 'sales', 'warehouse', 'finance')),
    status text NOT NULL CHECK (status IN ('invited', 'active', 'off')),
    invite_code text NOT NULL UNIQUE,
    invited_by bigint REFERENCES iam.account(id),
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (merchant_id, phone_hash)
);
COMMENT ON COLUMN iam.merchant_staff.phone_enc IS '应用层加密后存放';
COMMENT ON COLUMN iam.merchant_staff.phone_hash IS '规范化手机号的不可逆摘要，用于同商户内唯一性';

CREATE TABLE iam.merchant_role_perm (
    merchant_id bigint NOT NULL REFERENCES iam.merchant(id),
    role text NOT NULL CHECK (role IN ('manager', 'sales', 'warehouse', 'finance')),
    module text NOT NULL CHECK (module IN ('dash', 'prod', 'orders', 'store', 'fin', 'ads', 'prof', 'staff', 'ai')),
    level text NOT NULL CHECK (level IN ('none', 'view', 'edit')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (merchant_id, role, module),
    CHECK (module <> 'staff' OR level IN ('none', 'view'))
);

CREATE TABLE iam.merchant_role_perm_template (
    role text NOT NULL CHECK (role IN ('manager', 'sales', 'warehouse', 'finance')),
    module text NOT NULL CHECK (module IN ('dash', 'prod', 'orders', 'store', 'fin', 'ads', 'prof', 'staff', 'ai')),
    level text NOT NULL CHECK (level IN ('none', 'view', 'edit')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (role, module),
    CHECK (module <> 'staff' OR level IN ('none', 'view'))
);

CREATE TABLE iam.merchant_op_log (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    merchant_id bigint NOT NULL REFERENCES iam.merchant(id),
    actor_account_id bigint NOT NULL REFERENCES iam.account(id),
    action text NOT NULL,
    detail jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE iam.account_link (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    buyer_account_id bigint NOT NULL REFERENCES iam.account(id),
    merchant_account_id bigint NOT NULL REFERENCES iam.account(id),
    linked_at timestamptz NOT NULL DEFAULT now(),
    unlinked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (buyer_account_id <> merchant_account_id)
);
CREATE UNIQUE INDEX ux_account_link_active ON iam.account_link (buyer_account_id, merchant_account_id) WHERE unlinked_at IS NULL;

CREATE TABLE iam.admin_user (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    name text NOT NULL,
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    totp_secret_enc bytea,
    role text NOT NULL CHECK (role IN ('super', 'ops', 'finance', 'logi', 'cs', 'auditor')),
    data_scope text[] NOT NULL DEFAULT '{}',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'frozen', 'disabled')),
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON COLUMN iam.admin_user.password_hash IS '只存密码哈希';
COMMENT ON COLUMN iam.admin_user.totp_secret_enc IS '应用层加密后存放';

CREATE TABLE iam.admin_role_perm (
    role text NOT NULL CHECK (role IN ('super', 'ops', 'finance', 'logi', 'cs', 'auditor')),
    module text NOT NULL CHECK (module IN ('dash', 'usr', 'mer', 'prod', 'ord', 'fund', 'wd', 'mod', 'logi', 'integ', 'rbac', 'sens', 'cfg', 'fin', 'aud', 'ops', 'disp')),
    level text NOT NULL CHECK (level IN ('none', 'view', 'edit')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (role, module),
    CHECK (role <> 'super' OR level = 'edit')
);

CREATE TABLE iam.actor (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    public_id text NOT NULL UNIQUE CHECK (public_id ~ '^[0-9A-HJKMNP-TV-Z]{26}$'),
    kind text NOT NULL CHECK (kind IN ('account', 'admin', 'system', 'external')),
    ref_id bigint NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, ref_id)
);

-- Default privileges grant report_ro table-wide SELECT; narrow sensitive tables to safe columns.
REVOKE SELECT ON iam.person, iam.merchant, iam.bot, iam.session, iam.merchant_staff, iam.admin_user FROM report_ro;
GRANT SELECT (id, public_id, tg_user_id, tg_first_name, tg_username, tg_lang_code, phone_verified_at, status, created_at, updated_at) ON iam.person TO report_ro;
GRANT SELECT (id, public_id, name, type, area, svc, description, kind, status, owner_account_id, created_at, updated_at) ON iam.merchant TO report_ro;
GRANT SELECT (id, public_id, "end", username, role_in_pool, status, token_last4, priority, created_at, updated_at) ON iam.bot TO report_ro;
GRANT SELECT (id, public_id, account_id, device_id, ip, expires_at, revoked_at, revoked_reason, created_at, updated_at) ON iam.session TO report_ro;
GRANT SELECT (id, public_id, merchant_id, account_id, name, tg_username, note, role, status, invite_code, invited_by, last_login_at, created_at, updated_at) ON iam.merchant_staff TO report_ro;
GRANT SELECT (id, public_id, name, email, role, data_scope, status, last_login_at, created_at, updated_at) ON iam.admin_user TO report_ro;

-- seed:start
INSERT INTO iam.admin_role_perm (role, module, level) VALUES
    ('super','dash','edit'),('ops','dash','view'),('finance','dash','view'),('logi','dash','view'),('cs','dash','view'),('auditor','dash','view'),
    ('super','usr','edit'),('ops','usr','edit'),('finance','usr','view'),('logi','usr','none'),('cs','usr','view'),('auditor','usr','view'),
    ('super','mer','edit'),('ops','mer','edit'),('finance','mer','view'),('logi','mer','view'),('cs','mer','view'),('auditor','mer','view'),
    ('super','prod','edit'),('ops','prod','edit'),('finance','prod','none'),('logi','prod','none'),('cs','prod','view'),('auditor','prod','view'),
    ('super','ord','edit'),('ops','ord','view'),('finance','ord','view'),('logi','ord','view'),('cs','ord','view'),('auditor','ord','view'),
    ('super','fund','edit'),('ops','fund','none'),('finance','fund','edit'),('logi','fund','none'),('cs','fund','none'),('auditor','fund','view'),
    ('super','wd','edit'),('ops','wd','none'),('finance','wd','edit'),('logi','wd','none'),('cs','wd','none'),('auditor','wd','view'),
    ('super','mod','edit'),('ops','mod','edit'),('finance','mod','none'),('logi','mod','none'),('cs','mod','none'),('auditor','mod','view'),
    ('super','logi','edit'),('ops','logi','view'),('finance','logi','none'),('logi','logi','edit'),('cs','logi','none'),('auditor','logi','view'),
    ('super','integ','edit'),('ops','integ','none'),('finance','integ','none'),('logi','integ','view'),('cs','integ','none'),('auditor','integ','view'),
    ('super','rbac','edit'),('ops','rbac','none'),('finance','rbac','none'),('logi','rbac','none'),('cs','rbac','none'),('auditor','rbac','view'),
    ('super','sens','edit'),('ops','sens','none'),('finance','sens','view'),('logi','sens','none'),('cs','sens','view'),('auditor','sens','none'),
    ('super','cfg','edit'),('ops','cfg','edit'),('finance','cfg','none'),('logi','cfg','none'),('cs','cfg','none'),('auditor','cfg','view'),
    ('super','fin','edit'),('ops','fin','none'),('finance','fin','edit'),('logi','fin','none'),('cs','fin','none'),('auditor','fin','view'),
    ('super','aud','edit'),('ops','aud','none'),('finance','aud','view'),('logi','aud','none'),('cs','aud','none'),('auditor','aud','edit'),
    ('super','ops','edit'),('ops','ops','edit'),('finance','ops','view'),('logi','ops','view'),('cs','ops','view'),('auditor','ops','view'),
    ('super','disp','edit'),('ops','disp','edit'),('finance','disp','view'),('logi','disp','none'),('cs','disp','none'),('auditor','disp','view')
ON CONFLICT (role, module) DO NOTHING;

INSERT INTO iam.merchant_role_perm_template (role, module, level) VALUES
    ('manager','dash','view'),('manager','prod','edit'),('manager','orders','edit'),('manager','store','edit'),('manager','fin','view'),('manager','ads','edit'),('manager','prof','edit'),('manager','staff','view'),('manager','ai','edit'),
    ('sales','dash','view'),('sales','prod','edit'),('sales','orders','edit'),('sales','store','view'),('sales','fin','none'),('sales','ads','none'),('sales','prof','view'),('sales','staff','none'),('sales','ai','edit'),
    ('warehouse','dash','view'),('warehouse','prod','view'),('warehouse','orders','view'),('warehouse','store','edit'),('warehouse','fin','none'),('warehouse','ads','none'),('warehouse','prof','view'),('warehouse','staff','none'),('warehouse','ai','edit'),
    ('finance','dash','view'),('finance','prod','none'),('finance','orders','view'),('finance','store','view'),('finance','fin','edit'),('finance','ads','view'),('finance','prof','view'),('finance','staff','none'),('finance','ai','edit')
ON CONFLICT (role, module) DO NOTHING;
-- seed:end

-- +goose Down
DROP TABLE iam.actor;
DROP TABLE iam.admin_role_perm;
DROP TABLE iam.admin_user;
DROP TABLE iam.account_link;
DROP TABLE iam.merchant_op_log;
DROP TABLE iam.merchant_role_perm_template;
DROP TABLE iam.merchant_role_perm;
DROP TABLE iam.merchant_staff;
DROP TABLE iam.session;
DROP TABLE iam.login_event;
DROP TABLE iam.account_bot;
DROP TABLE iam.bot;
DROP TABLE iam.account_device;
ALTER TABLE iam.merchant DROP CONSTRAINT fk_merchant_owner_account;
DROP TABLE iam.account;
DROP TABLE iam.merchant;
DROP TABLE iam.person;
