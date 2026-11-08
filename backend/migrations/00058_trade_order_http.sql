-- +goose Up
ALTER TABLE trade.buyer_order
    ADD COLUMN goods_amount_e4 bigint NOT NULL DEFAULT 0,
    ADD COLUMN shipping_amount_e4 bigint NOT NULL DEFAULT 0,
    ADD COLUMN payable_amount_e4 bigint NOT NULL DEFAULT 0,
    ADD COLUMN shipping_quote_expires_at timestamptz,
    ADD COLUMN shipping_address_public_id text,
    ADD COLUMN shipping_recipient_name text,
    ADD COLUMN shipping_address_text text,
    ADD COLUMN shipping_phone text;
UPDATE trade.buyer_order SET goods_amount_e4=total_e4,payable_amount_e4=total_e4;
SET CONSTRAINTS ALL IMMEDIATE;
ALTER TABLE trade.buyer_order
    ADD CONSTRAINT ck_trade_buyer_order_amounts CHECK (
        goods_amount_e4 = total_e4 AND goods_amount_e4 >= 0 AND goods_amount_e4 % 100 = 0 AND
        shipping_amount_e4 >= 0 AND shipping_amount_e4 % 100 = 0 AND
        payable_amount_e4 = goods_amount_e4 + shipping_amount_e4),
    ADD CONSTRAINT ck_trade_buyer_order_address_snapshot CHECK (
        (shipping_address_public_id IS NULL AND shipping_recipient_name IS NULL AND shipping_address_text IS NULL AND shipping_phone IS NULL)
        OR (length(btrim(shipping_address_public_id)) > 0 AND length(btrim(shipping_recipient_name)) > 0 AND length(btrim(shipping_address_text)) > 0 AND length(btrim(shipping_phone)) > 0));

ALTER TABLE trade.merchant_order
    ADD COLUMN merchant_public_id text,
    ADD COLUMN goods_amount_e4 bigint NOT NULL DEFAULT 0,
    ADD COLUMN shipping_amount_e4 bigint NOT NULL DEFAULT 0,
    ADD COLUMN payable_amount_e4 bigint NOT NULL DEFAULT 0,
    ADD COLUMN shipping_quote_expires_at timestamptz;
UPDATE trade.merchant_order SET goods_amount_e4=total_e4,payable_amount_e4=total_e4;
SET CONSTRAINTS ALL IMMEDIATE;
ALTER TABLE trade.merchant_order ADD CONSTRAINT ck_trade_merchant_order_amounts CHECK (
    goods_amount_e4 = total_e4 AND goods_amount_e4 >= 0 AND goods_amount_e4 % 100 = 0 AND
    shipping_amount_e4 >= 0 AND shipping_amount_e4 % 100 = 0 AND
    payable_amount_e4 = goods_amount_e4 + shipping_amount_e4);

ALTER TABLE trade.kess_payment_intent ADD COLUMN expires_at timestamptz;
SET CONSTRAINTS ALL DEFERRED;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.assert_buyer_order_total() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_id bigint;
    target_ids bigint[];
    expected_goods_e4 bigint;
    expected_shipping_e4 bigint;
    expected_payable_e4 bigint;
    actual_goods_e4 bigint;
    actual_shipping_e4 bigint;
    actual_payable_e4 bigint;
