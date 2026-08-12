-- +goose Up
-- Subscriber duplicate hardening.
--
-- Problem: two webhook events (checkout.session.completed and invoice.paid) can
-- concurrently create a pending (is_active=false) subscriber for the same
-- subscription. The existing partial unique index only protects ACTIVE rows
-- (active_gateway_key), so pending duplicates are allowed. Then activation flips
-- BOTH rows active -> violates active_gateway_key -> UNIQUE constraint failed ->
-- the whole activation aborts, leaving every duplicate row permanently inactive.
--
-- This migration:
--   1. Recovers already-corrupted rows: for each (gateway, subscription) with
--      multiple live rows, keeps one canonical row and soft-deletes the rest. The
--      canonical row is the lowest-id ACTIVE row if any active row exists,
--      otherwise the lowest-id row.
--   2. Adds a UNIQUE index so one Stripe/ATLOS subscription can map to at most one
--      local row. Duplicate pending rows become impossible by construction.

-- ---------------------------------------------------------------------------
-- Step 1: soft-delete duplicate subscriber rows (recovery).
-- Only LIVE rows (deleted_at IS NULL) participate in de-duplication; a row already
-- soft-deleted is dead and must never be chosen as the keeper, otherwise a live
-- row could be removed in its place. Duplication is scoped per OWNING USER: only
-- rows belonging to the same (user_id, gateway, subscription) are duplicates.
-- ---------------------------------------------------------------------------

-- Mark duplicate rows that are NOT the canonical (primary) row for their
-- user's subscription as soft-deleted. Canonical selection (per user + gateway +
-- subscription):
--   keep = the lowest-id ACTIVE row if any active row exists, else the lowest id.
-- Only live rows with a non-empty subscription_id participate.
UPDATE billing_subscribers AS target
JOIN (
    SELECT
        id,
        row_number() OVER (
            PARTITION BY user_id, gateway_type, subscription_id
            -- active rows sort before inactive; among equals, lowest id wins
            ORDER BY is_active DESC, id ASC
        ) AS rn
    FROM billing_subscribers
    WHERE subscription_id IS NOT NULL AND subscription_id <> ''
      AND deleted_at IS NULL
) AS ranked
    ON target.id = ranked.id
SET target.deleted_at = COALESCE(target.deleted_at, NOW())
WHERE ranked.rn > 1;

-- ---------------------------------------------------------------------------
-- Step 2: unique constraint guaranteeing one local row per real subscription id.
-- Scoped per OWNING USER so a subscription id belongs to exactly one row per user;
-- a (gateway, subscription) pair shared across distinct users remains valid.
-- Uses a stored generated column so that NULL / empty subscription_id rows (used by
-- ATLOS credit-only plan changes before a subscription exists) are exempt.
-- ---------------------------------------------------------------------------
ALTER TABLE billing_subscribers
    ADD COLUMN sub_key VARCHAR(255) GENERATED ALWAYS AS (
        CASE
            WHEN subscription_id IS NOT NULL
                 AND subscription_id <> ''
                 AND deleted_at IS NULL
            THEN subscription_id
            ELSE NULL
        END
    ) STORED;

ALTER TABLE billing_subscribers
    ADD UNIQUE KEY uniq_billing_subscribers_subscription_id (user_id, gateway_type, sub_key);

-- +goose Down
ALTER TABLE billing_subscribers DROP INDEX uniq_billing_subscribers_subscription_id;
ALTER TABLE billing_subscribers DROP COLUMN sub_key;
