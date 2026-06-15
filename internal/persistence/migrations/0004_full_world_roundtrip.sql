-- Chronicle schema v4: extend persistence to round-trip ALL
-- simulation state so Phase 26's TestSaveLoadRoundTrip passes.
--
-- The v1-v3 persistence layer saved world_meta, people, world_rules,
-- relationships, memories, and (per-person) inventory. It did NOT
-- save locations (beyond id/name/region/population/cap/pressure),
-- factions, events, the world item catalog, or Person.Traits/
-- Needs/Goals. The Phase 25 WorldHash hashes all of these fields,
-- so a save/load round-trip would diverge from the pre-save hash.
--
-- v4 adds the missing columns and a new items table. All ALTER
-- TABLE statements are non-breaking (new columns default to NULL
-- or 0 for existing rows). The items table is new.
--
-- # What changes
--
--   * people.is_merchant            — merchant flag set by bootstrap
--                                     (used by Phase 19 action engine)
--   * people.traits_json, needs_json, goals_json
--                                   — already in the v1 schema, but
--                                     never written by Snapshot; v4
--                                     wires the code to use them
--   * locations.last_shortage_tick  — EconomyEngine transition detection
--   * locations.settlement_json     — the 4-resource SettlementInventory
--                                     (food/wood/iron/cloth floats)
--   * locations.prices_json         — the 4-resource Prices
--                                     (food/wood/iron/cloth ints)
--   * factions.color                — theme hint
--   * factions.base_location        — home location
--   * factions.rivals_json          — rival faction IDs
--   * factions.allies_json          — ally faction IDs
--   * factions.goals_json           — already in v1, but never used
--                                     (the v1 schema planned a JSON
--                                     array; v4 stores the single
--                                     Goal string as a JSON string)
--   * factions.members_json         — already in v1, but never used
--                                     (v4 stores MemberOccupations as
--                                     a JSON array)
--   * events.location               — per-event location scope
--                                     (empty for world-wide events
--                                     like TheftWave)
--   * items                         — the world's item catalog
--                                     (Phase 18+ map[string]Item)

ALTER TABLE people ADD COLUMN is_merchant INTEGER NOT NULL DEFAULT 0;

ALTER TABLE locations ADD COLUMN last_shortage_tick INTEGER NOT NULL DEFAULT 0;
ALTER TABLE locations ADD COLUMN settlement_json TEXT;
ALTER TABLE locations ADD COLUMN prices_json TEXT;

ALTER TABLE factions ADD COLUMN color TEXT;
ALTER TABLE factions ADD COLUMN base_location TEXT;
ALTER TABLE factions ADD COLUMN rivals_json TEXT;
ALTER TABLE factions ADD COLUMN allies_json TEXT;

ALTER TABLE events ADD COLUMN location TEXT;

CREATE TABLE items (
    name           TEXT PRIMARY KEY,
    count          INTEGER NOT NULL DEFAULT 0,
    weight         REAL NOT NULL DEFAULT 0,
    value          INTEGER NOT NULL DEFAULT 0,
    max_durability REAL NOT NULL DEFAULT 0
);