BEGIN
    IF TG_TABLE_NAME = 'buyer_order' THEN
        IF TG_OP = 'INSERT' THEN target_ids := ARRAY[NEW.id];
        ELSIF TG_OP = 'DELETE' THEN target_ids := ARRAY[OLD.id];
        ELSE target_ids := ARRAY[OLD.id, NEW.id];
        END IF;
    ELSE
        IF TG_OP = 'INSERT' THEN target_ids := ARRAY[NEW.buyer_order_id];
        ELSIF TG_OP = 'DELETE' THEN target_ids := ARRAY[OLD.buyer_order_id];
        ELSE target_ids := ARRAY[OLD.buyer_order_id, NEW.buyer_order_id];
        END IF;
    END IF;
    FOR target_id IN SELECT DISTINCT unnest(target_ids) LOOP
        SELECT goods_amount_e4, shipping_amount_e4, payable_amount_e4
          INTO expected_goods_e4, expected_shipping_e4, expected_payable_e4
          FROM trade.buyer_order WHERE id = target_id;
        IF FOUND THEN
            SELECT COALESCE(sum(goods_amount_e4), 0), COALESCE(sum(shipping_amount_e4), 0), COALESCE(sum(payable_amount_e4), 0)
              INTO actual_goods_e4, actual_shipping_e4, actual_payable_e4
              FROM trade.merchant_order WHERE buyer_order_id = target_id;
            IF (expected_goods_e4, expected_shipping_e4, expected_payable_e4)
                IS DISTINCT FROM (actual_goods_e4, actual_shipping_e4, actual_payable_e4) THEN
                RAISE EXCEPTION 'buyer order amounts do not equal merchant order amounts' USING ERRCODE = '23514';
            END IF;
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.guard_buyer_order_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.public_id <> OLD.public_id OR NEW.buyer_account_id <> OLD.buyer_account_id
       OR NEW.idempotency_key <> OLD.idempotency_key OR NEW.request_hash <> OLD.request_hash
       OR NEW.total_e4 <> OLD.total_e4 OR NEW.goods_amount_e4 <> OLD.goods_amount_e4
       OR NEW.shipping_amount_e4 <> OLD.shipping_amount_e4 OR NEW.payable_amount_e4 <> OLD.payable_amount_e4
       OR NEW.shipping_quote_expires_at IS DISTINCT FROM OLD.shipping_quote_expires_at
       OR NEW.shipping_address_public_id IS DISTINCT FROM OLD.shipping_address_public_id
       OR NEW.shipping_recipient_name IS DISTINCT FROM OLD.shipping_recipient_name
       OR NEW.shipping_address_text IS DISTINCT FROM OLD.shipping_address_text
       OR NEW.shipping_phone IS DISTINCT FROM OLD.shipping_phone OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'trade.buyer_order business facts are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.reservation_released_at IS DISTINCT FROM OLD.reservation_released_at THEN
        IF OLD.reservation_released_at IS NOT NULL OR NEW.reservation_released_at IS NULL
           OR EXISTS (SELECT 1 FROM trade.merchant_order WHERE buyer_order_id=OLD.id AND status NOT IN ('awaiting_payment','cancelled')) THEN
            RAISE EXCEPTION 'trade.buyer_order reservation release is irreversible and unpaid-only' USING ERRCODE = '55000';
        END IF;
    END IF;
    IF NEW.reservation_committed_at IS DISTINCT FROM OLD.reservation_committed_at THEN
        IF OLD.reservation_committed_at IS NOT NULL OR NEW.reservation_committed_at IS NULL
           OR NEW.reservation_released_at IS NOT NULL OR NEW.refund_required_at IS NOT NULL
           OR NOT EXISTS (SELECT 1 FROM trade.merchant_order WHERE buyer_order_id=OLD.id)
           OR EXISTS (SELECT 1 FROM trade.merchant_order WHERE buyer_order_id=OLD.id AND status <> 'paid') THEN
            RAISE EXCEPTION 'trade.buyer_order reservation commit is irreversible and paid-only' USING ERRCODE = '55000';
        END IF;
    END IF;
    IF NEW.refund_required_at IS DISTINCT FROM OLD.refund_required_at THEN
        IF OLD.refund_required_at IS NOT NULL OR NEW.refund_required_at IS NULL
           OR NOT EXISTS (SELECT 1 FROM trade.merchant_order WHERE buyer_order_id=OLD.id)
           OR EXISTS (SELECT 1 FROM trade.merchant_order WHERE buyer_order_id=OLD.id AND status <> 'paid') THEN
            RAISE EXCEPTION 'trade.buyer_order refund requirement is irreversible and paid-only' USING ERRCODE = '55000';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.guard_merchant_order_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.public_id <> OLD.public_id OR NEW.buyer_order_id <> OLD.buyer_order_id
       OR NEW.merchant_id <> OLD.merchant_id OR NEW.merchant_public_id IS DISTINCT FROM OLD.merchant_public_id OR NEW.external_order_id <> OLD.external_order_id
       OR NEW.total_e4 <> OLD.total_e4 OR NEW.goods_amount_e4 <> OLD.goods_amount_e4
       OR NEW.shipping_amount_e4 <> OLD.shipping_amount_e4 OR NEW.payable_amount_e4 <> OLD.payable_amount_e4
       OR NEW.shipping_quote_expires_at IS DISTINCT FROM OLD.shipping_quote_expires_at OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'trade.merchant_order business facts are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.status <> OLD.status THEN
        IF NOT ((OLD.status = 'awaiting_payment' AND NEW.status IN ('paid','cancelled'))
            OR (OLD.status = 'paid' AND NEW.status = 'submitted')
            OR (OLD.status = 'submitted' AND NEW.status IN ('accepted','rejected'))
            OR (OLD.status = 'accepted' AND NEW.status = 'in_fulfillment')
            OR (OLD.status = 'in_fulfillment' AND NEW.status = 'shipped')
            OR (OLD.status = 'shipped' AND NEW.status = 'delivered_verified')
            OR (OLD.status = 'delivered_verified' AND NEW.status = 'settled')) THEN
            RAISE EXCEPTION 'invalid merchant order transition % -> %', OLD.status, NEW.status USING ERRCODE = '23514';
        END IF;
        IF OLD.status = 'awaiting_payment' AND NEW.status = 'paid' AND NOT trade.has_payment_transition_guard(OLD.buyer_order_id) THEN
            RAISE EXCEPTION 'paid transition requires parent payment batch' USING ERRCODE = '55000';
        END IF;
        IF OLD.status = 'paid' AND NEW.status = 'submitted' AND EXISTS (
            SELECT 1 FROM trade.buyer_order WHERE id=OLD.buyer_order_id AND (refund_required_at IS NOT NULL OR reservation_committed_at IS NULL)
        ) THEN
            RAISE EXCEPTION 'order requires a committed reservation and no refund before submission' USING ERRCODE = 'TR004';
        END IF;
        IF OLD.status = 'awaiting_payment' AND NEW.status = 'cancelled' AND NOT EXISTS (
            SELECT 1 FROM trade.buyer_order buyer JOIN trade.reservation_release_outbox outbox
              ON outbox.reservation_key=buyer.buyer_account_id || ':' || buyer.idempotency_key AND outbox.action='release'
            WHERE buyer.id=OLD.buyer_order_id AND buyer.reservation_released_at IS NOT NULL
        ) THEN
            RAISE EXCEPTION 'cancelled order requires durable reservation release' USING ERRCODE = 'TR007';
        END IF;
        IF NEW.version <> OLD.version + 1 OR NOT EXISTS (
            SELECT 1 FROM trade.order_event e WHERE e.merchant_order_id=OLD.id AND e.buyer_order_id=OLD.buyer_order_id
              AND e.from_status=OLD.status AND e.to_status=NEW.status AND e.created_txid=txid_current()
        ) THEN
            RAISE EXCEPTION 'merchant order transition requires matching event' USING ERRCODE = '55000';
        END IF;
    ELSIF NEW.version <> OLD.version THEN
        RAISE EXCEPTION 'merchant order version may change only with status' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.guard_kess_payment_intent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'PENDING' OR NEW.create_attempts <> 0 OR NEW.channel_status IS NOT NULL
           OR NEW.channel_transaction_id IS NOT NULL OR NEW.payment_url IS NOT NULL OR NEW.create_claimed_at IS NOT NULL
           OR NEW.create_sent_at IS NOT NULL OR NEW.last_checked_at IS NOT NULL THEN
            RAISE EXCEPTION 'invalid initial kess payment intent state' USING ERRCODE = 'P0001';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.public_id <> OLD.public_id OR NEW.company_id <> OLD.company_id OR NEW.channel <> OLD.channel
       OR NEW.trade_no <> OLD.trade_no OR NEW.source_ref <> OLD.source_ref OR NEW.attempt_no <> OLD.attempt_no
       OR NEW.amount_e4 <> OLD.amount_e4 OR NEW.currency <> OLD.currency OR NEW.seller_code <> OLD.seller_code
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'kess payment intent receivable facts are immutable' USING ERRCODE = 'P0001';
    END IF;
    IF NOT ((OLD.status='PENDING' AND NEW.status IN ('PENDING','WAITING','UNKNOWN'))
        OR (OLD.status='WAITING' AND NEW.status IN ('WAITING','SUCCESS','FAILED','CLOSED','UNKNOWN','REVIEW','REFUND'))
        OR (OLD.status='UNKNOWN' AND NEW.status IN ('WAITING','SUCCESS','FAILED','CLOSED','UNKNOWN','REFUND'))
        OR (OLD.status='REVIEW' AND NEW.status IN ('REVIEW','SUCCESS','REFUND'))
        OR (OLD.status='SUCCESS' AND NEW.status IN ('SUCCESS','REFUND'))
        OR (OLD.status IN ('FAILED','CLOSED','REFUND') AND NEW.status=OLD.status)) THEN
        RAISE EXCEPTION 'invalid kess payment intent status transition' USING ERRCODE = 'P0001';
    END IF;
    NEW.updated_at := now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.confirm_order_payment(p_public_id text, p_source_ref text, p_paid_amount_e4 bigint)
