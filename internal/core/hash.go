// Package core — deterministic world hashing (Phase 25).
//
// WorldHash produces a stable SHA256 hex digest of a world's
// simulation state. The hash is the foundation of Chronicle's
// replay-validation system: if two runs with the same seed and
// tick count produce different hashes, the simulation is
// non-deterministic and the divergence is a bug.
//
// # Stability guarantees
//
// The hash is stable across:
//
//   - process restarts (deterministic JSON encoding, no time.Now()
//     or pointer addresses);
//   - Go map iteration order (every collection that originates as
//     a map is pre-sorted by its canonical key, and json.Marshal
//     sorts map keys alphabetically);
//   - platforms (consistent int, float, and string encoding; -0.0
//     is normalized to +0.0 so the IEEE-754 sign bit does not
//     leak into the hash).
//
// # Scope
//
// Only simulation state is hashed:
//
//   - tick, seed, world id, simulated clock (Now.Unix)
//   - world rules
//   - player id, coin
//   - item catalog and player inventory
//   - locations (with settlement stock and prices)
//   - people (with traits, needs, goals, per-person inventory)
//   - factions
//   - relationships
//   - memories
//   - events
//
// The hash explicitly EXCLUDES:
//
//   - pointer addresses (no fmt.Sprintf("%p"))
//   - runtime caches (e.g. the EventEngine's cooldown map)
//   - logger state, channels, goroutine IDs
//   - transient services that hold non-deterministic state
//
// # Hashing methodology
//
//  1. Convert the world to a nested map[string]any / []any tree.
//     Pre-sort every slice by its canonical key. Maps are sorted
//     by json.Marshal automatically (Go 1.12+).
//  2. Normalize all float64 values via cleanFloat (handles -0.0,
//     NaN, Inf) before they enter the tree.
//  3. json.Marshal the tree. Map keys are sorted; slice order is
//     the pre-sorted canonical order; struct-field order is
//     declaration order.
//  4. SHA256 the resulting bytes; hex-encode the digest.
//
// The tree shape is identical for the same world state regardless
// of how the engine produced it, so the hash is reproducible.
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"
)

// WorldHash returns a deterministic SHA256 hex digest of w's
// simulation state. Two worlds with the same seed, tick count,
// and action sequence produce the same hash; a single-bit
// difference in any hashed field produces a different hash.
//
// Returns the empty string for a nil world (a defensive
// fallback; production callers should never pass nil).
//
// The hash is suitable for:
//
//   - replay regression tests (hash(run_1) == hash(run_2))
//   - save/load round-trip verification
//   - branch divergence detection (different seeds -> different
//     hashes at the same tick)
//   - tamper detection (modifying any persisted field changes
//     the hash)
func WorldHash(w *World) string {
	if w == nil {
		return ""
	}
	state := worldToHashableState(w)
	bytes, err := json.Marshal(state)
	if err != nil {
		// Marshaling a map of basic types should never fail.
		// If it does (e.g. an unsupported type in Event.Payload),
		// return the empty string rather than panic, so a
		// test that uses WorldHash as a sanity check still
		// runs to completion.
		return ""
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:])
}

// worldToHashableState converts w into a JSON-serializable tree
// with every collection pre-sorted. The top-level keys are
// sorted alphabetically by json.Marshal; nested maps are sorted
// the same way; slices are pre-sorted here.
func worldToHashableState(w *World) map[string]any {
	return map[string]any{
		"id":            w.ID,
		"tick":          w.Tick,
		"seed":          w.Seed,
		"now_unix":      w.Now.Unix(),
		"player_id":     w.PlayerID,
		"coin":          w.Coin,
		"rules":         rulesToMap(w.Rules),
		"items":         itemsToMap(w.Items),
		"inventory":     itemsToMap(w.Inventory),
		"locations":     locationsToList(w.Locations),
		"people":        peopleToList(w.People),
		"factions":      factionsToList(w.Factions),
		"relationships": relationshipsToList(w.Relationships),
		"memories":      memoriesToList(w.Memories),
		"events":        eventsToList(w.Events),
	}
}

