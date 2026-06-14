package core

// Faction is a political or social group in the world, per
// chronicle-spec.md §2.3.
//
// Members are derived from Person.Occupation: a person is a member of
// faction F if Person.Occupation is in F.MemberOccupations. The World
// stores the faction definitions; the Faction engine (Phase 5+)
// computes membership.
type Faction struct {
	ID string

	// Name is the display name.
	Name string

	// Goal is a one-line statement of the faction's purpose.
	Goal string

	// Color is a theme hint for UI rendering (e.g. "gold", "blue").
	Color string

	// BaseLocation is the ID of the faction's home location, if any.
	BaseLocation string

	// Rivals and Allies are faction IDs.
	Rivals []string
	Allies []string

	// MemberOccupations is the set of occupation IDs whose holders
	// belong to this faction. The list comes from the worldpack YAML.
	MemberOccupations []string
}