RETURNS TABLE (reservation_key text, applied boolean, reconciliation_required boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, trade AS $$
DECLARE
    target_id bigint; target_buyer_id bigint; target_idempotency_key text; target_payable_e4 bigint;
    target_released_at timestamptz; target_committed_at timestamptz; target_refund_required_at timestamptz;
    target_reservation_key text; source_owner_id bigint; source_was_known boolean; source_was_applied boolean;
    source_requires_reconciliation boolean; different_source boolean; child_count integer; awaiting_count integer; cancelled_count integer;
BEGIN
    IF p_source_ref IS NULL OR length(btrim(p_source_ref))=0 THEN RAISE EXCEPTION 'payment source is required' USING ERRCODE='23514'; END IF;
    SELECT id,buyer_account_id,idempotency_key,payable_amount_e4,reservation_released_at,reservation_committed_at,refund_required_at
      INTO target_id,target_buyer_id,target_idempotency_key,target_payable_e4,target_released_at,target_committed_at,target_refund_required_at
      FROM trade.buyer_order WHERE public_id=p_public_id FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'buyer order not found' USING ERRCODE='23514'; END IF;
    SELECT EXISTS(SELECT 1 FROM trade.order_event_source WHERE buyer_order_id=target_id AND source_type='kess_result' AND source_ref<>p_source_ref) INTO different_source;
    IF different_source AND (p_paid_amount_e4 IS DISTINCT FROM target_payable_e4 OR target_released_at IS NOT NULL OR target_refund_required_at IS NOT NULL) THEN
        INSERT INTO trade.payment_reconciliation_task (buyer_order_id,source_ref,paid_amount_e4) VALUES (target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT (source_ref) DO NOTHING;
        reservation_key := target_buyer_id || ':' || target_idempotency_key; applied := false; reconciliation_required := true; RETURN NEXT; RETURN;
    END IF;
    IF target_released_at IS NOT NULL OR target_refund_required_at IS NOT NULL THEN RAISE EXCEPTION 'order reservation already released' USING ERRCODE='TR001'; END IF;
    IF p_paid_amount_e4 IS DISTINCT FROM target_payable_e4 THEN RAISE EXCEPTION 'paid amount does not match order total' USING ERRCODE='TR002'; END IF;
    SELECT buyer_order_id INTO source_owner_id FROM trade.order_event_source WHERE source_type='kess_result' AND source_ref=p_source_ref;
    source_was_known := FOUND;
    IF source_was_known AND source_owner_id IS DISTINCT FROM target_id THEN RAISE EXCEPTION 'payment source belongs to another order' USING ERRCODE='23514'; END IF;
    IF NOT source_was_known THEN
        INSERT INTO trade.order_event_source (buyer_order_id,source_type,source_ref) VALUES (target_id,'kess_result',p_source_ref) ON CONFLICT (source_type,source_ref) DO NOTHING;
        SELECT buyer_order_id INTO source_owner_id FROM trade.order_event_source WHERE source_type='kess_result' AND source_ref=p_source_ref;
        IF source_owner_id IS DISTINCT FROM target_id THEN RAISE EXCEPTION 'payment source belongs to another order' USING ERRCODE='23514'; END IF;
    END IF;
    SELECT EXISTS(SELECT 1 FROM trade.order_event WHERE buyer_order_id=target_id AND source_type='kess_result' AND source_ref=p_source_ref AND from_status='awaiting_payment' AND to_status='paid') INTO source_was_applied;
    SELECT EXISTS(SELECT 1 FROM trade.payment_reconciliation_task WHERE buyer_order_id=target_id AND source_ref=p_source_ref) INTO source_requires_reconciliation;
    SELECT count(*),count(*) FILTER (WHERE status='awaiting_payment'),count(*) FILTER (WHERE status='cancelled') INTO child_count,awaiting_count,cancelled_count FROM trade.merchant_order WHERE buyer_order_id=target_id;
    IF cancelled_count>0 THEN RAISE EXCEPTION 'order reservation already released' USING ERRCODE='TR001'; END IF;
    IF child_count=0 THEN RAISE EXCEPTION 'parent payment requires one consistent child batch' USING ERRCODE='23514'; END IF;
    target_reservation_key := target_buyer_id || ':' || target_idempotency_key; reservation_key := target_reservation_key;
    IF source_was_applied THEN
        IF target_committed_at IS NULL THEN INSERT INTO trade.reservation_release_outbox (reservation_key,action,reason) VALUES (target_reservation_key,'commit','payment_confirmed') ON CONFLICT DO NOTHING; END IF;
        applied:=false; reconciliation_required:=false; RETURN NEXT; RETURN;
    END IF;
    IF source_requires_reconciliation OR awaiting_count<>child_count THEN
        INSERT INTO trade.payment_reconciliation_task (buyer_order_id,source_ref,paid_amount_e4) VALUES (target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT (source_ref) DO NOTHING;
        applied:=false; reconciliation_required:=true; RETURN NEXT; RETURN;
    END IF;
    INSERT INTO trade.reservation_release_outbox (reservation_key,action,reason) VALUES (target_reservation_key,'commit','payment_confirmed') ON CONFLICT DO NOTHING;
    INSERT INTO trade.payment_transition_guard (txid,buyer_order_id) VALUES (txid_current(),target_id);
    INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref)
      SELECT id,target_id,'awaiting_payment','paid','kess_result',p_source_ref FROM trade.merchant_order WHERE buyer_order_id=target_id ORDER BY id;
    UPDATE trade.merchant_order SET status='paid',version=version+1,updated_at=clock_timestamp() WHERE buyer_order_id=target_id;
    DELETE FROM trade.payment_transition_guard WHERE txid=txid_current() AND buyer_order_id=target_id;
    applied:=true; reconciliation_required:=false; RETURN NEXT;
END;
$$;
-- +goose StatementEnd

-- New order facts are immutable, but runtime roles need to insert and read them.
GRANT SELECT, INSERT ON trade.buyer_order, trade.merchant_order TO trade_rw;
GRANT SELECT ON trade.buyer_order, trade.merchant_order TO report_ro;
GRANT SELECT (expires_at) ON trade.kess_payment_intent TO report_ro;

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.assert_buyer_order_total() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target_id bigint;
    target_ids bigint[];
    expected_e4 bigint;
    actual_e4 bigint;
BEGIN
    IF TG_TABLE_NAME = 'buyer_order' THEN
        IF TG_OP = 'INSERT' THEN target_ids := ARRAY[NEW.id];
        ELSIF TG_OP = 'DELETE' THEN target_ids := ARRAY[OLD.id];
        ELSE target_ids := ARRAY[OLD.id, NEW.id];
        END IF;
    ELSE
        IF TG_OP = 'INSERT' THEN target_ids := ARRAY[NEW.buyer_order_id];
        ELSIF TG_OP = 'DELETE' THEN target_ids := ARRAY[OLD.buyer_order_id];
        ELSE target_ids := ARRAY[OLD.buyer_order_id, NEW.buyer_order_id];
        END IF;
    END IF;
    FOR target_id IN SELECT DISTINCT unnest(target_ids) LOOP
        SELECT total_e4 INTO expected_e4 FROM trade.buyer_order WHERE id = target_id;
        IF FOUND THEN
            SELECT COALESCE(sum(total_e4), 0) INTO actual_e4 FROM trade.merchant_order WHERE buyer_order_id = target_id;
            IF expected_e4 <> actual_e4 THEN
                RAISE EXCEPTION 'buyer order total does not equal merchant order total' USING ERRCODE = '23514';
            END IF;
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.guard_buyer_order_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.public_id <> OLD.public_id OR NEW.buyer_account_id <> OLD.buyer_account_id
       OR NEW.idempotency_key <> OLD.idempotency_key OR NEW.request_hash <> OLD.request_hash
       OR NEW.total_e4 <> OLD.total_e4 OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'trade.buyer_order business facts are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.reservation_released_at IS DISTINCT FROM OLD.reservation_released_at THEN
        IF OLD.reservation_released_at IS NOT NULL OR NEW.reservation_released_at IS NULL
           OR EXISTS (SELECT 1 FROM trade.merchant_order WHERE buyer_order_id=OLD.id AND status NOT IN ('awaiting_payment','cancelled')) THEN
            RAISE EXCEPTION 'trade.buyer_order reservation release is irreversible and unpaid-only' USING ERRCODE = '55000';
        END IF;
    END IF;
    IF NEW.reservation_committed_at IS DISTINCT FROM OLD.reservation_committed_at THEN
        IF OLD.reservation_committed_at IS NOT NULL OR NEW.reservation_committed_at IS NULL
           OR NEW.reservation_released_at IS NOT NULL OR NEW.refund_required_at IS NOT NULL
           OR NOT EXISTS (SELECT 1 FROM trade.merchant_order WHERE buyer_order_id=OLD.id)
           OR EXISTS (SELECT 1 FROM trade.merchant_order WHERE buyer_order_id=OLD.id AND status <> 'paid') THEN
            RAISE EXCEPTION 'trade.buyer_order reservation commit is irreversible and paid-only' USING ERRCODE = '55000';
        END IF;
    END IF;
    IF NEW.refund_required_at IS DISTINCT FROM OLD.refund_required_at THEN
        IF OLD.refund_required_at IS NOT NULL OR NEW.refund_required_at IS NULL
           OR NOT EXISTS (SELECT 1 FROM trade.merchant_order WHERE buyer_order_id=OLD.id)
           OR EXISTS (SELECT 1 FROM trade.merchant_order WHERE buyer_order_id=OLD.id AND status <> 'paid') THEN
            RAISE EXCEPTION 'trade.buyer_order refund requirement is irreversible and paid-only' USING ERRCODE = '55000';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.guard_merchant_order_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.public_id <> OLD.public_id OR NEW.buyer_order_id <> OLD.buyer_order_id
       OR NEW.merchant_id <> OLD.merchant_id OR NEW.external_order_id <> OLD.external_order_id
       OR NEW.total_e4 <> OLD.total_e4 OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'trade.merchant_order business facts are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.status <> OLD.status THEN
        IF NOT ((OLD.status = 'awaiting_payment' AND NEW.status IN ('paid','cancelled'))
            OR (OLD.status = 'paid' AND NEW.status = 'submitted')
            OR (OLD.status = 'submitted' AND NEW.status IN ('accepted','rejected'))
            OR (OLD.status = 'accepted' AND NEW.status = 'in_fulfillment')
            OR (OLD.status = 'in_fulfillment' AND NEW.status = 'shipped')
            OR (OLD.status = 'shipped' AND NEW.status = 'delivered_verified')
            OR (OLD.status = 'delivered_verified' AND NEW.status = 'settled')) THEN
            RAISE EXCEPTION 'invalid merchant order transition % -> %', OLD.status, NEW.status USING ERRCODE = '23514';
        END IF;
        IF OLD.status = 'awaiting_payment' AND NEW.status = 'paid' AND NOT trade.has_payment_transition_guard(OLD.buyer_order_id) THEN
            RAISE EXCEPTION 'paid transition requires parent payment batch' USING ERRCODE = '55000';
        END IF;
        IF OLD.status = 'paid' AND NEW.status = 'submitted' AND EXISTS (
            SELECT 1 FROM trade.buyer_order WHERE id=OLD.buyer_order_id AND (refund_required_at IS NOT NULL OR reservation_committed_at IS NULL)
        ) THEN
            RAISE EXCEPTION 'order requires a committed reservation and no refund before submission' USING ERRCODE = 'TR004';
        END IF;
        IF OLD.status = 'awaiting_payment' AND NEW.status = 'cancelled' AND NOT EXISTS (
            SELECT 1 FROM trade.buyer_order buyer JOIN trade.reservation_release_outbox outbox
              ON outbox.reservation_key=buyer.buyer_account_id || ':' || buyer.idempotency_key AND outbox.action='release'
            WHERE buyer.id=OLD.buyer_order_id AND buyer.reservation_released_at IS NOT NULL
        ) THEN
            RAISE EXCEPTION 'cancelled order requires durable reservation release' USING ERRCODE = 'TR007';
        END IF;
        IF NEW.version <> OLD.version + 1 OR NOT EXISTS (
            SELECT 1 FROM trade.order_event e WHERE e.merchant_order_id=OLD.id AND e.buyer_order_id=OLD.buyer_order_id
              AND e.from_status=OLD.status AND e.to_status=NEW.status AND e.created_txid=txid_current()
        ) THEN
            RAISE EXCEPTION 'merchant order transition requires matching event' USING ERRCODE = '55000';
        END IF;
    ELSIF NEW.version <> OLD.version THEN
        RAISE EXCEPTION 'merchant order version may change only with status' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.guard_kess_payment_intent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status <> 'PENDING' OR NEW.create_attempts <> 0 OR NEW.channel_status IS NOT NULL
           OR NEW.channel_transaction_id IS NOT NULL OR NEW.payment_url IS NOT NULL OR NEW.create_claimed_at IS NOT NULL
           OR NEW.create_sent_at IS NOT NULL OR NEW.last_checked_at IS NOT NULL THEN
            RAISE EXCEPTION 'invalid initial kess payment intent state' USING ERRCODE = 'P0001';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.public_id <> OLD.public_id OR NEW.company_id <> OLD.company_id OR NEW.channel <> OLD.channel
       OR NEW.trade_no <> OLD.trade_no OR NEW.source_ref <> OLD.source_ref OR NEW.attempt_no <> OLD.attempt_no
       OR NEW.amount_e4 <> OLD.amount_e4 OR NEW.currency <> OLD.currency OR NEW.seller_code <> OLD.seller_code OR NEW.created_at <> OLD.created_at THEN
        RAISE EXCEPTION 'kess payment intent receivable facts are immutable' USING ERRCODE = 'P0001';
    END IF;
    IF NOT ((OLD.status='PENDING' AND NEW.status IN ('PENDING','WAITING','UNKNOWN'))
        OR (OLD.status='WAITING' AND NEW.status IN ('WAITING','SUCCESS','FAILED','CLOSED','UNKNOWN','REVIEW','REFUND'))
        OR (OLD.status='UNKNOWN' AND NEW.status IN ('WAITING','SUCCESS','FAILED','CLOSED','UNKNOWN','REFUND'))
        OR (OLD.status='REVIEW' AND NEW.status IN ('REVIEW','SUCCESS','REFUND'))
        OR (OLD.status='SUCCESS' AND NEW.status IN ('SUCCESS','REFUND'))
        OR (OLD.status IN ('FAILED','CLOSED','REFUND') AND NEW.status=OLD.status)) THEN
        RAISE EXCEPTION 'invalid kess payment intent status transition' USING ERRCODE = 'P0001';
    END IF;
    NEW.updated_at := now(); RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.confirm_order_payment(p_public_id text,p_source_ref text,p_paid_amount_e4 bigint)
RETURNS TABLE(reservation_key text,applied boolean,reconciliation_required boolean)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,trade AS $$
DECLARE target_id bigint;target_buyer_id bigint;target_idempotency_key text;target_total_e4 bigint;target_released_at timestamptz;target_committed_at timestamptz;target_refund_required_at timestamptz;target_reservation_key text;source_owner_id bigint;source_was_known boolean;source_was_applied boolean;source_requires_reconciliation boolean;child_count integer;awaiting_count integer;cancelled_count integer;
BEGIN
    IF p_source_ref IS NULL OR length(btrim(p_source_ref))=0 THEN RAISE EXCEPTION 'payment source is required' USING ERRCODE='23514'; END IF;
    SELECT id,buyer_account_id,idempotency_key,total_e4,reservation_released_at,reservation_committed_at,refund_required_at INTO target_id,target_buyer_id,target_idempotency_key,target_total_e4,target_released_at,target_committed_at,target_refund_required_at FROM trade.buyer_order WHERE public_id=p_public_id FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'buyer order not found' USING ERRCODE='23514'; END IF;
    IF target_released_at IS NOT NULL OR target_refund_required_at IS NOT NULL THEN RAISE EXCEPTION 'order reservation already released' USING ERRCODE='TR001'; END IF;
    IF p_paid_amount_e4 IS DISTINCT FROM target_total_e4 THEN RAISE EXCEPTION 'paid amount does not match order total' USING ERRCODE='TR002'; END IF;
    SELECT buyer_order_id INTO source_owner_id FROM trade.order_event_source WHERE source_type='kess_result' AND source_ref=p_source_ref; source_was_known:=FOUND;
    IF source_was_known AND source_owner_id IS DISTINCT FROM target_id THEN RAISE EXCEPTION 'payment source belongs to another order' USING ERRCODE='23514'; END IF;
    IF NOT source_was_known THEN INSERT INTO trade.order_event_source(buyer_order_id,source_type,source_ref) VALUES(target_id,'kess_result',p_source_ref) ON CONFLICT(source_type,source_ref) DO NOTHING; SELECT buyer_order_id INTO source_owner_id FROM trade.order_event_source WHERE source_type='kess_result' AND source_ref=p_source_ref; IF source_owner_id IS DISTINCT FROM target_id THEN RAISE EXCEPTION 'payment source belongs to another order' USING ERRCODE='23514'; END IF; END IF;
    SELECT EXISTS(SELECT 1 FROM trade.order_event WHERE buyer_order_id=target_id AND source_type='kess_result' AND source_ref=p_source_ref AND from_status='awaiting_payment' AND to_status='paid') INTO source_was_applied;
    SELECT EXISTS(SELECT 1 FROM trade.payment_reconciliation_task WHERE buyer_order_id=target_id AND source_ref=p_source_ref) INTO source_requires_reconciliation;
    SELECT count(*),count(*) FILTER(WHERE status='awaiting_payment'),count(*) FILTER(WHERE status='cancelled') INTO child_count,awaiting_count,cancelled_count FROM trade.merchant_order WHERE buyer_order_id=target_id;
    IF cancelled_count>0 THEN RAISE EXCEPTION 'order reservation already released' USING ERRCODE='TR001'; END IF; IF child_count=0 THEN RAISE EXCEPTION 'parent payment requires one consistent child batch' USING ERRCODE='23514'; END IF;
    target_reservation_key:=target_buyer_id||':'||target_idempotency_key;reservation_key:=target_reservation_key;
    IF source_was_applied THEN IF target_committed_at IS NULL THEN INSERT INTO trade.reservation_release_outbox(reservation_key,action,reason) VALUES(target_reservation_key,'commit','payment_confirmed') ON CONFLICT DO NOTHING; END IF;applied:=false;reconciliation_required:=false;RETURN NEXT;RETURN;END IF;
    IF source_requires_reconciliation OR awaiting_count<>child_count THEN INSERT INTO trade.payment_reconciliation_task(buyer_order_id,source_ref,paid_amount_e4) VALUES(target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT(source_ref) DO NOTHING;applied:=false;reconciliation_required:=true;RETURN NEXT;RETURN;END IF;
    INSERT INTO trade.reservation_release_outbox(reservation_key,action,reason) VALUES(target_reservation_key,'commit','payment_confirmed') ON CONFLICT DO NOTHING;
    INSERT INTO trade.payment_transition_guard(txid,buyer_order_id) VALUES(txid_current(),target_id);
    INSERT INTO trade.order_event(merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref) SELECT id,target_id,'awaiting_payment','paid','kess_result',p_source_ref FROM trade.merchant_order WHERE buyer_order_id=target_id ORDER BY id;
    UPDATE trade.merchant_order SET status='paid',version=version+1,updated_at=clock_timestamp() WHERE buyer_order_id=target_id;DELETE FROM trade.payment_transition_guard WHERE txid=txid_current() AND buyer_order_id=target_id;applied:=true;reconciliation_required:=false;RETURN NEXT;
END;
$$;
-- +goose StatementEnd

ALTER TABLE trade.kess_payment_intent DROP COLUMN expires_at;
ALTER TABLE trade.merchant_order DROP CONSTRAINT ck_trade_merchant_order_amounts,
    DROP COLUMN shipping_quote_expires_at,DROP COLUMN payable_amount_e4,DROP COLUMN shipping_amount_e4,DROP COLUMN goods_amount_e4,DROP COLUMN merchant_public_id;
ALTER TABLE trade.buyer_order DROP CONSTRAINT ck_trade_buyer_order_address_snapshot,DROP CONSTRAINT ck_trade_buyer_order_amounts,
    DROP COLUMN shipping_phone,DROP COLUMN shipping_address_text,DROP COLUMN shipping_recipient_name,DROP COLUMN shipping_address_public_id,
    DROP COLUMN shipping_quote_expires_at,DROP COLUMN payable_amount_e4,DROP COLUMN shipping_amount_e4,DROP COLUMN goods_amount_e4;