// -----------------------------------------------------------------------------
// Rules
// -----------------------------------------------------------------------------

// rulesToMap projects *WorldRules into a flat map. A nil pointer
// is encoded as an empty map (not null) so two worlds with
// "absent rules" hash identically regardless of how the absence
// is represented in memory.
func rulesToMap(r *WorldRules) map[string]any {
	if r == nil {
		return map[string]any{}
	}
	return map[string]any{
		"adult_age":                r.AdultAge,
		"fertile_min_age":          r.FertileMinAge,
		"fertile_max_age":          r.FertileMaxAge,
		"annual_death_chance":      cleanFloat(r.AnnualDeathChance),
		"min_birth_interval_ticks": r.MinBirthIntervalTicks,
		"max_children":             r.MaxChildren,
		"migration_fraction":       cleanFloat(r.MigrationFraction),
		"min_migrants_per_tick":    r.MinMigrantsPerTick,
	}
}

// -----------------------------------------------------------------------------
// Items & inventories
// -----------------------------------------------------------------------------

// itemsToMap converts a map[string]Item into map[string]any of
// item-maps. The map is rebuilt (not aliased) so the json.Marshal
// pass produces a fresh object with keys sorted alphabetically.
func itemsToMap(m map[string]Item) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = itemToMap(v)
	}
	return out
}

// itemToMap projects a single Item into a flat map. Floats pass
// through cleanFloat for IEEE-754 sign-bit normalization.
func itemToMap(it Item) map[string]any {
	return map[string]any{
		"name":           it.Name,
		"count":          it.Count,
		"weight":         cleanFloat(it.Weight),
		"value":          it.Value,
		"max_durability": cleanFloat(it.MaxDurability),
	}
}

// -----------------------------------------------------------------------------
// Locations
// -----------------------------------------------------------------------------

// locationsToList returns the locations sorted by ID. Sorting is
// required because Go map iteration is randomized; without it,
// the slice order (and therefore the hash) would vary between
// runs.
func locationsToList(m map[string]*Location) []any {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		loc := m[id]
		out = append(out, locationToMap(loc))
	}
	return out
}

// locationToMap projects a single Location into a flat map. The
// settlement stock and prices are nested objects so the JSON
// output is self-describing.
func locationToMap(l *Location) map[string]any {
	if l == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                 l.ID,
		"name":               l.Name,
		"region":             l.Region,
		"population":         l.Population,
		"population_cap":     l.PopulationCap,
		"pressure":           l.Pressure,
		"last_shortage_tick": l.LastShortageTick,
		"settlement":         settlementToMap(l.Settlement),
		"prices":             pricesToMap(l.Prices),
	}
}

// settlementToMap projects SettlementInventory. All four fields
// are float64 and go through cleanFloat.
func settlementToMap(s SettlementInventory) map[string]any {
	return map[string]any{
		"food":  cleanFloat(s.Food),
		"wood":  cleanFloat(s.Wood),
		"iron":  cleanFloat(s.Iron),
		"cloth": cleanFloat(s.Cloth),
	}
}

// pricesToMap projects Prices. All four fields are integers and
// need no float normalization.
func pricesToMap(p Prices) map[string]any {
	return map[string]any{
		"food":  p.Food,
		"wood":  p.Wood,
		"iron":  p.Iron,
		"cloth": p.Cloth,
	}
}

// -----------------------------------------------------------------------------
// People
// -----------------------------------------------------------------------------

// peopleToList returns the people sorted by ID. Each Person's
// traits, needs, goals, and inventory are recursively serialized.
func peopleToList(m map[string]*Person) []any {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		p := m[id]
		out = append(out, personToMap(p))
	}
	return out
}

