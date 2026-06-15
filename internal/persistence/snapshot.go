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
	// Phase 19: clear ALL inventory rows (per-person, scoped by
	// person_id). Pre-Phase 19 the player was the only person with
	// an inventory, so this is a superset of the previous behavior.
	if _, err := tx.ExecContext(ctx, "DELETE FROM inventory"); err != nil {
		return fmt.Errorf("persistence: delete inventory: %w", err)
	}
	// Phase 26: clear the tables that v4 added full round-trip
	// paths for. Locations, factions, events, and items are
	// always full-replaced on Snapshot.
	if _, err := tx.ExecContext(ctx, "DELETE FROM locations"); err != nil {
		return fmt.Errorf("persistence: delete locations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM factions"); err != nil {
		return fmt.Errorf("persistence: delete factions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM events"); err != nil {
		return fmt.Errorf("persistence: delete events: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM items"); err != nil {
		return fmt.Errorf("persistence: delete items: %w", err)
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
	// Phase 26: write the remaining state tables so a save/load
	// round-trip preserves every field core.WorldHash covers.
	if err := writeLocations(ctx, tx, w); err != nil {
		return err
	}
	if err := writeFactions(ctx, tx, w); err != nil {
		return err
	}
	if err := writeEvents(ctx, tx, w); err != nil {
		return err
	}
	if err := writeItemCatalog(ctx, tx, w); err != nil {
		return err
	}
	// Phase 19: write inventory rows for every Person who has a
	// non-nil Inventory. The player's inventory (w.Inventory) is
	// written first; then every NPC with a non-nil Inventory.
	if err := writeAllInventories(ctx, tx, w); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("persistence: commit snapshot: %w", err)
	}
	return nil
}

// Restore reads the world's metadata, rules, people, relationships,
// memories, locations, factions, events, items, and inventories
// from the database into the given world. The world's People map
// is replaced and the fields populated from world_meta (ID, Seed,
// Tick, Now, PlayerID, Coin) overwrite any existing values.
// w.Rules is set from the world_rules table, or left nil if the
// table is empty. w.Relationships and w.Memories are replaced with
// the database contents (possibly empty slices). w.Locations,
// w.Factions, w.Items, and w.Inventory are replaced with the
// database contents (possibly empty maps). w.Events is replaced
// with the database contents (possibly empty slice).
//
// If the database has no world_meta rows, Restore leaves the world's
// existing metadata fields untouched but replaces the People map,
// the Rules pointer, the Relationships slice, the Memories slice,
// the Locations map, the Factions map, the Events slice, the Items
// map, and the player inventory with the (possibly empty)
// database contents.
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

	// Phase 26: read the rest of the world state so a save/load
	// round-trip preserves every field core.WorldHash covers.
	locations, err := readLocations(db)
	if err != nil {
		return err
	}
	w.Locations = locations

	factions, err := readFactions(db)
	if err != nil {
		return err
	}
	w.Factions = factions

	events, err := readEvents(db)
	if err != nil {
		return err
	}
	w.Events = events

	items, err := readItemCatalog(db)
	if err != nil {
		return err
	}
	w.Items = items

	// Phase 19: inventory is per-person, keyed by person_id.
	// The player's inventory (w.PlayerID, if set) goes into
	// w.Inventory; every other Person with a non-nil
	// inventory gets their rows loaded into p.Inventory.
	// Persons with no inventory rows keep a non-nil empty
	// map (so the action engine's resolveBuy/resolveSell
	// can append without nil checks).
	if err := readAllInventories(db, w); err != nil {
		return err
	}
	return nil
}

// writeMeta writes the world's metadata as key/value rows. Phase
// 17.6+ adds the `coin` key for the player's money. Phase 26 adds
// `player_id` so the player-identity round-trips with the rest of
// the world state. The WorldHash includes PlayerID; a missing key
// would cause a save/load round-trip to diverge.
func writeMeta(ctx context.Context, tx *sql.Tx, w *core.World) error {
	rows := map[string]string{
		"id":        w.ID,
		"seed":      strconv.FormatInt(w.Seed, 10),
		"tick":      strconv.FormatInt(w.Tick, 10),
		"now":       w.Now.UTC().Format(time.RFC3339Nano),
		"coin":      strconv.Itoa(w.Coin),
		"player_id": w.PlayerID,
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

// writePerson inserts a single person row with all Phase 2 fields
// plus the Phase 26 is_merchant flag and the JSON-encoded
// traits/needs/goals columns. The traits_json/needs_json/goals_json
// columns were already in the v1 schema but were never written by
// Snapshot; v4 wires them so the WorldHash's per-person field set
// round-trips intact.
func writePerson(ctx context.Context, tx *sql.Tx, p *core.Person) error {
	alive := 0
	if p.Alive {
		alive = 1
	}
	isMerchant := 0
	if p.IsMerchant {
		isMerchant = 1
	}
	deathTick := sql.NullInt64{}
	if p.DeathTick > 0 {
		deathTick = sql.NullInt64{Int64: p.DeathTick, Valid: true}
	}
	traitsJSON, err := json.Marshal(p.Traits)
	if err != nil {
		return fmt.Errorf("persistence: marshal traits for %s: %w", p.ID, err)
	}
	needsJSON, err := json.Marshal(p.Needs)
	if err != nil {
		return fmt.Errorf("persistence: marshal needs for %s: %w", p.ID, err)
	}
	goalsJSON, err := json.Marshal(p.Goals)
	if err != nil {
		return fmt.Errorf("persistence: marshal goals for %s: %w", p.ID, err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO people (
			id, name, gender, birth_tick, death_tick, alive,
			location_id, class, occupation,
			father_id, mother_id, spouse_id,
			is_merchant, traits_json, needs_json, goals_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, nullStr(p.Gender), p.BirthTick, deathTick, alive,
		nullStr(p.LocationID), nullStr(p.Class), nullStr(p.Occupation),
		nullStr(p.FatherID), nullStr(p.MotherID), nullStr(p.SpouseID),
		isMerchant, string(traitsJSON), string(needsJSON), string(goalsJSON),
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
	// Phase 26+: player_id is stored as TEXT in world_meta. An
	// empty string is a valid value (no specific player; the
	// simulation runs in world-level mode).
	if v, ok := meta["player_id"]; ok {
		w.PlayerID = v
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

// readPeople reads all rows from the people table into Person
// values, including the Phase 26 is_merchant flag and the
// traits/needs/goals JSON columns. The JSON columns are decoded
// into the corresponding Go types; a missing or empty column
// yields a non-nil empty map/slice so the action engine can
// index without nil checks.
func readPeople(db *DB) (map[string]*core.Person, error) {
	rows, err := db.Query(`SELECT
		id, name, gender, birth_tick, death_tick, alive,
		location_id, class, occupation,
		father_id, mother_id, spouse_id,
		is_merchant, traits_json, needs_json, goals_json
	FROM people`)
	if err != nil {
		return nil, fmt.Errorf("persistence: query people: %w", err)
	}
	defer rows.Close()
	out := make(map[string]*core.Person)
	for rows.Next() {
		var p core.Person
		var alive, isMerchant int
		var gender, locID, class, occ, father, mother, spouse sql.NullString
		var traitsJSON, needsJSON, goalsJSON sql.NullString
		var birthTick int64
		var deathTick sql.NullInt64
		if err := rows.Scan(
			&p.ID, &p.Name, &gender, &birthTick, &deathTick, &alive,
			&locID, &class, &occ, &father, &mother, &spouse,
			&isMerchant, &traitsJSON, &needsJSON, &goalsJSON,
		); err != nil {
			return nil, fmt.Errorf("persistence: scan people: %w", err)
		}
		p.Alive = alive != 0
		p.IsMerchant = isMerchant != 0
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
		p.Traits = decodeIntMap(traitsJSON)
		p.Needs = decodeIntMap(needsJSON)
		p.Goals = decodeGoals(goalsJSON)
		out[p.ID] = &p
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate people: %w", err)
	}
	return out, nil
}

// decodeIntMap parses a JSON-encoded map[string]int. NULL or empty
// input yields a non-nil empty map. A decode error yields a
// non-nil empty map and logs to stderr so a single corrupt row
// doesn't fail the whole Restore.
func decodeIntMap(s sql.NullString) map[string]int {
	out := make(map[string]int)
	if !s.Valid || s.String == "" {
		return out
	}
	if err := json.Unmarshal([]byte(s.String), &out); err != nil {
		fmt.Fprintf(os.Stderr, "persistence: warning: invalid int-map JSON %q: %v\n", s.String, err)
		return make(map[string]int)
	}
	if out == nil {
		return make(map[string]int)
	}
	return out
}

// decodeGoals parses a JSON-encoded []core.Goal. NULL or empty
// input yields a non-nil empty slice.
func decodeGoals(s sql.NullString) []core.Goal {
	out := []core.Goal{}
	if !s.Valid || s.String == "" {
		return out
	}
	if err := json.Unmarshal([]byte(s.String), &out); err != nil {
		fmt.Fprintf(os.Stderr, "persistence: warning: invalid goals JSON %q: %v\n", s.String, err)
		return []core.Goal{}
	}
	if out == nil {
		return []core.Goal{}
	}
	return out
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

// writeInventory writes a single person's inventory rows to
// the inventory table, scoped by personID. A no-op if inv is
// nil (no inventory to write). Phase 19: called for the
// player (w.Inventory) and for every NPC with a non-nil
// Inventory, so each person's stock round-trips separately.
//
// Phase 18: each row stores the full core.Item metadata
// (weight, value, max_durability) in addition to the count.
// The v3 migration added the new columns. Legacy v1/v2 rows
// default to 0 for the new columns.
func writeInventory(ctx context.Context, tx *sql.Tx, personID string, inv map[string]core.Item) error {
	if personID == "" || inv == nil {
		return nil
	}
	for resource, item := range inv {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO inventory (person_id, resource, amount, weight, value, max_durability) VALUES (?, ?, ?, ?, ?, ?)",
			personID, resource, float64(item.Count), item.Weight, item.Value, item.MaxDurability); err != nil {
			return fmt.Errorf("persistence: write inventory %s/%s: %w", personID, resource, err)
		}
	}
	return nil
}

// readInventory reads a single person's inventory rows from
// the inventory table, scoped by personID. Returns a non-nil
// empty map when no rows exist or personID is empty. Phase
// 19: called once per person (player + every NPC who had
// rows on the prior Snapshot).
//
// Phase 18: each row returns a full core.Item (Count,
// Weight, Value, MaxDurability). Legacy v1/v2 rows have 0
// for the new columns; the action engine can refresh the
// metadata from the worldpack catalog on the next buy/sell.
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

// writeLocations writes every location in w.Locations. Phase 26
// adds full settlement/prices/last_shortage_tick persistence; the
// v1 schema only stored the basic id/name/region/population/cap/
// pressure columns, so v4 also added settlement_json, prices_json,
// and last_shortage_tick.
//
// Full-replace semantics: this is called from Snapshot AFTER the
// existing "DELETE FROM locations" so the table reflects exactly
// the current world state. A world with no locations writes zero
// rows; a fresh Restore on such a world yields an empty
// w.Locations map (consistent with the v1 design).
func writeLocations(ctx context.Context, tx *sql.Tx, w *core.World) error {
	for _, l := range w.Locations {
		if l == nil || l.ID == "" {
			continue
		}
		settlementJSON, err := json.Marshal(l.Settlement)
		if err != nil {
			return fmt.Errorf("persistence: marshal settlement for %s: %w", l.ID, err)
		}
		pricesJSON, err := json.Marshal(l.Prices)
		if err != nil {
			return fmt.Errorf("persistence: marshal prices for %s: %w", l.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO locations (
				id, name, region, population, population_cap, pressure,
				last_shortage_tick, settlement_json, prices_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			l.ID, l.Name, nullStr(l.Region), l.Population, l.PopulationCap, l.Pressure,
			l.LastShortageTick, string(settlementJSON), string(pricesJSON),
		); err != nil {
			return fmt.Errorf("persistence: write location %s: %w", l.ID, err)
		}
	}
	return nil
}

// readLocations reads every row from the locations table. The
// settlement_json and prices_json columns are decoded into the
// corresponding struct types; a missing or empty column yields
// the zero value, which matches a freshly-constructed Location's
// defaults.
func readLocations(db *DB) (map[string]*core.Location, error) {
	rows, err := db.Query(`SELECT
		id, name, region, population, population_cap, pressure,
		last_shortage_tick, settlement_json, prices_json
	FROM locations`)
	if err != nil {
		return nil, fmt.Errorf("persistence: query locations: %w", err)
	}
	defer rows.Close()
	out := make(map[string]*core.Location)
	for rows.Next() {
		var l core.Location
		var region sql.NullString
		var settlementJSON, pricesJSON sql.NullString
		if err := rows.Scan(
			&l.ID, &l.Name, &region, &l.Population, &l.PopulationCap, &l.Pressure,
			&l.LastShortageTick, &settlementJSON, &pricesJSON,
		); err != nil {
			return nil, fmt.Errorf("persistence: scan locations: %w", err)
		}
		l.Region = region.String
		if settlementJSON.Valid && settlementJSON.String != "" {
			var s core.SettlementInventory
			if err := json.Unmarshal([]byte(settlementJSON.String), &s); err == nil {
				l.Settlement = s
			}
		}
		if pricesJSON.Valid && pricesJSON.String != "" {
			var p core.Prices
			if err := json.Unmarshal([]byte(pricesJSON.String), &p); err == nil {
				l.Prices = p
			}
		}
		out[l.ID] = &l
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate locations: %w", err)
	}
	return out, nil
}

// writeFactions writes every faction in w.Factions. Phase 26 adds
// color, base_location, rivals, and allies; the v1 schema had only
// goals_json and members_json columns. v4 added the new columns;
// this function writes all of them.
//
// goals_json stores the Faction.Goal as a JSON string (the v1
// schema planned an array; v4 stays backward-compatible by
// JSON-encoding a single string). members_json stores
// MemberOccupations as a JSON array. rivals_json and allies_json
// mirror members_json.
func writeFactions(ctx context.Context, tx *sql.Tx, w *core.World) error {
	for _, f := range w.Factions {
		if f == nil || f.ID == "" {
			continue
		}
		goalJSON, err := json.Marshal(f.Goal)
		if err != nil {
			return fmt.Errorf("persistence: marshal goal for %s: %w", f.ID, err)
		}
		membersJSON, err := json.Marshal(f.MemberOccupations)
		if err != nil {
			return fmt.Errorf("persistence: marshal members for %s: %w", f.ID, err)
		}
		rivals := f.Rivals
		if rivals == nil {
			rivals = []string{}
		}
		allies := f.Allies
		if allies == nil {
			allies = []string{}
		}
		rivalsJSON, err := json.Marshal(rivals)
		if err != nil {
			return fmt.Errorf("persistence: marshal rivals for %s: %w", f.ID, err)
		}
		alliesJSON, err := json.Marshal(allies)
		if err != nil {
			return fmt.Errorf("persistence: marshal allies for %s: %w", f.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO factions (
				id, name, color, base_location,
				goals_json, members_json, rivals_json, allies_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			f.ID, f.Name, nullStr(f.Color), nullStr(f.BaseLocation),
			string(goalJSON), string(membersJSON), string(rivalsJSON), string(alliesJSON),
		); err != nil {
			return fmt.Errorf("persistence: write faction %s: %w", f.ID, err)
		}
	}
	return nil
}

// readFactions reads every row from the factions table. goals_json
// is decoded into the single Goal string (or left as "" for an
// empty or missing column). members_json is decoded into the
// MemberOccupations slice (or a non-nil empty slice).
func readFactions(db *DB) (map[string]*core.Faction, error) {
	rows, err := db.Query(`SELECT
		id, name, color, base_location,
		goals_json, members_json, rivals_json, allies_json
	FROM factions`)
	if err != nil {
		return nil, fmt.Errorf("persistence: query factions: %w", err)
	}
	defer rows.Close()
	out := make(map[string]*core.Faction)
	for rows.Next() {
		var f core.Faction
		var color, baseLoc sql.NullString
		var goalJSON, membersJSON, rivalsJSON, alliesJSON sql.NullString
		if err := rows.Scan(
			&f.ID, &f.Name, &color, &baseLoc,
			&goalJSON, &membersJSON, &rivalsJSON, &alliesJSON,
		); err != nil {
			return nil, fmt.Errorf("persistence: scan factions: %w", err)
		}
		f.Color = color.String
		f.BaseLocation = baseLoc.String
		if goalJSON.Valid && goalJSON.String != "" {
			// goals_json is a JSON-encoded string; decode it
			// into a string variable first so we strip the
			// quotes.
			var goal string
			if err := json.Unmarshal([]byte(goalJSON.String), &goal); err == nil {
				f.Goal = goal
			}
		}
		f.MemberOccupations = decodeStringSlice(membersJSON)
		f.Rivals = decodeStringSlice(rivalsJSON)
		f.Allies = decodeStringSlice(alliesJSON)
		out[f.ID] = &f
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate factions: %w", err)
	}
	return out, nil
}

// writeEvents writes every event in w.Events. Phase 26 adds the
// location column (v1 schema omitted it; v4 added it). The
// payload_json column is the JSON-encoded event payload (a
// map[string]any).
func writeEvents(ctx context.Context, tx *sql.Tx, w *core.World) error {
	for i := range w.Events {
		ev := &w.Events[i]
		payloadJSON, err := json.Marshal(ev.Payload)
		if err != nil {
			return fmt.Errorf("persistence: marshal payload for event %s: %w", ev.ID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events (id, parent_event_id, tick, kind, location, payload_json)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			ev.ID, sql.NullString{}, ev.Tick, string(ev.Type), nullStr(ev.Location), string(payloadJSON),
		); err != nil {
			return fmt.Errorf("persistence: write event %s: %w", ev.ID, err)
		}
	}
	return nil
}

// readEvents reads every row from the events table. The payload_json
// column is JSON-decoded into a map[string]any; a missing column
// yields a non-nil empty map.
func readEvents(db *DB) ([]core.Event, error) {
	rows, err := db.Query(`SELECT
		id, tick, kind, location, payload_json
	FROM events`)
	if err != nil {
		return nil, fmt.Errorf("persistence: query events: %w", err)
	}
	defer rows.Close()
	out := []core.Event{}
	for rows.Next() {
		var ev core.Event
		var kind, location, payloadJSON sql.NullString
		if err := rows.Scan(
			&ev.ID, &ev.Tick, &kind, &location, &payloadJSON,
		); err != nil {
			return nil, fmt.Errorf("persistence: scan events: %w", err)
		}
		ev.Type = core.EventType(kind.String)
		ev.Location = location.String
		ev.Payload = decodePayload(payloadJSON)
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate events: %w", err)
	}
	return out, nil
}

// writeItemCatalog writes every item in w.Items to the items
// table. Phase 26 added the items table; the v1-v3 schemas had no
// catalog persistence, so a Restore of a v3-or-earlier DB yields
// an empty w.Items (which the action engine handles gracefully —
// buy/sell returns "no such item").
func writeItemCatalog(ctx context.Context, tx *sql.Tx, w *core.World) error {
	for name, it := range w.Items {
		if name == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO items (name, count, weight, value, max_durability)
			 VALUES (?, ?, ?, ?, ?)`,
			name, it.Count, it.Weight, it.Value, it.MaxDurability,
		); err != nil {
			return fmt.Errorf("persistence: write item %s: %w", name, err)
		}
	}
	return nil
}

// readItemCatalog reads every row from the items table. The count
// column is stored (for catalog entries it's 0, but the table
// reuses the same column for inventory rows in a future schema
// consolidation; for now, only catalog rows use the items table).
func readItemCatalog(db *DB) (map[string]core.Item, error) {
	rows, err := db.Query(`SELECT name, count, weight, value, max_durability FROM items`)
	if err != nil {
		return nil, fmt.Errorf("persistence: query items: %w", err)
	}
	defer rows.Close()
	out := make(map[string]core.Item)
	for rows.Next() {
		var it core.Item
		if err := rows.Scan(&it.Name, &it.Count, &it.Weight, &it.Value, &it.MaxDurability); err != nil {
			return nil, fmt.Errorf("persistence: scan items: %w", err)
		}
		out[it.Name] = it
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("persistence: iterate items: %w", err)
	}
	return out, nil
}

// decodeStringSlice parses a JSON-encoded []string. NULL or empty
// input yields a non-nil empty slice.
func decodeStringSlice(s sql.NullString) []string {
	out := []string{}
	if !s.Valid || s.String == "" {
		return out
	}
	if err := json.Unmarshal([]byte(s.String), &out); err != nil {
		fmt.Fprintf(os.Stderr, "persistence: warning: invalid string-slice JSON %q: %v\n", s.String, err)
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}

// decodePayload parses a JSON-encoded map[string]any. NULL or empty
// input yields a non-nil empty map. json.Unmarshal into any
// produces float64 for JSON numbers, which is what the rest of the
// engine expects (e.g., Event.Payload's avg_hunger float).
func decodePayload(s sql.NullString) map[string]any {
	out := make(map[string]any)
	if !s.Valid || s.String == "" {
		return out
	}
	if err := json.Unmarshal([]byte(s.String), &out); err != nil {
		fmt.Fprintf(os.Stderr, "persistence: warning: invalid payload JSON %q: %v\n", s.String, err)
		return make(map[string]any)
	}
	if out == nil {
		return make(map[string]any)
	}
	return out
}

// writeAllInventories writes inventory rows for the player
// (w.Inventory) and for every NPC with a non-nil Inventory.
// Phase 19: each person's stock round-trips independently
// so a switch back to a pre-buy world preserves merchant
// stock in addition to the player's items.
func writeAllInventories(ctx context.Context, tx *sql.Tx, w *core.World) error {
	if w.PlayerID != "" {
		if err := writeInventory(ctx, tx, w.PlayerID, w.Inventory); err != nil {
			return err
		}
	}
	for _, p := range w.People {
		if p == nil || p.ID == "" {
			continue
		}
		// Avoid double-writing the player (already written
		// above; p.Inventory is w.Inventory when p.ID ==
		// w.PlayerID).
		if p.ID == w.PlayerID {
			continue
		}
		if p.Inventory == nil {
			continue
		}
		if err := writeInventory(ctx, tx, p.ID, p.Inventory); err != nil {
			return err
		}
	}
	return nil
}

// readAllInventories reads inventory rows for the player
// and for every NPC. The player's rows go into w.Inventory;
// every other person's rows go into p.Inventory. Persons
// with no rows still get a non-nil empty Inventory (so the
// action engine can append without nil checks).
//
// The person list comes from the people table, so any NPC
// who existed at Snapshot time and is still in the
// restored w.People will be touched. A person present at
// Snapshot but missing from the restored people map (which
// should never happen given the full-replace semantics) is
// silently skipped — their rows are abandoned in the DB.
func readAllInventories(db *DB, w *core.World) error {
	if w.PlayerID != "" {
		inv, err := readInventory(db, w.PlayerID)
		if err != nil {
			return err
		}
		w.Inventory = inv
	}
	for _, p := range w.People {
		if p == nil || p.ID == "" {
			continue
		}
		if p.ID == w.PlayerID {
			// Already loaded above.
			continue
		}
		inv, err := readInventory(db, p.ID)
		if err != nil {
			return err
		}
		// readInventory always returns a non-nil map (empty
		// when no rows), so p.Inventory is always non-nil
		// after this assignment. The action engine can then
		// append/lookup without nil checks.
		p.Inventory = inv
	}
	return nil
}
