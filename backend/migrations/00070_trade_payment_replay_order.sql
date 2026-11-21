-- +goose Up
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.confirm_order_payment(p_public_id text, p_source_ref text, p_paid_amount_e4 bigint)
RETURNS TABLE (reservation_key text, applied boolean, reconciliation_required boolean, error_code text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, trade AS $$
DECLARE
    target_id bigint; target_buyer_id bigint; target_idempotency_key text; target_payable_e4 bigint;
    target_released_at timestamptz; target_committed_at timestamptz; target_refund_required_at timestamptz;
    target_reservation_key text; source_owner_id bigint; source_was_known boolean; source_was_applied boolean;
    source_requires_reconciliation boolean; child_count integer; awaiting_count integer; cancelled_count integer;
BEGIN
    IF p_source_ref IS NULL OR length(btrim(p_source_ref))=0 THEN RAISE EXCEPTION 'payment source is required' USING ERRCODE='23514'; END IF;
    SELECT id,buyer_account_id,idempotency_key,payable_amount_e4,reservation_released_at,reservation_committed_at,refund_required_at
      INTO target_id,target_buyer_id,target_idempotency_key,target_payable_e4,target_released_at,target_committed_at,target_refund_required_at
      FROM trade.buyer_order WHERE public_id=p_public_id FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'buyer order not found' USING ERRCODE='23514'; END IF;
    target_reservation_key := target_buyer_id || ':' || target_idempotency_key; reservation_key := target_reservation_key;

    SELECT buyer_order_id INTO source_owner_id FROM trade.order_event_source WHERE source_type='kess_result' AND source_ref=p_source_ref;
    source_was_known := FOUND;
    IF source_was_known AND source_owner_id IS DISTINCT FROM target_id THEN RAISE EXCEPTION 'payment source belongs to another order' USING ERRCODE='23514'; END IF;
    IF source_was_known THEN
        SELECT EXISTS(SELECT 1 FROM trade.order_event WHERE buyer_order_id=target_id AND source_type='kess_result' AND source_ref=p_source_ref AND from_status='awaiting_payment' AND to_status='paid') INTO source_was_applied;
        IF source_was_applied THEN
            IF target_committed_at IS NULL THEN INSERT INTO trade.reservation_release_outbox (reservation_key,action,reason) VALUES (target_reservation_key,'commit','payment_confirmed') ON CONFLICT DO NOTHING; END IF;
            applied:=false; reconciliation_required:=false; error_code:=''; RETURN NEXT; RETURN;
        END IF;
    END IF;

    IF target_released_at IS NOT NULL OR target_refund_required_at IS NOT NULL THEN
        INSERT INTO trade.payment_reconciliation_task (buyer_order_id,source_ref,paid_amount_e4) VALUES (target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT (source_ref) DO NOTHING;
        applied := false; reconciliation_required := true; error_code := 'TR001'; RETURN NEXT; RETURN;
    END IF;
    IF p_paid_amount_e4 IS DISTINCT FROM target_payable_e4 THEN
        INSERT INTO trade.payment_reconciliation_task (buyer_order_id,source_ref,paid_amount_e4) VALUES (target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT (source_ref) DO NOTHING;
        applied := false; reconciliation_required := true; error_code := 'TR002'; RETURN NEXT; RETURN;
    END IF;
    IF NOT source_was_known THEN
        INSERT INTO trade.order_event_source (buyer_order_id,source_type,source_ref) VALUES (target_id,'kess_result',p_source_ref) ON CONFLICT (source_type,source_ref) DO NOTHING;
        SELECT buyer_order_id INTO source_owner_id FROM trade.order_event_source WHERE source_type='kess_result' AND source_ref=p_source_ref;
        IF source_owner_id IS DISTINCT FROM target_id THEN RAISE EXCEPTION 'payment source belongs to another order' USING ERRCODE='23514'; END IF;
    END IF;
    SELECT EXISTS(SELECT 1 FROM trade.payment_reconciliation_task WHERE buyer_order_id=target_id AND source_ref=p_source_ref) INTO source_requires_reconciliation;
    SELECT count(*),count(*) FILTER (WHERE status='awaiting_payment'),count(*) FILTER (WHERE status='cancelled') INTO child_count,awaiting_count,cancelled_count FROM trade.merchant_order WHERE buyer_order_id=target_id;
    IF cancelled_count>0 THEN
        INSERT INTO trade.payment_reconciliation_task (buyer_order_id,source_ref,paid_amount_e4) VALUES (target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT (source_ref) DO NOTHING;
        applied := false; reconciliation_required := true; error_code := 'TR001'; RETURN NEXT; RETURN;
    END IF;
    IF child_count=0 THEN RAISE EXCEPTION 'parent payment requires one consistent child batch' USING ERRCODE='23514'; END IF;
    IF source_requires_reconciliation OR awaiting_count<>child_count THEN
        INSERT INTO trade.payment_reconciliation_task (buyer_order_id,source_ref,paid_amount_e4) VALUES (target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT (source_ref) DO NOTHING;
        applied:=false; reconciliation_required:=true; error_code:=''; RETURN NEXT; RETURN;
    END IF;
    INSERT INTO trade.reservation_release_outbox (reservation_key,action,reason) VALUES (target_reservation_key,'commit','payment_confirmed') ON CONFLICT DO NOTHING;
    INSERT INTO trade.payment_transition_guard (txid,buyer_order_id) VALUES (txid_current(),target_id);
    INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref)
      SELECT id,target_id,'awaiting_payment','paid','kess_result',p_source_ref FROM trade.merchant_order WHERE buyer_order_id=target_id ORDER BY id;
    UPDATE trade.merchant_order SET status='paid',version=version+1,updated_at=clock_timestamp() WHERE buyer_order_id=target_id;
    DELETE FROM trade.payment_transition_guard WHERE txid=txid_current() AND buyer_order_id=target_id;
    applied:=true; reconciliation_required:=false; error_code:=''; RETURN NEXT;
END;
$$;
-- +goose StatementEnd

-- The application role must not read payment reconciliation rows directly.
-- This definer function claims due work while keeping that table private.
-- +goose StatementBegin
CREATE FUNCTION trade.claim_due_kess_payment_intents(p_now timestamptz, p_limit integer)
RETURNS TABLE (
    id bigint, company_id bigint, channel text, trade_no text, source_ref text,
    attempt_no integer, amount_e4 bigint, currency text, seller_code text, status text,
    create_attempts integer, created_at timestamptz, create_sent_at timestamptz,
    expires_at timestamptz, payment_url text, channel_transaction_id text
)
LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog, trade AS $$
    WITH due AS (
        SELECT intent.id
        FROM trade.kess_payment_intent AS intent
        WHERE (
            intent.status = 'PENDING'
            AND (intent.create_claimed_at IS NULL OR intent.create_claimed_at < p_now - interval '5 minutes')
            AND COALESCE(intent.last_checked_at, intent.updated_at) < p_now - interval '1 minute'
        ) OR (
            intent.status IN ('WAITING','UNKNOWN','INVALIDATED')
            AND COALESCE(intent.last_checked_at, intent.updated_at) < p_now - interval '1 minute'
        ) OR (
            intent.status = 'REVIEW'
            AND COALESCE(intent.last_checked_at, intent.updated_at) < p_now - interval '15 minutes'
        ) OR (
            intent.status = 'SUCCESS' AND intent.channel_transaction_id IS NOT NULL
            AND NOT EXISTS (
                SELECT 1 FROM trade.order_event_source source
                WHERE source.source_type = 'kess_result' AND source.source_ref = intent.channel_transaction_id
            )
            AND NOT EXISTS (
                SELECT 1 FROM trade.payment_reconciliation_task task
                WHERE task.source_ref = intent.channel_transaction_id
            )
            AND COALESCE(intent.last_checked_at, intent.updated_at) < p_now - interval '1 minute'
        )
        ORDER BY intent.id
        FOR UPDATE SKIP LOCKED
        LIMIT GREATEST(p_limit, 0)
    )
    UPDATE trade.kess_payment_intent AS intent
    SET last_checked_at = p_now
    FROM due WHERE intent.id = due.id
    RETURNING intent.id, intent.company_id, intent.channel, intent.trade_no, intent.source_ref,
        intent.attempt_no, intent.amount_e4, intent.currency, intent.seller_code, intent.status,
        intent.create_attempts, intent.created_at, intent.create_sent_at, intent.expires_at,
        COALESCE(intent.payment_url,''), COALESCE(intent.channel_transaction_id,'')
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION trade.claim_due_kess_payment_intents(timestamptz, integer) FROM PUBLIC, trade_rw, report_ro;
GRANT EXECUTE ON FUNCTION trade.claim_due_kess_payment_intents(timestamptz, integer) TO trade_rw;

-- +goose Down
DROP FUNCTION trade.claim_due_kess_payment_intents(timestamptz, integer);
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION trade.confirm_order_payment(p_public_id text, p_source_ref text, p_paid_amount_e4 bigint)
RETURNS TABLE (reservation_key text, applied boolean, reconciliation_required boolean, error_code text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, trade AS $$
DECLARE
    target_id bigint; target_buyer_id bigint; target_idempotency_key text; target_payable_e4 bigint;
    target_released_at timestamptz; target_committed_at timestamptz; target_refund_required_at timestamptz;
    target_reservation_key text; source_owner_id bigint; source_was_known boolean; source_was_applied boolean;
    source_requires_reconciliation boolean; child_count integer; awaiting_count integer; cancelled_count integer;
BEGIN
    IF p_source_ref IS NULL OR length(btrim(p_source_ref))=0 THEN RAISE EXCEPTION 'payment source is required' USING ERRCODE='23514'; END IF;
    SELECT id,buyer_account_id,idempotency_key,payable_amount_e4,reservation_released_at,reservation_committed_at,refund_required_at
      INTO target_id,target_buyer_id,target_idempotency_key,target_payable_e4,target_released_at,target_committed_at,target_refund_required_at
      FROM trade.buyer_order WHERE public_id=p_public_id FOR UPDATE;
    IF NOT FOUND THEN RAISE EXCEPTION 'buyer order not found' USING ERRCODE='23514'; END IF;
    target_reservation_key := target_buyer_id || ':' || target_idempotency_key; reservation_key := target_reservation_key;
    IF target_released_at IS NOT NULL OR target_refund_required_at IS NOT NULL THEN
        INSERT INTO trade.payment_reconciliation_task (buyer_order_id,source_ref,paid_amount_e4) VALUES (target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT (source_ref) DO NOTHING;
        applied := false; reconciliation_required := true; error_code := 'TR001'; RETURN NEXT; RETURN;
    END IF;
    IF p_paid_amount_e4 IS DISTINCT FROM target_payable_e4 THEN
        INSERT INTO trade.payment_reconciliation_task (buyer_order_id,source_ref,paid_amount_e4) VALUES (target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT (source_ref) DO NOTHING;
        applied := false; reconciliation_required := true; error_code := 'TR002'; RETURN NEXT; RETURN;
    END IF;
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
    IF cancelled_count>0 THEN
        INSERT INTO trade.payment_reconciliation_task (buyer_order_id,source_ref,paid_amount_e4) VALUES (target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT (source_ref) DO NOTHING;
        applied := false; reconciliation_required := true; error_code := 'TR001'; RETURN NEXT; RETURN;
    END IF;
    IF child_count=0 THEN RAISE EXCEPTION 'parent payment requires one consistent child batch' USING ERRCODE='23514'; END IF;
    IF source_was_applied THEN
        IF target_committed_at IS NULL THEN INSERT INTO trade.reservation_release_outbox (reservation_key,action,reason) VALUES (target_reservation_key,'commit','payment_confirmed') ON CONFLICT DO NOTHING; END IF;
        applied:=false; reconciliation_required:=false; error_code:=''; RETURN NEXT; RETURN;
    END IF;
    IF source_requires_reconciliation OR awaiting_count<>child_count THEN
        INSERT INTO trade.payment_reconciliation_task (buyer_order_id,source_ref,paid_amount_e4) VALUES (target_id,p_source_ref,p_paid_amount_e4) ON CONFLICT (source_ref) DO NOTHING;
        applied:=false; reconciliation_required:=true; error_code:=''; RETURN NEXT; RETURN;
    END IF;
    INSERT INTO trade.reservation_release_outbox (reservation_key,action,reason) VALUES (target_reservation_key,'commit','payment_confirmed') ON CONFLICT DO NOTHING;
    INSERT INTO trade.payment_transition_guard (txid,buyer_order_id) VALUES (txid_current(),target_id);
    INSERT INTO trade.order_event (merchant_order_id,buyer_order_id,from_status,to_status,source_type,source_ref)
      SELECT id,target_id,'awaiting_payment','paid','kess_result',p_source_ref FROM trade.merchant_order WHERE buyer_order_id=target_id ORDER BY id;
    UPDATE trade.merchant_order SET status='paid',version=version+1,updated_at=clock_timestamp() WHERE buyer_order_id=target_id;
    DELETE FROM trade.payment_transition_guard WHERE txid=txid_current() AND buyer_order_id=target_id;
    applied:=true; reconciliation_required:=false; error_code:=''; RETURN NEXT;
END;
$$;
-- +goose StatementEnd
