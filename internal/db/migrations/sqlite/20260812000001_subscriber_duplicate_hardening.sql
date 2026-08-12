-- +goose Up
-- Subscriber duplicate hardening. See mysql/20260812000001_subscriber_duplicate_hardening.sql
-- for the full root-cause explanation. SQLite supports generated columns (>= 3.31),
-- so we mirror the MySQL generated-column approach so the unique index is a plain
-- (non-partial) index that can serve as an ON CONFLICT target.

-- ---------------------------------------------------------------------------
-- Step 1: soft-delete duplicate subscriber rows (recovery).
-- Only LIVE rows (deleted_at IS NULL) participate: a soft-deleted row must never
-- be chosen as the keeper, otherwise a live row could be removed in its place.
-- Duplication is scoped per OWNING USER.
-- Keep the lowest-id ACTIVE row if any active row exists, else the lowest id.
-- ---------------------------------------------------------------------------
UPDATE billing_subscribers
SET deleted_at = datetime('now')
WHERE deleted_at IS NULL
  AND subscription_id IS NOT NULL
  AND subscription_id <> ''
  AND id NOT IN (
      SELECT id FROM (
          SELECT id,
                 row_number() OVER (
                     PARTITION BY user_id, gateway_type, subscription_id
                     ORDER BY is_active DESC, id ASC
                 ) AS rn
          FROM billing_subscribers
          WHERE subscription_id IS NOT NULL AND subscription_id <> ''
            AND deleted_at IS NULL
      ) WHERE rn = 1
  );

-- ---------------------------------------------------------------------------
-- Step 2: generated column keying non-empty subscription ids, NULL otherwise.
-- A UNIQUE index scoped per OWNING USER allows multiple NULL rows (ATLOS
-- credit-only plan changes before a subscription exists) while forbidding two rows
-- owned by the same user with the same real subscription id. A (gateway,
-- subscription) pair shared across distinct users remains valid.
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

CREATE UNIQUE INDEX IF NOT EXISTS uniq_billing_subscribers_subscription_id
    ON billing_subscribers(user_id, gateway_type, sub_key);

-- +goose Down
DROP INDEX IF EXISTS uniq_billing_subscribers_subscription_id;
ALTER TABLE billing_subscribers DROP COLUMN sub_key;