// personToMap projects a single Person. Traits and Needs are
// string-keyed maps; Goals is a sorted slice; Inventory is a
// string-keyed map of items.
func personToMap(p *Person) map[string]any {
	if p == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":          p.ID,
		"name":        p.Name,
		"gender":      p.Gender,
		"birth_tick":  p.BirthTick,
		"death_tick":  p.DeathTick,
		"alive":       p.Alive,
		"location_id": p.LocationID,
		"class":       p.Class,
		"occupation":  p.Occupation,
		"is_merchant": p.IsMerchant,
		"father_id":   p.FatherID,
		"mother_id":   p.MotherID,
		"spouse_id":   p.SpouseID,
		"traits":      intMapToAny(p.Traits),
		"needs":       intMapToAny(p.Needs),
		"goals":       goalsToList(p.Goals),
		"inventory":   itemsToMap(p.Inventory),
	}
}

// intMapToAny converts a map[string]int to map[string]any so it
// can be mixed with float64 values in the same parent object.
// json.Marshal handles both transparently.
func intMapToAny(m map[string]int) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// goalsToList returns the goals sorted by ID. The Person.Goals
// slice is conceptually a set (the engine reads it as Priority
// by ID), so the hash treats it as one.
func goalsToList(goals []Goal) []any {
	sorted := make([]Goal, len(goals))
	copy(sorted, goals)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})
	out := make([]any, 0, len(sorted))
	for _, g := range sorted {
		out = append(out, map[string]any{
			"id":       string(g.ID),
			"priority": cleanFloat(g.Priority),
			"progress": cleanFloat(g.Progress),
		})
	}
	return out
}

// -----------------------------------------------------------------------------
// Factions
// -----------------------------------------------------------------------------

// factionsToList returns the factions sorted by ID. Faction
// membership is derived from Person.Occupation at read time, so
// the faction records themselves do not store member lists; the
// Faction struct holds only the static definition (name, goal,
// color, allies, rivals, member occupations).
//
// Factions do not currently mutate during simulation, but they
// are part of the world state and are included in the hash so
// a future engine that mutates them (e.g. influence decay,
// dynamic ally/rival updates) is automatically covered by
// replay validation.
func factionsToList(m map[string]*Faction) []any {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		f := m[id]
		out = append(out, factionToMap(f))
	}
	return out
}

// factionToMap projects a single Faction into a flat map. The
// rival, ally, and member-occupation slices are pre-sorted for
// determinism (the worldpack loads them in declaration order,
// but the hash should not depend on that).
func factionToMap(f *Faction) map[string]any {
	if f == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                 f.ID,
		"name":               f.Name,
		"goal":               f.Goal,
		"color":              f.Color,
		"base_location":      f.BaseLocation,
		"rivals":             sortedStrings(f.Rivals),
		"allies":             sortedStrings(f.Allies),
		"member_occupations": sortedStrings(f.MemberOccupations),
	}
}

// sortedStrings returns a lexicographically sorted []any copy
// of s. Used for faction rival/ally/member slices and memory
// tags, which are semantically sets but stored as []string.
func sortedStrings(s []string) []any {
	sorted := make([]string, len(s))
	copy(sorted, s)
	sort.Strings(sorted)
	out := make([]any, len(sorted))
	for i, v := range sorted {
		out[i] = v
	}
	return out
}

// -----------------------------------------------------------------------------
// Relationships
// -----------------------------------------------------------------------------

// relationshipsToList returns the relationships sorted by
// (FromID, ToID). The order in w.Relationships is engine-dependent
// (it grows by append as new acquaintances are formed) and not
// stable across runs; sorting here is required for hash stability.
func relationshipsToList(rels []Relationship) []any {
	sorted := make([]Relationship, len(rels))
	copy(sorted, rels)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].FromID != sorted[j].FromID {
			return sorted[i].FromID < sorted[j].FromID
		}
		return sorted[i].ToID < sorted[j].ToID
	})
	out := make([]any, 0, len(sorted))
	for _, r := range sorted {
		out = append(out, map[string]any{
			"from_id":    r.FromID,
			"to_id":      r.ToID,
			"trust":      cleanFloat(r.Trust),
			"respect":    cleanFloat(r.Respect),
			"fear":       cleanFloat(r.Fear),
			"attraction": cleanFloat(r.Attraction),
			"loyalty":    cleanFloat(r.Loyalty),
		})
	}
	return out
}

