// Package worldpack loads Chronicle world packs (YAML configuration
// bundles) from disk and applies them to a core.World.
//
// A world pack is a directory of six YAML files:
//
//	entities.yaml    - region, locations, trade routes, geography
//	factions.yaml    - political / social groups
//	actions.yaml     - 3-layer contextual action rules
//	occupations.yaml - occupation archetypes
//	rules.yaml       - tunable lifecycle, family, migration, etc.
//	generation.yaml  - bootstrap parameters (population, age, class)
//
// The Pack struct is the in-memory representation of these files. Use
// Load to parse a directory into a Pack, and Bootstrap to apply a Pack
// to a fresh core.World.
package worldpack

// Pack is a fully-loaded worldpack: all six YAML files parsed into
// Go structs. Fields are populated by Load and consumed by Bootstrap.
type Pack struct {
	// Region is the world's region (name, size, era).
	Region Region

	// Locations is the list of settlement specs (towns, villages, forts).
	Locations []LocationSpec

	// TradeRoutes connect locations; not yet used by Phase 4 bootstrap.
	TradeRoutes []TradeRoute

	// Geography describes the region's natural features; not yet used.
	Geography Geography

	// Factions is the list of faction specs.
	Factions []FactionSpec

	// Occupations is the list of occupation archetypes.
	Occupations []OccupationSpec

	// ActionRules is the list of contextual action rules (layer 2).
	ActionRules []ActionRuleSpec

	// Rules is the bundle of tunable world parameters.
	Rules RulesSpec

	// Generation is the bundle of bootstrap parameters.
	Generation GenerationSpec
}

// Region is the top-level description of a world's setting.
type Region struct {
	Name   string `yaml:"name"`
	SizeKm int    `yaml:"size_km"`
	Era    string `yaml:"era"`
}

// LocationSpec is one location in entities.yaml.
type LocationSpec struct {
	ID            string   `yaml:"id"`
	Name          string   `yaml:"name"`
	Kind          string   `yaml:"kind"`
	Region        string   `yaml:"region"`
	Population    int      `yaml:"population"`
	PopulationCap int      `yaml:"population_cap"`
	Buildings     []string `yaml:"buildings"`
}

