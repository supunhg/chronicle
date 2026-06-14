package worldpack

import (
	"fmt"
	"math/rand"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// Bootstrap creates the initial world state from a Pack into w.
//
// The world w must already exist (use core.NewWorld). Bootstrap:
//
//  1. Creates all locations from pack.Locations.
//  2. Creates all factions from pack.Factions.
//  3. Generates pack.Generation.Population NPCs, deterministically,
//     using a math/rand RNG seeded with `seed`.
//
// Slot allocation interprets the location_distribution buckets as:
//   - "villages"  -> round-robin across all village-kind locations
//   - "travelers" -> no LocationID (empty string)
//   - "nobility"  -> blackwater with class="upper"
//   - "clergy"    -> dawn_monastery with occupation="priest"
//   - any other   -> the bucket's Location string verbatim
//
// All other buckets fall through to the verbatim-location case, so a
// worldpack with location names matching real location IDs works
// without special-casing.
func Bootstrap(pack *Pack, w *core.World, seed int64) error {
	if pack == nil {
		return fmt.Errorf("worldpack: nil pack")
	}
	if w == nil {
		return fmt.Errorf("worldpack: nil world")
	}

	// 1. Locations
	for _, ls := range pack.Locations {
		w.AddLocation(core.NewLocation(ls.ID, ls.Name, ls.Region, ls.PopulationCap))
	}

	// 2. Factions
	if w.Factions == nil {
		w.Factions = make(map[string]*core.Faction)
	}
	for _, fs := range pack.Factions {
		w.Factions[fs.ID] = &core.Faction{
			ID:                fs.ID,
			Name:              fs.Name,
			Goal:              fs.Goal,
			Color:             fs.Color,
			BaseLocation:      fs.BaseLocation,
			Rivals:            fs.Rivals,
			Allies:            fs.Allies,
			MemberOccupations: fs.Members.Occupations,
		}
	}

	// 2.5 World rules (populated from pack.Rules.Lifecycle and
	// pack.Rules.Family). Engines consult w.RulesOrDefault() to read
	// these; if w.Rules is nil, the engine falls back to built-in
	// defaults.
	w.Rules = rulesFromPack(pack)

	// 2.6 Item catalog (populated from pack.Rules.Economy.Items
	// and pack.Rules.Economy.Resources). The action engine's
	// buy/sell handlers read the per-item Value from this
	// catalog. A worldpack with neither Items nor Resources
	// produces an empty catalog (which makes buy/sell always
	// reject — the correct Phase 17.6 fallback for legacy
	// worlds that ship without an economy config).
	w.Items = BuildItemCatalog(pack.Rules.Economy)

	// 3. Deterministic RNG
	r := rand.New(rand.NewSource(seed))

	// 4. Build slot list from the generation spec
	slots := assignSlots(pack, r)
	if pack.Generation.Population.Total != len(slots) {
		return fmt.Errorf("worldpack: population.Total=%d, slots=%d", pack.Generation.Population.Total, len(slots))
	}

	// 5. Occupation lookup tables
	occByID := make(map[string]OccupationSpec, len(pack.Occupations))
	occByClass := make(map[string][]OccupationSpec)
	for _, o := range pack.Occupations {
		occByID[o.ID] = o
		occByClass[o.SocialClass] = append(occByClass[o.SocialClass], o)
	}

	// 6. Generate people
	for i, s := range slots {
		p := generatePerson(pack, s, i+1, r, occByID, occByClass)
		w.AddPerson(p)
	}

	// 6.5 Seed merchant inventories. Phase 19: any Person whose
	// occupation has is_merchant=true gets an Inventory seeded
	// from the worldpack's item catalog. Phase 20: the
	// occupation's merchant_inventory allowlist (if non-empty)
	// restricts the seed to a niche subset. The starting count
	// defaults to 10 per item (or whatever merchant_starting_stock
	// says). The action engine's resolveBuy decrements the
	// merchant's Count; resolveSell increments it.
	for _, p := range w.People {
		if !p.IsMerchant {
			continue
		}
		occSpec, ok := occByID[p.Occupation]
		if !ok {
			continue
		}
		p.Inventory = BuildMerchantInventory(occSpec.MerchantInventory, w.Items, occSpec.MerchantStartingStock)
	}

	// 7. Recompute populations (Location.Population is derived state)
	w.RecomputeLocationPopulations()

	return nil
}

// rulesFromPack projects a Pack's Rules block into a *core.WorldRules.
// core.WorldRules is a flat struct (not *Pack) to avoid a circular
// import between core and worldpack.
func rulesFromPack(pack *Pack) *core.WorldRules {
	lc := pack.Rules.Lifecycle
	fc := pack.Rules.Family
	mc := pack.Rules.Migration
	return &core.WorldRules{
		AdultAge:              lc.AdultAge,
		FertileMinAge:         lc.FertileMinAge,
		FertileMaxAge:         lc.FertileMaxAge,
		AnnualDeathChance:     lc.AnnualDeathChance,
		MinBirthIntervalTicks: int64(fc.MinBirthIntervalTicks),
		MaxChildren:           fc.MaxChildren,
		MigrationFraction:     mc.FractionMovedPerTick,
		MinMigrantsPerTick:    mc.MinMigrantsPerTick,
	}
}

// slot is one person's location/class/occupation assignment, resolved
// from the location_distribution buckets before the per-person loop.
type slot struct {
	LocationID    string
	ClassOverride string // "" = sample from social_class_distribution
	OccOverride   string // "" = sample from class-appropriate occupations
}

// assignSlots walks the location_distribution buckets and produces a
// flat list of slot assignments (one per person), then shuffles it so
// the generated Person IDs (n0001..nNNNN) are not all in the same
// location. The shuffle is deterministic given the same RNG.
func assignSlots(pack *Pack, r *rand.Rand) []slot {
	var villages []string
	for _, l := range pack.Locations {
		if l.Kind == "village" {
			villages = append(villages, l.ID)
		}
	}

	var slots []slot
	for _, b := range pack.Generation.LocationDistribution {
		switch b.Location {
		case "villages":
			if len(villages) == 0 {
				continue
			}
			for i := 0; i < b.Count; i++ {
				slots = append(slots, slot{LocationID: villages[i%len(villages)]})
			}
		case "travelers":
			for i := 0; i < b.Count; i++ {
				slots = append(slots, slot{LocationID: ""})
			}
		case "nobility":
			for i := 0; i < b.Count; i++ {
				slots = append(slots, slot{LocationID: "blackwater", ClassOverride: "upper"})
			}
		case "clergy":
			for i := 0; i < b.Count; i++ {
				slots = append(slots, slot{LocationID: "dawn_monastery", OccOverride: "priest"})
			}
		default:
			for i := 0; i < b.Count; i++ {
				slots = append(slots, slot{LocationID: b.Location})
			}
		}
	}

	r.Shuffle(len(slots), func(i, j int) { slots[i], slots[j] = slots[j], slots[i] })
	return slots
}

// generatePerson fills a single Person record from a slot. All random
// choices use the shared deterministic RNG.
func generatePerson(
	pack *Pack,
	s slot,
	idx int,
	r *rand.Rand,
	occByID map[string]OccupationSpec,
	occByClass map[string][]OccupationSpec,
) *core.Person {
	// Gender
	gender := "F"
	if r.Float64() > pack.Generation.GenderRatio.Female {
		gender = "M"
	}

	// Age in years (Phase 4 v1: years; 365 ticks/year)
	age := sampleAge(pack.Generation.AgeDistribution, r)

	// Social class
	class := s.ClassOverride
	if class == "" {
		class = sampleClass(pack.Generation.SocialClassDistribution, r)
	}

	// Occupation
	occupation := s.OccOverride
	if occupation == "" {
		candidates := occByClass[class]
		if len(candidates) == 0 {
			// Fallback: any occupation
			for _, o := range occByID {
				candidates = append(candidates, o)
			}
		}
		occupation = candidates[r.Intn(len(candidates))].ID
	}

	// Name
	name := pickName(pack.Generation.Names, gender, r)

	// BirthTick: age years before world start (t=0). Negative birth
	// tick means the person was born before the simulation began.
	birthTick := -int64(age) * 365

	// Traits
	traits := make(map[string]int, len(pack.Generation.Traits))
	for traitName, spec := range pack.Generation.Traits {
		v := spec.Default
		if spec.Max > spec.Min {
			v = spec.Min + r.Intn(spec.Max-spec.Min+1)
		}
		traits[traitName] = v
	}

	// Needs: initialize from occupation weights, then ensure common
	// needs are present at a default value of 50.
	needs := make(map[string]int)
	if occ, ok := occByID[occupation]; ok {
		for need, weight := range occ.NeedsWeights {
			v := int(weight * 50)
			if v > 100 {
				v = 100
			}
			needs[need] = v
		}
	}
	for _, need := range []string{
		"hunger", "wealth", "safety", "loneliness", "health",
		"romance", "status", "ambition", "devout",
	} {
		if _, ok := needs[need]; !ok {
			needs[need] = 50
		}
	}

	// Merchant flag (Phase 19). Set from the occupation spec
	// so the action engine can identify this person as a
	// merchant at the player's location.
	isMerchant := false
	if occ, ok := occByID[occupation]; ok {
		isMerchant = occ.IsMerchant
	}

	return &core.Person{
		ID:         fmt.Sprintf("n%04d", idx),
		Name:       name,
		Gender:     gender,
		BirthTick:  birthTick,
		Alive:      true,
		LocationID: s.LocationID,
		Class:      class,
		Occupation: occupation,
		IsMerchant: isMerchant,
		Traits:     traits,
		Needs:      needs,
		Goals:      []core.Goal{},
	}
}

// sampleAge picks an age bracket per share, then a uniform age in
// the bracket.
func sampleAge(brackets []AgeBracket, r *rand.Rand) int {
	if len(brackets) == 0 {
		return 30
	}
	u := r.Float64()
	cum := 0.0
	bracket := brackets[len(brackets)-1]
	for _, b := range brackets {
		cum += b.Share
		if u < cum {
			bracket = b
			break
		}
	}
	lo, hi := bracket.Range[0], bracket.Range[1]
	if hi < lo {
		hi = lo
	}
	return lo + r.Intn(hi-lo+1)
}

// sampleClass picks a class per share. "upper" is filtered out: the
// "nobility" location bucket is the source of truth for upper class
// and assigns exactly 5 people; the 5% share in the spec is
// informational, not a sampling share. This avoids double-counting
// (5% of 150 = ~8, plus 5 nobility = 13) and keeps the bootstrap
// total aligned with the bucket counts.
//
// NOTE: a worldpack that uses social_class_distribution to assign
// upper class (no "nobility"-style bucket) will produce zero upper
// class people, because the distribution's upper share is filtered
// out unconditionally. If a future worldpack needs upper class from
// the distribution, add a per-pack toggle here.
func sampleClass(buckets []ClassBucket, r *rand.Rand) string {
	var filtered []ClassBucket
	var total float64
	for _, b := range buckets {
		if b.Class == "upper" {
			continue
		}
		filtered = append(filtered, b)
		total += b.Share
	}
	if len(filtered) == 0 {
		return "lower"
	}
	if total == 0 {
		return filtered[0].Class
	}
	u := r.Float64()
	cum := 0.0
	bucket := filtered[len(filtered)-1]
	for _, b := range filtered {
		// Renormalize so the remaining shares sum to 1.0.
		cum += b.Share / total
		if u < cum {
			bucket = b
			break
		}
	}
	return bucket.Class
}

// pickName samples a first name from the appropriate gender pool and
// a surname from the surname pool.
func pickName(pools NamePools, gender string, r *rand.Rand) string {
	first := pools.Male
	if gender == "F" {
		first = pools.Female
	}
	if len(first) == 0 || len(pools.Surnames) == 0 {
		return "Anon"
	}
	return first[r.Intn(len(first))] + " " + pools.Surnames[r.Intn(len(pools.Surnames))]
}
