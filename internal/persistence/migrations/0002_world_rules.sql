-- Chronicle schema v2: adds world_rules table for persisting
-- WorldRules (populated by worldpack.Bootstrap from pack.Rules).
--
-- This is a delta migration. v1's tables are unaffected. The world_rules
-- table holds one row per field; values are stored as TEXT and parsed
-- to int / int64 / float64 on read. Keys match the snake_case form
-- of the corresponding WorldRules struct field.
--
-- Example rows:
--   ('annual_death_chance', '0.01')
--   ('fertile_min_age', '16')
--   ('migration_fraction', '0.5')

CREATE TABLE world_rules (
  key   TEXT PRIMARY KEY,
  value TEXT
);
