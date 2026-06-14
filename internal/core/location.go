package core

// Location is a settlement in the simulation.
//
// Phase 2 v1 fields per chronicle-spec.md §5.1:
//   - PopulationCap is a soft cap; population pressure rises when exceeded
//   - Pressure is a 0-100 scalar that modifies migration probability
type Location struct {
	ID            string
	Name          string
	Region        string
	Population    int
	PopulationCap int
	Pressure      int // 0..100, derived from Population vs PopulationCap
}

// NewLocation returns a Location with the given ID, name, region, and
// population cap. Population starts at 0 and Pressure at 0; both are
// recomputed by the PopulationEngine.
func NewLocation(id, name, region string, cap int) *Location {
	return &Location{
		ID:            id,
		Name:          name,
		Region:        region,
		Population:    0,
		PopulationCap: cap,
		Pressure:      0,
	}
}

// IsOvercrowded returns true if Population exceeds PopulationCap.
func (l *Location) IsOvercrowded() bool {
	return l.Population > l.PopulationCap
}
