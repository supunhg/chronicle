package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// Snapshot writes the given world to the database in a single
// transaction. The world metadata is stored as key/value rows in
// world_meta. All people are written to the people table; existing
// people with the same ID are replaced. If w.Rules is non-nil, the
// 8 WorldRules fields are written as key/value rows in world_rules.
// All relationships are written to the relationships table; existing
// (from_id, to_id) pairs are replaced. All memories are written to
// the memories table; existing memory IDs are replaced.
//
// Phase 2 scope: world_meta and people. The people table now stores
// gender, birth_tick, death_tick, location_id, class, occupation,
// father_id, mother_id, spouse_id, plus JSON-encoded traits/needs/goals.
// Phase 7: also writes w.Rules to the world_rules table. The
// locations, factions, events, and inventory tables are not written
// by Snapshot in this phase.
// Phase 13: also writes w.Relationships to the relationships table
// and w.Memories to the memories table. Relationships are keyed by
// (from_id, to_id); memories are keyed by id. Full-replace
// semantics apply to both — the previous snapshot's rows are
// cleared before re-insertion.
//
// Snapshot is the inverse of Restore: Snapshot(w); Restore(w2) yields
// a w2 that is observationally equal to w.
func (db *DB) Snapshot(w *core.World) error {
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("persistence: begin snapshot tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Full-replace semantics: clear existing rows before re-inserting.
	if _, err := tx.ExecContext(ctx, "DELETE FROM world_meta"); err != nil {
		return fmt.Errorf("persistence: delete world_meta: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM people"); err != nil {
		return fmt.Errorf("persistence: delete people: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM world_rules"); err != nil {
		return fmt.Errorf("persistence: delete world_rules: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM relationships"); err != nil {
		return fmt.Errorf("persistence: delete relationships: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM memories"); err != nil {
		return fmt.Errorf("persistence: delete memories: %w", err)
	}
	// Clear the player's inventory rows (only the player's, in case
	// a future phase adds per-NPC inventories with a different
	// person_id). A world with no PlayerID skips this.
	if w.PlayerID != "" {
		if _, err := tx.ExecContext(ctx,
			"DELETE FROM inventory WHERE person_id = ?", w.PlayerID); err != nil {
			return fmt.Errorf("persistence: delete inventory: %w", err)
		}
	}
	if err := writeMeta(ctx, tx, w); err != nil {
		return err
	}
	for _, p := range w.People {
		if err := writePerson(ctx, tx, p); err != nil {
			return err
		}
	}
	if err := writeRules(ctx, tx, w); err != nil {
		return err
	}
	for _, r := range w.Relationships {
		if err := writeRelationship(ctx, tx, &r); err != nil {
			return err
		}
	}
	for i := range w.Memories {
		if err := writeMemory(ctx, tx, &w.Memories[i]); err != nil {
			return err
		}
	}
	if err := writeInventory(ctx, tx, w); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("persistence: commit snapshot: %w", err)
	}
	return nil
}

// Restore reads the world's metadata, rules, people, relationships,
// and memories from the database into the given world. The world's
// People map is replaced and the fields populated from world_meta
// (ID, Seed, Tick, Now) overwrite any existing values. w.Rules is
// set from the world_rules table, or left nil if the table is empty.
// w.Relationships and w.Memories are replaced with the database
// contents (possibly empty slices). Other tables are not read in
// Phase 13.
//
// If the database has no world_meta rows, Restore leaves the world's
// existing metadata fields untouched but replaces the People map,
// the Rules pointer, the Relationships slice, and the Memories slice
// with the (possibly empty) database contents.
func (db *DB) Restore(w *core.World) error {
	meta, err := readMeta(db)
	if err != nil {
		return err
	}
	applyMeta(w, meta)

	rules, err := readRules(db)
	if err != nil {
		return err
	}
	w.Rules = rules

	people, err := readPeople(db)
	if err != nil {
		return err
	}
	w.People = people

	rels, err := readRelationships(db)
	if err != nil {
		return err
	}
	w.Relationships = rels

	mems, err := readMemories(db)
	if err != nil {
		return err
	}
	w.Memories = mems

	// Inventory is scoped to the player (if any). A world with no
	// PlayerID leaves w.Inventory as-is (it stays nil or whatever
	// the caller set). Phase 18: reads the full core.Item
	// metadata (Weight, Value, MaxDurability) from the inventory
	// table.
	inv, err := readInventory(db, w.PlayerID)
	if err != nil {
		return err
	}
	if w.PlayerID != "" {
		w.Inventory = inv
	}
	return nil
}

// writeMeta writes the world's metadata as key/value rows. Phase
// 17.6+ adds the `coin` key for the player's money.
func writeMeta(ctx context.Context, tx *sql.Tx, w *core.World) error {
	rows := map[string]string{
		"id":   w.ID,
		"seed": strconv.FormatInt(w.Seed, 10),
		"tick": strconv.FormatInt(w.Tick, 10),
		"now":  w.Now.UTC().Format(time.RFC3339Nano),
		"coin": strconv.Itoa(w.Coin),
	}
	for k, v := range rows {
		if _, err := tx.ExecContext(ctx,
			"INSERT OR REPLACE INTO world_meta (key, value) VALUES (?, ?)",
			k, v); err != nil {
			return fmt.Errorf("persistence: write world_meta[%s]: %w", k, err)
		}
	}
	return nil
}

// writePerson inserts a single person row with all Phase 2 fields.
func writePerson(ctx context.Context, tx *sql.Tx, p *core.Person) error {
	alive := 0
	if p.Alive {
		alive = 1
	}
	deathTick := sql.NullInt64{}
	if p.DeathTick > 0 {
		deathTick = sql.NullInt64{Int64: p.DeathTick, Valid: true}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO people (
			id, name, gender, birth_tick, death_tick, alive,
			location_id, class, occupation,
			father_id, mother_id, spouse_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, nullStr(p.Gender), p.BirthTick, deathTick, alive,
		nullStr(p.LocationID), nullStr(p.Class), nullStr(p.Occupation),
		nullStr(p.FatherID), nullStr(p.MotherID), nullStr(p.SpouseID),
	); err != nil {
		return fmt.Errorf("persistence: write person %s: %w", p.ID, err)
	}
	return nil
}

// nullStr returns sql.NullString for an empty string (so we store NULL).
func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// readMeta reads all world_meta key/value rows.
func readMeta(db *DB) (map[string]string, error) {
	rows, err := db.Query("SELECT key, value FROM world_meta")
	if err != nil {
		return nil, fmt.Errorf("persistence: query world_meta: %w", err)
	}
	defer rows.Close()
	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("persistence: scan world_meta: %w", err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate world_meta: %w", err)
	}
	return out, nil
}

// applyMeta populates the world's metadata fields from world_meta.
func applyMeta(w *core.World, meta map[string]string) {
	if v, ok := meta["id"]; ok {
		w.ID = v
	}
	if v, ok := meta["seed"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			w.Seed = n
		}
	}
	if v, ok := meta["tick"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			w.Tick = n
		}
	}
	if v, ok := meta["now"]; ok {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			w.Now = t
		}
	}
	// Phase 17.6+: coin is stored as TEXT in world_meta. A missing
	// or unparseable value leaves w.Coin at its current value (0 by
	// default, or whatever the caller set).
	if v, ok := meta["coin"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			w.Coin = n
		}
	}
}

// writeRules writes w.Rules as key/value rows in world_rules. A no-op
// if w.Rules is nil (so a Snapshot of a rules-less world leaves the
// world_rules table empty, and Restore gets nil back).
//
// Values are stored as TEXT and parsed back to int / int64 / float64
// on read. Keys are the snake_case names of the WorldRules fields.
func writeRules(ctx context.Context, tx *sql.Tx, w *core.World) error {
	if w.Rules == nil {
		return nil
	}
	rules := w.Rules
	rows := map[string]string{
		"adult_age":                strconv.Itoa(rules.AdultAge),
		"fertile_min_age":          strconv.Itoa(rules.FertileMinAge),
		"fertile_max_age":          strconv.Itoa(rules.FertileMaxAge),
		"annual_death_chance":      strconv.FormatFloat(rules.AnnualDeathChance, 'f', -1, 64),
		"min_birth_interval_ticks": strconv.FormatInt(rules.MinBirthIntervalTicks, 10),
		"max_children":             strconv.Itoa(rules.MaxChildren),
		"migration_fraction":       strconv.FormatFloat(rules.MigrationFraction, 'f', -1, 64),
		"min_migrants_per_tick":    strconv.Itoa(rules.MinMigrantsPerTick),
	}
	for k, v := range rows {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO world_rules (key, value) VALUES (?, ?)",
			k, v); err != nil {
			return fmt.Errorf("persistence: write world_rules[%s]: %w", k, err)
		}
	}
	return nil
}

