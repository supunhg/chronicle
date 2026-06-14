package core

// Relationship is a directed pair of (from, to) with five axes per
// chronicle-spec.md §5.2.
//
// The five axes are stored as float64s. The historical event log that
// produced the current values lives in the memories table; Relationship
// is the cached aggregate.
type Relationship struct {
	FromID     string
	ToID       string
	Trust      float64 // 0..100
	Respect    float64
	Fear       float64
	Attraction float64
	Loyalty    float64
}

// Key returns a stable string key for indexing relationships in a map.
func (r *Relationship) Key() string {
	return r.FromID + "|" + r.ToID
}
