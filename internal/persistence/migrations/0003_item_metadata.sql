-- Chronicle schema v3: add per-item metadata to the inventory table.
--
-- Phase 18: Inventory was promoted from map[string]int to
-- map[string]core.Item, with each stack carrying Weight (kg per
-- unit), Value (coin per unit), and MaxDurability (1.0 = pristine,
-- 0.0 = perishable). The existing (person_id, resource, amount)
-- is kept; we add three new columns.
--
-- Default values of 0 mean "no metadata" — a legacy DB written
-- by Phase 17.6/17.7 (which only stored the amount) restores
-- with all metadata at 0, and the action engine falls back to
-- the worldpack catalog to fill in any missing metadata on the
-- next buy/sell.
--
-- Backward compat: existing rows are untouched (the new columns
-- default to 0 for them). New Snapshot writes always populate
-- the new columns.

ALTER TABLE inventory ADD COLUMN weight REAL NOT NULL DEFAULT 0;
ALTER TABLE inventory ADD COLUMN value INTEGER NOT NULL DEFAULT 0;
ALTER TABLE inventory ADD COLUMN max_durability REAL NOT NULL DEFAULT 0;
