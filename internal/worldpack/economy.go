package worldpack

import (
	"strings"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// BuildItemCatalog builds a map[string]core.Item (the world's
// item catalog) from the worldpack's EconomySpec. The catalog
// is the source of truth for per-item metadata: the action
// engine's buy/sell handlers look up the per-item Value from
// the catalog instead of the Phase 17.6 hardcoded priceList.
//
// Behavior:
//   - Every entry in economy.Items becomes a catalog entry.
//     Missing fields fall back to DefaultItemSpec (per name).
//   - Resources listed in economy.Resources but NOT in
//     economy.Items are added with full defaults (so a minimal
//     worldpack with just `resources: [bread, sword]` still
//     gets a usable catalog).
//   - Item names are lowercased to match the action engine's
//     case-insensitive lookup.
//
// The returned map may be empty (if both Items and Resources
// are empty). A world with no catalog cannot buy or sell,
// which is the correct Phase 17.6 fallback for legacy worlds.
func BuildItemCatalog(economy EconomySpec) map[string]core.Item {
	out := make(map[string]core.Item)

	// Pass 1: explicit ItemSpec entries (these can override any
	// defaults by name).
	for _, spec := range economy.Items {
		name := strings.ToLower(strings.TrimSpace(spec.Name))
		if name == "" {
			continue
		}
		defWeight, defValue, defDur := DefaultItemSpec(name)
		weight := spec.Weight
		if weight == 0 {
			weight = defWeight
		}
		value := spec.Value
		if value == 0 {
			value = defValue
		}
		dur := spec.MaxDurability
		if dur == 0 {
			dur = defDur
		}
		out[name] = core.Item{
			Name:          name,
			Weight:        weight,
			Value:         value,
			MaxDurability: dur,
		}
	}

	// Pass 2: bare resource names that don't have an explicit
	// ItemSpec. Use full defaults from DefaultItemSpec.
	for _, name := range economy.Resources {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		if _, exists := out[key]; exists {
			continue
		}
		w, v, d := DefaultItemSpec(key)
		out[key] = core.Item{
			Name:          key,
			Weight:        w,
			Value:         v,
			MaxDurability: d,
		}
	}

	return out
}

// DefaultItemSpec returns the Phase 18 default weight, value,
// and max-durability for a given resource name. This is a
// hand-tuned v1 table for the common Phase 17.6 goods. The
// worldpack's economy.yaml can override any of these by
// listing the item in `economy.items` with explicit values.
//
// Durability semantics:
//   - 1.0 = pristine (tools, weapons, books)
//   - 0.5 = consumable/wearable (rope, torch)
//   - 0.0 = perishable, no durability (food, drink, potions)
func DefaultItemSpec(name string) (weight float64, value int, maxDurability float64) {
	switch strings.ToLower(name) {
	// Perishables — no durability.
	case "bread":
		return 0.5, 3, 0.0
	case "apple":
		return 0.2, 1, 0.0
	case "cheese":
		return 0.4, 5, 0.0
	case "meat":
		return 1.0, 8, 0.0
	case "ale":
		return 1.0, 2, 0.0
	case "potion":
		return 0.3, 20, 0.0
	// Wearables — half durability.
	case "rope":
		return 1.0, 4, 0.5
	case "torch":
		return 0.5, 2, 0.5
	// Tools/weapons/books — full durability.
	case "bed":
		return 30.0, 15, 1.0
	case "sword":
		return 4.0, 50, 1.0
	case "shield":
		return 6.0, 35, 1.0
	case "book":
		return 0.5, 10, 1.0
	default:
		// Sensible fallback for unknown resources: 1 kg, free,
		// half durability.
		return 1.0, 0, 0.5
	}
}
