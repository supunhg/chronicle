package worldpack

import (
	"testing"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// TestBuildItemCatalog_ExplicitItemsTakePrecedence verifies
// that economy.Items entries override the default table.
func TestBuildItemCatalog_ExplicitItemsTakePrecedence(t *testing.T) {
	economy := EconomySpec{
		Items: []ItemSpec{
			{Name: "bread", Value: 99, Weight: 1.0, MaxDurability: 0.5},
		},
		Resources: []string{"bread"},
	}
	cat := BuildItemCatalog(economy)
	if cat["bread"].Value != 99 {
		t.Errorf("bread.Value = %d, want 99 (explicit override)", cat["bread"].Value)
	}
	if cat["bread"].Weight != 1.0 {
		t.Errorf("bread.Weight = %f, want 1.0", cat["bread"].Weight)
	}
}

// TestBuildItemCatalog_ResourcesGetDefaults verifies that
// resources listed only in economy.Resources (no explicit
// ItemSpec) get the DefaultItemSpec values.
func TestBuildItemCatalog_ResourcesGetDefaults(t *testing.T) {
	economy := EconomySpec{
		Resources: []string{"bread"},
	}
	cat := BuildItemCatalog(economy)
	if cat["bread"].Value != 3 {
		t.Errorf("bread.Value = %d, want 3 (default)", cat["bread"].Value)
	}
	if cat["bread"].Weight != 0.5 {
		t.Errorf("bread.Weight = %f, want 0.5 (default)", cat["bread"].Weight)
	}
}

// TestBuildMerchantInventory_AllowlistStocksOnlyListedItems
// verifies that when an allowlist is provided, the merchant
// stocks ONLY the listed items (case-insensitive), each at
// the starting count.
func TestBuildMerchantInventory_AllowlistStocksOnlyListedItems(t *testing.T) {
	catalog := map[string]core.Item{
		"bread":  {Name: "bread", Weight: 0.5, Value: 3, MaxDurability: 0.0},
		"ale":    {Name: "ale", Weight: 1.0, Value: 2, MaxDurability: 0.0},
		"sword":  {Name: "sword", Weight: 4.0, Value: 50, MaxDurability: 1.0},
		"shield": {Name: "shield", Weight: 6.0, Value: 35, MaxDurability: 1.0},
		"potion": {Name: "potion", Weight: 0.3, Value: 20, MaxDurability: 0.0},
	}
	stock := BuildMerchantInventory([]string{"sword", "shield"}, catalog, 5)
	if len(stock) != 2 {
		t.Errorf("len(stock) = %d, want 2 (sword + shield only)", len(stock))
	}
	if stock["sword"].Count != 5 {
		t.Errorf("sword count = %d, want 5", stock["sword"].Count)
	}
	if stock["shield"].Count != 5 {
		t.Errorf("shield count = %d, want 5", stock["shield"].Count)
	}
	if _, exists := stock["bread"]; exists {
		t.Errorf("bread should not be in stock (not in allowlist)")
	}
}

// TestBuildMerchantInventory_AllowlistCaseInsensitive
// verifies that allowlist entries are lowercased before
// lookup, so "Sword" and "sword" both work.
func TestBuildMerchantInventory_AllowlistCaseInsensitive(t *testing.T) {
	catalog := map[string]core.Item{
		"sword": {Name: "sword", Weight: 4.0, Value: 50, MaxDurability: 1.0},
	}
	stock := BuildMerchantInventory([]string{"Sword", "SHIELD"}, catalog, 3)
	if len(stock) != 1 {
		t.Errorf("len(stock) = %d, want 1 (only 'sword' is in catalog)", len(stock))
	}
	if stock["sword"].Count != 3 {
		t.Errorf("sword count = %d, want 3", stock["sword"].Count)
	}
}

// TestBuildMerchantInventory_AllowlistSkipsUnknown verifies
// that allowlist entries not present in the catalog are
// silently skipped (a typo in occupations.yaml must not
// crash bootstrap).
func TestBuildMerchantInventory_AllowlistSkipsUnknown(t *testing.T) {
	catalog := map[string]core.Item{
		"sword": {Name: "sword", Weight: 4.0, Value: 50, MaxDurability: 1.0},
	}
	stock := BuildMerchantInventory([]string{"sword", "unicorn", ""}, catalog, 2)
	if len(stock) != 1 {
		t.Errorf("len(stock) = %d, want 1 (sword only; unicorn and empty skipped)", len(stock))
	}
}

// TestBuildMerchantInventory_AllowlistDeduplicates verifies
// that duplicate allowlist entries yield a single inventory
// entry (not stacked).
func TestBuildMerchantInventory_AllowlistDeduplicates(t *testing.T) {
	catalog := map[string]core.Item{
		"sword": {Name: "sword", Weight: 4.0, Value: 50, MaxDurability: 1.0},
	}
	stock := BuildMerchantInventory([]string{"sword", "SWORD", "Sword"}, catalog, 4)
	if len(stock) != 1 {
		t.Errorf("len(stock) = %d, want 1 (deduplicated)", len(stock))
	}
	if stock["sword"].Count != 4 {
		t.Errorf("sword count = %d, want 4", stock["sword"].Count)
	}
}

// TestBuildMerchantInventory_EmptyAllowlistStocksAllCatalog
// verifies the Phase 19 backward-compat behavior: an empty
// or nil allowlist yields the full catalog (the merchant
// stocks every item, like a general store).
func TestBuildMerchantInventory_EmptyAllowlistStocksAllCatalog(t *testing.T) {
	catalog := map[string]core.Item{
		"bread": {Name: "bread", Weight: 0.5, Value: 3, MaxDurability: 0.0},
		"sword": {Name: "sword", Weight: 4.0, Value: 50, MaxDurability: 1.0},
	}
	stock := BuildMerchantInventory(nil, catalog, 7)
	if len(stock) != 2 {
		t.Errorf("len(stock) = %d, want 2 (full catalog)", len(stock))
	}
	if stock["bread"].Count != 7 {
		t.Errorf("bread count = %d, want 7", stock["bread"].Count)
	}
	if stock["sword"].Count != 7 {
		t.Errorf("sword count = %d, want 7", stock["sword"].Count)
	}
	// Empty (non-nil) allowlist also triggers the fallback.
	stock2 := BuildMerchantInventory([]string{}, catalog, 3)
	if len(stock2) != 2 {
		t.Errorf("empty allowlist: len(stock) = %d, want 2 (full catalog fallback)", len(stock2))
	}
}

// TestBuildMerchantInventory_DefaultStartingCount verifies
// that a non-positive startingCount defaults to 10 (Phase 19
// behavior).
func TestBuildMerchantInventory_DefaultStartingCount(t *testing.T) {
	catalog := map[string]core.Item{
		"sword": {Name: "sword", Weight: 4.0, Value: 50, MaxDurability: 1.0},
	}
	stock := BuildMerchantInventory([]string{"sword"}, catalog, 0)
	if stock["sword"].Count != 10 {
		t.Errorf("sword count = %d, want 10 (default)", stock["sword"].Count)
	}
	stock = BuildMerchantInventory([]string{"sword"}, catalog, -5)
	if stock["sword"].Count != 10 {
		t.Errorf("sword count = %d, want 10 (default for negative)", stock["sword"].Count)
	}
}

// TestBuildMerchantInventory_CopiesMetadata verifies that
// each inventory entry copies the catalog's Weight, Value,
// and MaxDurability — the same contract as Phase 19's
// direct seeding.
func TestBuildMerchantInventory_CopiesMetadata(t *testing.T) {
	catalog := map[string]core.Item{
		"sword": {Name: "sword", Weight: 4.0, Value: 50, MaxDurability: 1.0},
	}
	stock := BuildMerchantInventory([]string{"sword"}, catalog, 5)
	s := stock["sword"]
	if s.Weight != 4.0 {
		t.Errorf("sword.Weight = %f, want 4.0", s.Weight)
	}
	if s.Value != 50 {
		t.Errorf("sword.Value = %d, want 50", s.Value)
	}
	if s.MaxDurability != 1.0 {
		t.Errorf("sword.MaxDurability = %f, want 1.0", s.MaxDurability)
	}
}

// TestBuildMerchantInventory_NeverNil verifies the function
// returns a non-nil map even when the allowlist matches
// nothing. The action engine assumes a non-nil Inventory.
func TestBuildMerchantInventory_NeverNil(t *testing.T) {
	stock := BuildMerchantInventory([]string{"unicorn"}, map[string]core.Item{}, 5)
	if stock == nil {
		t.Errorf("stock is nil; expected non-nil empty map")
	}
}