// readRules reads all world_rules rows and returns a *core.WorldRules.
// Returns (nil, nil) if the table is empty (no rules were snapshotted).
// A parse error for an individual key is silently ignored so a stale
// schema with extra/unknown keys doesn't fail the whole Restore.
func readRules(db *DB) (*core.WorldRules, error) {
	rows, err := db.Query("SELECT key, value FROM world_rules")
	if err != nil {
		return nil, fmt.Errorf("persistence: query world_rules: %w", err)
	}
	defer rows.Close()
	rules := &core.WorldRules{}
	count := 0
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("persistence: scan world_rules: %w", err)
		}
		count++
		switch k {
		case "adult_age":
			if n, err := strconv.Atoi(v); err == nil {
				rules.AdultAge = n
			}
		case "fertile_min_age":
			if n, err := strconv.Atoi(v); err == nil {
				rules.FertileMinAge = n
			}
		case "fertile_max_age":
			if n, err := strconv.Atoi(v); err == nil {
				rules.FertileMaxAge = n
			}
		case "annual_death_chance":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				rules.AnnualDeathChance = f
			}
		case "min_birth_interval_ticks":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				rules.MinBirthIntervalTicks = n
			}
		case "max_children":
			if n, err := strconv.Atoi(v); err == nil {
				rules.MaxChildren = n
			}
		case "migration_fraction":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				rules.MigrationFraction = f
			}
		case "min_migrants_per_tick":
			if n, err := strconv.Atoi(v); err == nil {
				rules.MinMigrantsPerTick = n
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate world_rules: %w", err)
	}
	if count == 0 {
		return nil, nil
	}
	return rules, nil
}

