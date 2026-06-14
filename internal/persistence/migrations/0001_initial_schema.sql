-- Chronicle schema v1
-- Source of truth: chronicle-spec.md §9.5
--
-- Phase 2 changes vs. Phase 1:
--   * Dropped `age` column on `people` (Age is derived from birth_tick).
--   * Added `father_id`, `mother_id`, `spouse_id` for family trees.

CREATE TABLE world_meta (
  key   TEXT PRIMARY KEY,
  value TEXT
);

CREATE TABLE people (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL DEFAULT '',
  gender      TEXT,
  birth_tick  INTEGER,
  death_tick  INTEGER,
  alive       INTEGER NOT NULL DEFAULT 1,
  location_id TEXT,
  class       TEXT,
  occupation  TEXT,
  father_id   TEXT,
  mother_id   TEXT,
  spouse_id   TEXT,
  traits_json TEXT,
  needs_json  TEXT,
  goals_json  TEXT,
  legacy      TEXT
);

CREATE TABLE relationships (
  from_id        TEXT NOT NULL,
  to_id          TEXT NOT NULL,
  trust          REAL NOT NULL DEFAULT 0,
  respect        REAL NOT NULL DEFAULT 0,
  fear           REAL NOT NULL DEFAULT 0,
  attraction     REAL NOT NULL DEFAULT 0,
  loyalty        REAL NOT NULL DEFAULT 0,
  history_json   TEXT,
  PRIMARY KEY (from_id, to_id)
);

CREATE TABLE memories (
  id                 TEXT PRIMARY KEY,
  owner_id           TEXT NOT NULL,
  event_id           TEXT,
  cause_event_id     TEXT,
  tick               INTEGER NOT NULL,
  importance         REAL NOT NULL DEFAULT 0,
  recency            REAL NOT NULL DEFAULT 0,
  emotional          REAL NOT NULL DEFAULT 0,
  trust_delta        REAL NOT NULL DEFAULT 0,
  relationship_delta REAL NOT NULL DEFAULT 0,
  description        TEXT,
  tags_json          TEXT
);

CREATE TABLE locations (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL DEFAULT '',
  region       TEXT,
  population   INTEGER NOT NULL DEFAULT 0,
  population_cap INTEGER NOT NULL DEFAULT 0,
  pressure     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE factions (
  id           TEXT PRIMARY KEY,
  name         TEXT NOT NULL DEFAULT '',
  goals_json   TEXT,
  members_json TEXT
);

CREATE TABLE events (
  id              TEXT PRIMARY KEY,
  parent_event_id TEXT,
  tick            INTEGER NOT NULL,
  kind            TEXT NOT NULL,
  payload_json    TEXT
);

CREATE TABLE inventory (
  person_id TEXT NOT NULL,
  resource  TEXT NOT NULL,
  amount    REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (person_id, resource)
);

CREATE INDEX idx_people_alive     ON people(alive);
CREATE INDEX idx_people_location  ON people(location_id);
CREATE INDEX idx_people_father    ON people(father_id);
CREATE INDEX idx_people_mother    ON people(mother_id);
CREATE INDEX idx_memories_owner   ON memories(owner_id);
CREATE INDEX idx_memories_event   ON memories(event_id);
CREATE INDEX idx_events_tick      ON events(tick);
CREATE INDEX idx_inventory_person ON inventory(person_id);
