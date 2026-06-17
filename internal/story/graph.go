package story

import (
	"fmt"
	"sort"
)

// Graph is the canonical v2 StoryGraph: a map of StoryNode by ID.
//
// Phase 36.A wires Graph into engine.Runner. Phase 36.E
// (content/loader.go) populates a Graph from content/acts/*.yaml.
//
// Graph is keyed by ID only; node order is not preserved by
// insertion (use IDs() for a deterministic list). Engine traversal
// is graph-based: step reads WorldState.CurrentNodeID, follows the
// chosen Choice.NextNodeID to the next node.
type Graph struct {
	nodes map[string]StoryNode
}

// NewGraph returns an empty Graph with the underlying map
// initialised. NewGraph is the canonical way to construct a Graph
// in tests; production wiring hands the constructor the content
// loader's output in Phase 36.E.
func NewGraph() *Graph {
	return &Graph{nodes: make(map[string]StoryNode)}
}

// Add registers a StoryNode by ID.
//
// Add is fail-fast: an empty ID or a duplicate ID is a hard error,
// not silent overwrite. This mirrors the loader's contract (36.E):
// every content file is content-addressed and any load-time
// address collision is a hard error.
//
// Add returns the same error pattern that Go's `database/sql`
// package uses for "rows affected" — caller must check the error.
func (g *Graph) Add(n StoryNode) error {
	if n.ID == "" {
		return fmt.Errorf("storygraph: Add: node ID must not be empty")
	}
	if _, exists := g.nodes[n.ID]; exists {
		return fmt.Errorf("storygraph: Add: duplicate node ID %q", n.ID)
	}
	g.nodes[n.ID] = n
	return nil
}

// Lookup returns the StoryNode registered under id or a wrapped
// error if not found. Runner.Step propagates the error verbatim
// up to the CLI caller; broken NextNodeID references surface as
// hard errors (not silent divergence) per §18A invariant #3.
func (g *Graph) Lookup(id string) (StoryNode, error) {
	n, ok := g.nodes[id]
	if !ok {
		return StoryNode{}, fmt.Errorf("storygraph: Lookup: no node with ID %q", id)
	}
	return n, nil
}

// IDs returns the deterministic ID list of every node in the
// graph, sorted lexicographically. Used by content-audit tests
// (Phase 36.E loader.go validates every Choice.NextNodeID
// against this list at load time; §24 content test policy).
func (g *Graph) IDs() []string {
	out := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Len returns the number of nodes in the graph. Used by tests and
// by content-audit tooling.
func (g *Graph) Len() int {
	return len(g.nodes)
}