// TradeRoute connects two locations.
type TradeRoute struct {
	ID   string `yaml:"id"`
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// Geography describes natural features of a region.
type Geography struct {
	Forest    string   `yaml:"forest"`
	River     string   `yaml:"river"`
	Landmarks []string `yaml:"landmarks"`
}

// FactionSpec is one faction in factions.yaml.
type FactionSpec struct {
	ID           string         `yaml:"id"`
	Name         string         `yaml:"name"`
	Goal         string         `yaml:"goal"`
	Color        string         `yaml:"color"`
	Members      FactionMembers `yaml:"members"`
	Rivals       []string       `yaml:"rivals"`
	Allies       []string       `yaml:"allies"`
	BaseLocation string         `yaml:"base_location"`
}

// FactionMembers is the membership rule for a faction.
type FactionMembers struct {
	Occupations         []string       `yaml:"occupations"`
	RecruitmentCriteria map[string]any `yaml:"recruitment_criteria"`
}

// OccupationSpec is one occupation in occupations.yaml.
type OccupationSpec struct {
	ID           string             `yaml:"id"`
	Name         string             `yaml:"name"`
	Category     string             `yaml:"category"`
	NeedsWeights map[string]float64 `yaml:"needs_weights"`
	BaseWealth   int                `yaml:"base_wealth"`
	SocialClass  string             `yaml:"social_class"`
	Locations    []string           `yaml:"locations"`

	// IsMerchant marks this occupation as a merchant. Phase 19:
	// Bootstrap sets Person.IsMerchant=true for everyone in this
	// occupation, and seeds their Inventory from the world's item
	// catalog (default 10 of each item). The action engine's
	// resolveBuy/resolveSell only operate when a merchant is
	// at the same location as the player.
	IsMerchant bool `yaml:"is_merchant"`

	// MerchantStartingStock is the per-item starting count for
	// merchants in this occupation. Phase 19: defaults to 10 of
	// each catalog item when omitted. A worldpack can set this
	// to e.g. 25 for a well-stocked general store, or 3 for a
	// traveling peddler with limited goods.
	MerchantStartingStock int `yaml:"merchant_starting_stock"`

	// MerchantInventory is an allowlist of items this merchant
	// stocks. Phase 20: when set, the merchant's starting
	// inventory contains ONLY these items (each at
	// MerchantStartingStock count). When omitted or empty, the
	// merchant gets the full catalog (backward compat with
	// Phase 19). The allowlist enables niche merchants — a
	// swordsmith stocks sword + shield, not bread + potion +
	// bed. Item names are lowercased before lookup; entries
	// not present in the world catalog are silently skipped.
	MerchantInventory []string `yaml:"merchant_inventory"`
}

// ActionRuleSpec is one contextual action rule in actions.yaml.
// The When/Effects maps are kept as raw interface{} so the loader does
// not need to know the action DSL; the Goal engine interprets them.
type ActionRuleSpec struct {
	ID            string         `yaml:"id"`
	When          map[string]any `yaml:"when"`
	Preconditions []string       `yaml:"preconditions"`
	Effects       map[string]any `yaml:"effects"`
}

// RulesSpec is the bundle of tunable world parameters from rules.yaml.
type RulesSpec struct {
	Lifecycle     LifecycleSpec `yaml:"lifecycle"`
	Family        FamilySpec    `yaml:"family"`
	Migration     MigrationSpec `yaml:"migration"`
	Economy       EconomySpec   `yaml:"economy"`
	Memory        MemorySpec    `yaml:"memory"`
	Relationships RelSpec       `yaml:"relationships"`
	Events        EventsSpec    `yaml:"events"`
	Tensions      []TensionSpec `yaml:"tensions"`
}

// LifecycleSpec governs aging and mortality.
type LifecycleSpec struct {
	AnnualDeathChance float64 `yaml:"annual_death_chance"`
	AgingYearTicks    int     `yaml:"aging_year_ticks"`
	AdultAge          int     `yaml:"adult_age"`
	FertileMinAge     int     `yaml:"fertile_min_age"`
	FertileMaxAge     int     `yaml:"fertile_max_age"`
	RecordDeathTick   bool    `yaml:"record_death_tick"`
}

// FamilySpec governs births and inheritance.
type FamilySpec struct {
	MinBirthIntervalTicks int         `yaml:"min_birth_interval_ticks"`
	MaxChildren           int         `yaml:"max_children"`
	Inheritance           InheritSpec `yaml:"inheritance"`
}

// InheritSpec defines inheritance priority order.
type InheritSpec struct {
	Priority []string `yaml:"priority"`
}

// MigrationSpec governs population pressure and movement.
type MigrationSpec struct {
	PressureCalculation  string  `yaml:"pressure_calculation"`
	FractionMovedPerTick float64 `yaml:"fraction_moved_per_tick"`
	MinMigrantsPerTick   int     `yaml:"min_migrants_per_tick"`
	DestinationStrategy  string  `yaml:"destination_strategy"`
}

// EconomySpec is the economy configuration.
type EconomySpec struct {
	Resources             []string                  `yaml:"resources"`
	Items                 []ItemSpec                `yaml:"items"`
	StartingCoinPerPerson int                       `yaml:"starting_coin_per_person"`
	InflationThreshold    float64                   `yaml:"inflation_threshold"`
	ProductionLoops       map[string]ProductionLoop `yaml:"production_loops"`
	Productivity          ProductivitySpec          `yaml:"productivity"`
}

// ItemSpec describes a single item in the worldpack's economy.
// Each ItemSpec becomes an entry in the world's item catalog
// (World.Items). Phase 18: the action engine's buy/sell handlers
// read the per-item Value from the catalog instead of the
// Phase 17.6 hardcoded priceList.
//
// Fields default to sensible values when omitted from the
// worldpack's rules.yaml: Weight defaults to the value in
// worldpack.DefaultItemSpec, Value defaults to 0 (free), and
// MaxDurability defaults to the default-table value. The
// name is the canonical lowercase item name (e.g. "bread").
type ItemSpec struct {
	Name          string  `yaml:"name"`
	Weight        float64 `yaml:"weight"`
	Value         int     `yaml:"value"`
	MaxDurability float64 `yaml:"max_durability"`
}

// ProductionLoop is one occupation's production recipe.
type ProductionLoop struct {
	Output   map[string]float64 `yaml:"output"`
	Requires map[string]float64 `yaml:"requires"`
}

// ProductivitySpec holds productivity multipliers.
type ProductivitySpec struct {
	ToolsMultiplierPer5 float64 `yaml:"tools_multiplier_per_5"`
}

// MemorySpec is the memory configuration.
type MemorySpec struct {
	ImportanceThreshold float64 `yaml:"importance_threshold"`
	DefaultDecayPerTick float64 `yaml:"default_decay_per_tick"`
	RecencyFloor        float64 `yaml:"recency_floor"`
	SchemaVersion       int     `yaml:"schema_version"`
}

// RelSpec is the relationship-axes configuration.
type RelSpec struct {
	Axes         []string `yaml:"axes"`
	Default      float64  `yaml:"default"`
	Min          float64  `yaml:"min"`
	Max          float64  `yaml:"max"`
	DecayPerTick float64  `yaml:"decay_per_tick"`
}

// EventsSpec is the event engine configuration.
type EventsSpec struct {
	FrequencyPerTick float64  `yaml:"frequency_per_tick"`
	Kinds            []string `yaml:"kinds"`
}

// TensionSpec is one initial tension seed.
type TensionSpec struct {
	ID                string   `yaml:"id"`
	Name              string   `yaml:"name"`
	Description       string   `yaml:"description"`
	Intensity         float64  `yaml:"intensity"`
	AffectedFactions  []string `yaml:"affected_factions"`
	AffectedLocations []string `yaml:"affected_locations"`
}

// GenerationSpec is the bundle of bootstrap parameters from generation.yaml.
type GenerationSpec struct {
	Population              PopulationSpec       `yaml:"population"`
	GenderRatio             GenderRatio          `yaml:"gender_ratio"`
	AgeDistribution         []AgeBracket         `yaml:"age_distribution"`
	LocationDistribution    []LocationBucket     `yaml:"location_distribution"`
	SocialClassDistribution []ClassBucket        `yaml:"social_class_distribution"`
	Traits                  map[string]TraitSpec `yaml:"traits"`
	Names                   NamePools            `yaml:"names"`
	Marriage                MarriageSpec         `yaml:"marriage"`
}

// PopulationSpec is the nested object under "population" in generation.yaml.
type PopulationSpec struct {
	Total int `yaml:"total"`
}

// GenderRatio is the female/male split.
type GenderRatio struct {
	Female float64 `yaml:"female"`
	Male   float64 `yaml:"male"`
}

// AgeBracket is one (range, share) pair for initial ages.
type AgeBracket struct {
	Range [2]int `yaml:"range"`
	Share float64 `yaml:"share"`
}

// LocationBucket assigns a number of NPCs to a location category.
type LocationBucket struct {
	Location string `yaml:"location"`
	Count    int    `yaml:"count"`
}

// ClassBucket assigns a share of NPCs to a social class.
type ClassBucket struct {
	Class string  `yaml:"class"`
	Share float64 `yaml:"share"`
}

// TraitSpec defines the sampling range and default for a trait.
type TraitSpec struct {
	Min     int `yaml:"min"`
	Max     int `yaml:"max"`
	Default int `yaml:"default"`
}

// NamePools holds the male, female, and surname name lists.
type NamePools struct {
	Male     []string `yaml:"male"`
	Female   []string `yaml:"female"`
	Surnames []string `yaml:"surnames"`
}

// MarriageSpec is the marriage-model parameters.
type MarriageSpec struct {
	SameClassPreference float64 `yaml:"same_class_preference"`
	MinAgeGap           int     `yaml:"min_age_gap"`
	MaxAgeGap           int     `yaml:"max_age_gap"`
}