// readPeople reads all rows from the people table into Person values.
func readPeople(db *DB) (map[string]*core.Person, error) {
	rows, err := db.Query(`SELECT
		id, name, gender, birth_tick, death_tick, alive,
		location_id, class, occupation,
		father_id, mother_id, spouse_id
	FROM people`)
	if err != nil {
		return nil, fmt.Errorf("persistence: query people: %w", err)
	}
	defer rows.Close()
	out := make(map[string]*core.Person)
	for rows.Next() {
		var p core.Person
		var alive int
		var gender, locID, class, occ, father, mother, spouse sql.NullString
		var birthTick int64
		var deathTick sql.NullInt64
		if err := rows.Scan(
			&p.ID, &p.Name, &gender, &birthTick, &deathTick, &alive,
			&locID, &class, &occ, &father, &mother, &spouse,
		); err != nil {
			return nil, fmt.Errorf("persistence: scan people: %w", err)
		}
		p.Alive = alive != 0
		p.BirthTick = birthTick
		if deathTick.Valid {
			p.DeathTick = deathTick.Int64
		}
		p.Gender = gender.String
		p.LocationID = locID.String
		p.Class = class.String
		p.Occupation = occ.String
		p.FatherID = father.String
		p.MotherID = mother.String
		p.SpouseID = spouse.String
		out[p.ID] = &p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate people: %w", err)
	}
	return out, nil
}

// writeRelationship inserts a single relationship row. The (from_id,
// to_id) pair is the primary key, so duplicate writes replace the
// existing row. history_json is written as a JSON array of strings
// (currently always empty, since the RelationshipEngine is a stub;
// Phase 14+ will populate it with memory IDs that contributed to
// this relationship score).
func writeRelationship(ctx context.Context, tx *sql.Tx, r *core.Relationship) error {
	historyJSON := "[]"
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO relationships (
			from_id, to_id,
			trust, respect, fear, attraction, loyalty,
			history_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.FromID, r.ToID,
		r.Trust, r.Respect, r.Fear, r.Attraction, r.Loyalty,
		historyJSON,
	); err != nil {
		return fmt.Errorf("persistence: write relationship %s->%s: %w", r.FromID, r.ToID, err)
	}
	return nil
}

// readRelationships reads all rows from the relationships table.
// Returns a non-nil empty slice when the table is empty. history_json
// is read but not decoded in Phase 13 (the RelationshipEngine is a
// stub); future phases can unmarshal it into a []string.
func readRelationships(db *DB) ([]core.Relationship, error) {
	rows, err := db.Query(`SELECT
		from_id, to_id,
		trust, respect, fear, attraction, loyalty,
		history_json
	FROM relationships`)
	if err != nil {
		return nil, fmt.Errorf("persistence: query relationships: %w", err)
	}
	defer rows.Close()
	out := []core.Relationship{}
	for rows.Next() {
		var r core.Relationship
		var historyJSON sql.NullString
		if err := rows.Scan(
			&r.FromID, &r.ToID,
			&r.Trust, &r.Respect, &r.Fear, &r.Attraction, &r.Loyalty,
			&historyJSON,
		); err != nil {
			return nil, fmt.Errorf("persistence: scan relationships: %w", err)
		}
		// history_json is intentionally ignored for now; included in
		// the SELECT to make future phases a one-line change.
		_ = historyJSON
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate relationships: %w", err)
	}
	return out, nil
}