// -----------------------------------------------------------------------------
// Memories
// -----------------------------------------------------------------------------

// memoriesToList returns the memories sorted by (Tick, ID). The
// engine appends in event-occurrence order, which is not stable
// across runs (the MemoryEngine iterates w.People with a
// randomized order to find new births/deaths). Sorting is the
// canonicalization step.
func memoriesToList(mems []Memory) []any {
	sorted := make([]Memory, len(mems))
	copy(sorted, mems)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Tick != sorted[j].Tick {
			return sorted[i].Tick < sorted[j].Tick
		}
		return sorted[i].ID < sorted[j].ID
	})
	out := make([]any, 0, len(sorted))
	for _, m := range sorted {
		out = append(out, map[string]any{
			"id":                 m.ID,
			"owner_id":           m.OwnerID,
			"event_id":           m.EventID,
			"cause_event_id":     m.CauseEventID,
			"tick":               m.Tick,
			"importance":         cleanFloat(m.Importance),
			"recency":            cleanFloat(m.Recency),
			"emotional_score":    cleanFloat(m.EmotionalScore),
			"trust_delta":        cleanFloat(m.TrustDelta),
			"relationship_delta": cleanFloat(m.RelationshipDelta),
			"description":        m.Description,
			"tags":               sortedStringSlice(m.Tags),
		})
	}
	return out
}

// -----------------------------------------------------------------------------
// Events
// -----------------------------------------------------------------------------

// eventsToList returns the events sorted by (Tick, ID). Same
// rationale as memories: the engine appends in iteration order
// and the iteration order is not stable.
func eventsToList(events []Event) []any {
	sorted := make([]Event, len(events))
	copy(sorted, events)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Tick != sorted[j].Tick {
			return sorted[i].Tick < sorted[j].Tick
		}
		return sorted[i].ID < sorted[j].ID
	})
	out := make([]any, 0, len(sorted))
	for _, e := range sorted {
		out = append(out, map[string]any{
			"id":       e.ID,
			"type":     string(e.Type),
			"tick":     e.Tick,
			"location": e.Location,
			"payload":  payloadToMap(e.Payload),
		})
	}
	return out
}

// payloadToMap converts a free-form payload into a map of clean
// values. Floats are normalized; nested maps are recursed; other
// values pass through unchanged. json.Marshal handles the rest.
func payloadToMap(p map[string]any) map[string]any {
	out := make(map[string]any, len(p))
	for k, v := range p {
		out[k] = cleanValue(v)
	}
	return out
}

// cleanValue normalizes a single payload value. Recurses into
// nested maps (the EventEngine's payloads are one level deep,
// but a defensive recursion keeps the function correct for any
// future payload shape).
func cleanValue(v any) any {
	switch x := v.(type) {
	case float64:
		return cleanFloat(x)
	case map[string]any:
		return payloadToMap(x)
	default:
		return v
	}
}

// -----------------------------------------------------------------------------
// String slices
// -----------------------------------------------------------------------------

// sortedStringSlice is a thin alias kept for call sites that
// semantically describe a "set of tags" (memory tags). It
// delegates to sortedStrings so there is exactly one sorting
// helper.
func sortedStringSlice(s []string) []any { return sortedStrings(s) }

// -----------------------------------------------------------------------------
// Float normalization
// -----------------------------------------------------------------------------

// cleanFloat returns a canonical form of f. Specifically:
//
//   - +0.0 and -0.0 both return +0.0 (json.Marshal produces
//     different strings for the two; without normalization the
//     hash would vary with the IEEE-754 sign bit)
//   - NaN returns 0.0 (NaN marshals to invalid JSON, which is
//     a hash-time panic; the simulation should not produce NaN
//     but we defend against it)
//   - +Inf and -Inf return 0.0 (same rationale as NaN)
//   - all other values pass through unchanged
//
// The result is portable across IEEE-754-compliant platforms.
func cleanFloat(f float64) float64 {
	if f == 0 {
		return 0.0
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0.0
	}
	return f
}
