-- +goose Up
-- Allow trade-owned system exceptions (for example, a missing SKU mapping)
-- to leave a paid order through the same event-and-version guard as the
-- normal fulfillment path.
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
            OR (OLD.status = 'paid' AND NEW.status IN ('submitted','exception'))
            OR (OLD.status = 'submitted' AND NEW.status IN ('accepted','rejected'))
            OR (OLD.status = 'accepted' AND NEW.status = 'in_fulfillment')
            OR (OLD.status = 'in_fulfillment' AND NEW.status = 'shipped')
            OR (OLD.status = 'shipped' AND NEW.status = 'delivered_verified')
            OR (OLD.status = 'delivered_verified' AND NEW.status = 'settled')) THEN
            RAISE EXCEPTION 'invalid merchant order transition % -> %', OLD.status, NEW.status USING ERRCODE = '23514';
        END IF;
        IF OLD.status = 'awaiting_payment' AND NEW.status = 'paid'
           AND NOT trade.has_payment_transition_guard(OLD.buyer_order_id) THEN
            RAISE EXCEPTION 'paid transition requires parent payment batch' USING ERRCODE = '55000';
        END IF;
        IF OLD.status = 'paid' AND NEW.status = 'submitted' AND EXISTS (
            SELECT 1 FROM trade.buyer_order WHERE id=OLD.buyer_order_id
              AND (refund_required_at IS NOT NULL OR reservation_committed_at IS NULL)
        ) THEN
            RAISE EXCEPTION 'order requires a committed reservation and no refund before submission' USING ERRCODE = 'TR004';
        END IF;
        IF OLD.status = 'awaiting_payment' AND NEW.status = 'cancelled' AND NOT EXISTS (
            SELECT 1 FROM trade.buyer_order buyer
            JOIN trade.reservation_release_outbox outbox
              ON outbox.reservation_key=buyer.buyer_account_id || ':' || buyer.idempotency_key
             AND outbox.action='release'
            WHERE buyer.id=OLD.buyer_order_id AND buyer.reservation_released_at IS NOT NULL
        ) THEN
            RAISE EXCEPTION 'cancelled order requires durable reservation release' USING ERRCODE = 'TR007';
        END IF;
        IF NEW.version <> OLD.version + 1 OR NOT EXISTS (
            SELECT 1 FROM trade.order_event e
            WHERE e.merchant_order_id = OLD.id AND e.buyer_order_id = OLD.buyer_order_id
              AND e.from_status = OLD.status AND e.to_status = NEW.status AND e.created_txid=txid_current()
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

-- +goose Down
-- The previous function body is intentionally restored by the next release
-- migration rather than edited in place, preserving forward-only migrations.