// writeMemory inserts a single memory row. Nullable string fields
// (event_id, cause_event_id, description) are stored as NULL when
// empty. tags_json is written as a JSON-encoded array of strings
// (or "[]" when Tags is nil/empty).
func writeMemory(ctx context.Context, tx *sql.Tx, m *core.Memory) error {
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	tagsJSON, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("persistence: marshal memory %s tags: %w", m.ID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memories (
			id, owner_id, event_id, cause_event_id, tick,
			importance, recency, emotional,
			trust_delta, relationship_delta,
			description, tags_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.OwnerID,
		nullStr(m.EventID), nullStr(m.CauseEventID),
		m.Tick,
		m.Importance, m.Recency, m.EmotionalScore,
		m.TrustDelta, m.RelationshipDelta,
		nullStr(m.Description), string(tagsJSON),
	); err != nil {
		return fmt.Errorf("persistence: write memory %s: %w", m.ID, err)
	}
	return nil
}

// readMemories reads all rows from the memories table. tags_json is
// decoded into a []string; if the field is NULL or empty, Tags is
// left as a non-nil empty slice. Returns a non-nil empty slice when
// the table is empty.
func readMemories(db *DB) ([]core.Memory, error) {
	rows, err := db.Query(`SELECT
		id, owner_id, event_id, cause_event_id, tick,
		importance, recency, emotional,
		trust_delta, relationship_delta,
		description, tags_json
	FROM memories`)
	if err != nil {
		return nil, fmt.Errorf("persistence: query memories: %w", err)
	}
	defer rows.Close()
	out := []core.Memory{}
	for rows.Next() {
		var m core.Memory
		var eventID, causeID, desc, tagsJSON sql.NullString
		if err := rows.Scan(
			&m.ID, &m.OwnerID, &eventID, &causeID, &m.Tick,
			&m.Importance, &m.Recency, &m.EmotionalScore,
			&m.TrustDelta, &m.RelationshipDelta,
			&desc, &tagsJSON,
		); err != nil {
			return nil, fmt.Errorf("persistence: scan memories: %w", err)
		}
		m.EventID = eventID.String
		m.CauseEventID = causeID.String
		m.Description = desc.String
		m.Tags = decodeTags(tagsJSON)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate memories: %w", err)
	}
	return out, nil
}

// decodeTags parses a JSON-encoded string array. NULL or empty input
// yields a non-nil empty slice. A decode error is logged and also
// yields a non-nil empty slice — the alternative (returning an error
// from readMemories) would force every caller to handle corruption
// that we want to survive so a single bad row doesn't fail the
// whole Restore.
func decodeTags(s sql.NullString) []string {
	if !s.Valid || s.String == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(s.String), &out); err != nil {
		fmt.Fprintf(os.Stderr, "persistence: warning: invalid tags_json %q: %v\n", s.String, err)
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}

// writeInventory writes the player's inventory rows to the inventory
// table, scoped by PlayerID. A no-op if PlayerID is empty (world-
// level mode) — there is no per-person inventory yet for that case.
//
// Phase 18: each row now stores the full core.Item metadata
// (weight, value, max_durability) in addition to the count. The
// v3 migration added the new columns. Legacy v1/v2 rows default
// to 0 for the new columns.
func writeInventory(ctx context.Context, tx *sql.Tx, w *core.World) error {
	if w.PlayerID == "" {
		return nil
	}
	if w.Inventory == nil {
		return nil
	}
	for resource, item := range w.Inventory {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO inventory (person_id, resource, amount, weight, value, max_durability) VALUES (?, ?, ?, ?, ?, ?)",
			w.PlayerID, resource, float64(item.Count), item.Weight, item.Value, item.MaxDurability); err != nil {
			return fmt.Errorf("persistence: write inventory %s/%s: %w", w.PlayerID, resource, err)
		}
	}
	return nil
}

// readInventory reads the player's inventory rows from the inventory
// table, scoped by personID. Returns a non-nil empty map when no
// rows exist. An empty personID yields an empty map (nothing to
// load for world-level mode).
//
// Phase 18: each row returns a full core.Item (Count, Weight,
// Value, MaxDurability). Legacy v1/v2 rows have 0 for the new
// columns; the action engine can refresh the metadata from
// the worldpack catalog on the next buy/sell.
func readInventory(db *DB, personID string) (map[string]core.Item, error) {
	out := make(map[string]core.Item)
	if personID == "" {
		return out, nil
	}
	rows, err := db.Query(
		"SELECT resource, amount, weight, value, max_durability FROM inventory WHERE person_id = ?", personID)
	if err != nil {
		return nil, fmt.Errorf("persistence: query inventory: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item core.Item
		var amount float64
		if err := rows.Scan(&item.Name, &amount, &item.Weight, &item.Value, &item.MaxDurability); err != nil {
			return nil, fmt.Errorf("persistence: scan inventory: %w", err)
		}
		item.Count = int(amount)
		out[item.Name] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate inventory: %w", err)
	}
	return out, nil
}
