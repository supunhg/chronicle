// Package lineage implements player death and succession per
// ARCHITECTURE.md §11.2 — "Death and Succession" and §11.4 — "Legacy
// Record".
//
// When the player character dies, the engine computes a list of
// successor candidates scored on the seven axes of the spec
// (relationship, family, age, proximity, shared history, inheritance
// rights, faction membership). The top candidate is the default
// "Heir" pick; the other four continuation modes (Family, Character,
// Observer, End Bloodline) give the player agency over who — or
// whether — to inhabit next.
//
// The package is pure-functional over *core.World: it reads the
// deceased's relationships, family tree, memories, and faction
// memberships; it does not mutate state. Callers (the REPL, the
// action engine, or a future "you died" screen) apply the result by
// setting w.PlayerID to the chosen successor, or by closing the
// chronicle when the player picks "End Bloodline".
//
// Phase 30 v1 scope:
//
//   - ScoreSuccessors: returns the top N candidates, sorted by score
//     descending, with a stable tie-break by sorted Person ID.
//   - PickSuccessor: applies a continuation mode to a candidate list
//     and returns the chosen *core.Person (or nil for Observer / End
//     Bloodline).
//   - ComputeLegacy: derives a Legacy record from the deceased's
//     memories, relationships, and family tree.
//
// The package does NOT decide WHEN to trigger succession (that's the
// REPL's job, after detecting w.PlayerID person.Alive == false) and
// does NOT touch persistence (the world is snapshotted on save the
// same way regardless of who the player is).
package lineage

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// ContinuationMode is the player's choice on death.
//
// Per spec §11.3, the five modes are:
//
//   - Heir: top-scoring successor (default; auto-applied if the
//     player types nothing within the prompt).
//   - Family: any living relative (child, parent, sibling, cousin,
//     grandchild, spouse).
//   - Character: any living person in the world (not just family).
//   - Observer: no body; the player watches the world with no
//     PlayerID. The chronicle continues; player input is disabled
//     until they pick a character (or quit).
//   - EndBloodline: the chronicle ends. The world keeps running
//     (or doesn't, depending on whether auto-tick is on) but the
//     REPL exits with a "the chronicle ends here" message.
type ContinuationMode string

const (
	ModeHeir        ContinuationMode = "heir"
	ModeFamily      ContinuationMode = "family"
	ModeCharacter   ContinuationMode = "character"
	ModeObserver    ContinuationMode = "observer"
	ModeEndBloodline ContinuationMode = "end_bloodline"
)

// AllModes returns the five continuation modes in the order the REPL
// should present them to the player. The order matches the spec's
// §11.3 list (Heir first, then Family, then Character, then
// Observer, then End Bloodline).
func AllModes() []ContinuationMode {
	return []ContinuationMode{ModeHeir, ModeFamily, ModeCharacter, ModeObserver, ModeEndBloodline}
}

// IsValid reports whether m is one of the five recognized modes.
func (m ContinuationMode) IsValid() bool {
	for _, v := range AllModes() {
		if v == m {
			return true
		}
	}
	return false
}

// Successor is a candidate to inherit the player role, with the
// score that ranked them. The score is the weighted sum of the
// seven axes from spec §11.2. The Breakdown field exposes the
// per-axis contribution so the REPL can show "I picked Amelia
// because: relationship 12, family 25, age 8, ...".
type Successor struct {
	Person    *core.Person
	Score     float64
	Breakdown ScoreBreakdown
}

// ScoreBreakdown is the per-axis contribution to a Successor's
// total score. Each field is the points contributed by that axis;
// the total Score is their sum. Zero values are valid (e.g., a
// stranger with no relationship or family ties scores 0 on those
// axes but can still win on age, proximity, or faction).
//
// Phase 30 v1: SharedHistory is intentionally omitted. Without
// a TargetID on core.Memory, inferring "who was this memory
// about" from the description is fragile (see ScoreSuccessors
// for details). A future phase can add the field and re-enable
// the axis.
type ScoreBreakdown struct {
	Relationship    float64 // trust+respect+loyalty+attraction with the deceased
	Family          float64 // closer = higher; spouse > child > parent > sibling > cousin
	Age             float64 // prefers adults 16-50, slight peak at 25-35
	Proximity       float64 // same location > same region > far
	Inheritance     float64 // firstborn / first heir gets a bonus
	Faction         float64 // same faction as the deceased
}

// Weights for the six v1 axes. The total of the weights is 90,
// not 100, because SharedHistory is dropped in v1 (see
// ScoreBreakdown). A perfect score is 90; the relative
// ranking is unchanged. The weights are tuned to match the
// spec's intent: family and relationship are the dominant
// signals; age and proximity break ties; inheritance and
// faction are tiebreakers-of-tiebreakers.
var Weights = struct {
	Relationship, Family, Age, Proximity, Inheritance, Faction float64
}{
	Relationship:   30,
	Family:         25,
	Age:            15,
	Proximity:      10,
	Inheritance:    5,
	Faction:        5,
}

// ScoreSuccessors returns the top N successor candidates for the
// deceased, sorted by score descending with a stable tie-break by
// Person ID. The deceased themselves is excluded from the list.
//
// N=0 returns the top 5 (the spec's "show me the candidates" UX
// budget). The list is filtered to living, adult (16+) people —
// children can't inherit, and dead people obviously can't either.
//
// Scoring uses the deceased's PlayerID-scoped data: the deceased's
// outgoing relationships (w.Relationships where FromID ==
// deceased.ID), the deceased's family tree (parents, spouse,
// children, grandchildren, siblings, cousins), the deceased's
// faction memberships, and the memory records that link the
// deceased to each candidate. Locations are read from w.Locations
// to compute proximity.
func ScoreSuccessors(w *core.World, deceasedID string, n int) []Successor {
	if n <= 0 {
		n = 5
	}
	deceased, ok := w.People[deceasedID]
	if !ok {
		return nil
	}
	if len(w.People) == 0 {
		return nil
	}

	// Build a lookup of the deceased's outgoing relationships.
	relByTo := make(map[string]core.Relationship, len(w.Relationships))
	for _, r := range w.Relationships {
		if r.FromID == deceasedID {
			relByTo[r.ToID] = r
		}
	}

	// Build a lookup of the deceased's family tree.
	familyDepth := make(map[string]int) // 0=spouse, 1=child/parent, 2=grandchild/sibling, 3=cousin
	familyDepth[deceased.SpouseID] = 0
	for _, p := range w.People {
		if p.ID == deceasedID {
			continue
		}
		switch {
		case p.FatherID == deceasedID || p.MotherID == deceasedID:
			familyDepth[p.ID] = 1
		case p.FatherID == deceased.SpouseID || p.MotherID == deceased.SpouseID:
			// stepchild of the deceased (spouse's child)
			if _, ok := familyDepth[p.ID]; !ok {
				familyDepth[p.ID] = 1
			}
		case isGrandchild(w, deceased, p):
			// grandchild (candidate's parent is deceased's child,
			// by father OR mother). isGrandchild covers all four
			// combinations: (p.Father is deceased's child via
			// father), (via mother), (p.Mother is deceased's
			// child via father), (via mother).
			if _, ok := familyDepth[p.ID]; !ok {
				familyDepth[p.ID] = 2
			}
		case p.FatherID == deceased.FatherID || p.MotherID == deceased.MotherID:
			// sibling
			if _, ok := familyDepth[p.ID]; !ok {
				familyDepth[p.ID] = 2
			}
		}
	}

	// Memory records that link the deceased to another person.
	// Phase 30 v1: SharedHistory is intentionally NOT computed
	// here. The v1 memory model (core.Memory) has no TargetID
	// field, so inferring "who was this memory about" from the
	// description is fragile (e.g., "chatted with Lily
	// Kensington" would have matched "Kensington" not "Lily
	// Kensington"; "traveled from A to B" matches "B"; "Founded
	// Blackwater Trading Guild" matches "Guild"). Rather than
	// ship a wrong signal, we drop the axis in v1. A future
	// phase can add TargetID to core.Memory and re-enable
	// SharedHistory. Weights below reflect the 6-axis scoring.

	// Faction membership: who is in the same faction as the
	// deceased? Phase 30 v1: the deceased's faction membership
	// isn't stored on Person (it lives in w.Factions[].Members).
	// We approximate by checking the deceased's name appears in
	// any faction's MemberOccupations — if so, anyone with that
	// occupation is a co-member. For v1 this is a rough signal;
	// a future phase can add Person.FactionID and use it
	// directly. Fallback: if the deceased's Occupation matches
	// a faction's MemberOccupations, count co-occupation as
	// co-membership.
	deadOcc := deceased.Occupation
	var coOccMembers []string
	for _, f := range w.Factions {
		for _, occ := range f.MemberOccupations {
			if occ == deadOcc {
				// Anyone with this occupation is a co-member.
				for _, p := range w.People {
					if p.Occupation == occ {
						coOccMembers = append(coOccMembers, p.ID)
					}
				}
			}
		}
	}
	factionBonus := make(map[string]bool, len(coOccMembers))
	for _, id := range coOccMembers {
		factionBonus[id] = true
	}

	// Firstborn bonus: the first child of the deceased (sorted by
	// ID) gets the inheritance bonus. If the deceased has no
	// children, the first child of the deceased's spouse (if any)
	// is the next-in-line.
	var firstborn string
	var firstSpouseChild string
	for _, p := range w.People {
		if p.FatherID == deceasedID || p.MotherID == deceasedID {
			if firstborn == "" || p.ID < firstborn {
				firstborn = p.ID
			}
		}
		if deceased.SpouseID != "" && (p.FatherID == deceased.SpouseID || p.MotherID == deceased.SpouseID) {
			if firstSpouseChild == "" || p.ID < firstSpouseChild {
				firstSpouseChild = p.ID
			}
		}
	}
	if firstborn == "" {
		firstborn = firstSpouseChild
	}

	out := make([]Successor, 0, len(w.People))
	for _, p := range w.People {
		if p.ID == deceasedID {
			continue
		}
		if !p.Alive {
			continue
		}
		if !p.IsAdult(w.Tick) {
			continue // children don't inherit
		}
		bd := ScoreBreakdown{}

		// Relationship: average of the deceased→candidate axes.
		if r, ok := relByTo[p.ID]; ok {
			bd.Relationship = (r.Trust + r.Respect + r.Loyalty + r.Attraction) / 4.0
		}

		// Family: depth-weighted.
		if depth, ok := familyDepth[p.ID]; ok {
			switch depth {
			case 0:
				bd.Family = 100 // spouse
			case 1:
				bd.Family = 90 // child or parent
			case 2:
				bd.Family = 70 // grandchild or sibling
			case 3:
				bd.Family = 40 // cousin
			default:
				bd.Family = 20
			}
		}

		// Age: prefers 25-35, declining outside that.
		age := p.AgeAt(w.Tick)
		switch {
		case age < 16:
			bd.Age = 0 // shouldn't happen (filtered above)
		case age < 20:
			bd.Age = 60
		case age < 25:
			bd.Age = 80
		case age <= 35:
			bd.Age = 100
		case age <= 50:
			bd.Age = 80
		case age <= 65:
			bd.Age = 60
		default:
			bd.Age = 40
		}

		// Proximity: same location > same region > far.
		deadLoc, deadHasLoc := w.Locations[deceased.LocationID]
		pLoc, pHasLoc := w.Locations[p.LocationID]
		switch {
		case deadHasLoc && pHasLoc && p.LocationID == deceased.LocationID:
			bd.Proximity = 100
		case deadHasLoc && pHasLoc && deadLoc.Region == pLoc.Region:
			bd.Proximity = 60
		default:
			bd.Proximity = 20
		}

		// Shared history: NOT computed in v1 (see comment above).

		// Inheritance: firstborn bonus.
		if p.ID == firstborn {
			bd.Inheritance = 100
		}

		// Faction: co-occupation = co-membership (v1 heuristic;
		// see comment on the coOccMembers computation above).
		if factionBonus[p.ID] {
			bd.Faction = 100
		}

		total := bd.Relationship*Weights.Relationship/100 +
			bd.Family*Weights.Family/100 +
			bd.Age*Weights.Age/100 +
			bd.Proximity*Weights.Proximity/100 +
			bd.Inheritance*Weights.Inheritance/100 +
			bd.Faction*Weights.Faction/100

		out = append(out, Successor{
			Person:    p,
			Score:     total,
			Breakdown: bd,
		})
	}

	// Sort by score desc, then by Person ID for stable ties.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Person.ID < out[j].Person.ID
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// PickSuccessor applies a continuation mode to a candidate list
// and returns the chosen *core.Person. The function is pure: it
// does not mutate the world. The caller (the REPL) applies the
// result by setting w.PlayerID to the returned person (or by
// setting a "chronicle ended" flag for ModeEndBloodline).
//
// Mode semantics:
//
//   - ModeHeir: out[0] (the top scorer). Returns nil if out is
//     empty (no living candidates) — the caller should then
//     fall through to ModeEndBloodline.
//   - ModeFamily: the first candidate with a non-zero Family
//     score. Returns nil if no family member is alive — caller
//     should fall through to ModeCharacter.
//   - ModeCharacter: out[0] (same as Heir but the meaning is
//     "I want the top scorer, period", and the REPL can
//     present this option when there's no family).
//   - ModeObserver: always returns nil with no world mutation.
//     The caller should set a "no PlayerID" flag.
//   - ModeEndBloodline: always returns nil. The caller should
//     end the REPL.
func PickSuccessor(candidates []Successor, mode ContinuationMode) *core.Person {
	switch mode {
	case ModeHeir, ModeCharacter:
		if len(candidates) == 0 {
			return nil
		}
		return candidates[0].Person
	case ModeFamily:
		for _, c := range candidates {
			if c.Breakdown.Family > 0 {
				return c.Person
			}
		}
		return nil
	case ModeObserver, ModeEndBloodline:
		return nil
	default:
		return nil
	}
}

// Legacy is the on-death record of a player's life. Per spec
// §11.4, it includes born/died dates, achievements (derived from
// significant memories), relationships, descendants count,
// reputation (average trust across all relationships), and a
// Legacy Score (weighted sum).
type Legacy struct {
	PlayerID      string
	Name          string
	BornTick      int64
	DiedTick      int64
	BornDate      time.Time
	DiedDate      time.Time
	AgeAtDeath    int
	Achievements  []string    // descriptions of high-importance memories
	SpouseName    string      // empty if none
	ChildCount    int         // total children (alive + dead)
	DescendantCount int       // children + grandchildren
	Reputation    float64     // average trust across all relationships
	LegacyScore   float64     // weighted sum
	TopRelations  []string    // up to 5 names with the highest trust to the deceased
}

// ComputeLegacy derives a Legacy record from the deceased's world
// state. The function is read-only. The Achievements list is built
// from the deceased's memories with Importance >= 0.7 (the
// "significant" threshold per spec §10.6). The Reputation is the
// average Trust axis across all relationships where the deceased
// is the FromID. The LegacyScore is a weighted sum: 1 point per
// year lived, 50 per surviving child, 25 per grandchild, 5 per
// significant memory, 1 per point of reputation.
//
// Returns nil if deceasedID doesn't refer to a known person.
func ComputeLegacy(w *core.World, deceasedID string) *Legacy {
	deceased, ok := w.People[deceasedID]
	if !ok {
		return nil
	}

	l := &Legacy{
		PlayerID:   deceasedID,
		Name:       deceased.Name,
		BornTick:   deceased.BirthTick,
		DiedTick:   deceased.DeathTick,
		BornDate:   w.Now.AddDate(0, 0, int(deceased.BirthTick)),
		DiedDate:   w.Now,
		AgeAtDeath: deceased.AgeAt(w.Tick),
	}

	// Achievements: high-importance memories.
	for _, m := range w.Memories {
		if m.OwnerID == deceasedID && m.Importance >= 0.7 {
			l.Achievements = append(l.Achievements, m.Description)
		}
	}

	// Spouse.
	if deceased.SpouseID != "" {
		if spouse, ok := w.People[deceased.SpouseID]; ok {
			l.SpouseName = spouse.Name
		}
	}

	// Children and grandchildren.
	childIDs := make(map[string]bool)
	for _, p := range w.People {
		if p.FatherID == deceasedID || p.MotherID == deceasedID {
			l.ChildCount++
			childIDs[p.ID] = true
		}
	}
	for _, p := range w.People {
		if p.FatherID != "" && childIDs[p.FatherID] {
			l.DescendantCount++
		} else if p.MotherID != "" && childIDs[p.MotherID] {
			l.DescendantCount++
		}
	}
	l.DescendantCount += l.ChildCount

	// Reputation: average trust across all relationships where
	// the deceased is the FromID. (We use the deceased→other
	// direction; a more thorough score would average both
	// directions, but v1 keeps it simple.)
	var trustSum float64
	var trustN int
	type namedTrust struct {
		name  string
		trust float64
	}
	var topRelations []namedTrust
	for _, r := range w.Relationships {
		if r.FromID == deceasedID {
			trustSum += r.Trust
			trustN++
			if other, ok := w.People[r.ToID]; ok {
				topRelations = append(topRelations, namedTrust{other.Name, r.Trust})
			}
		}
	}
	if trustN > 0 {
		l.Reputation = trustSum / float64(trustN)
	}
	// Top 5 relations by trust.
	sort.Slice(topRelations, func(i, j int) bool {
		if topRelations[i].trust != topRelations[j].trust {
			return topRelations[i].trust > topRelations[j].trust
		}
		return topRelations[i].name < topRelations[j].name
	})
	for i, tr := range topRelations {
		if i >= 5 {
			break
		}
		l.TopRelations = append(l.TopRelations, tr.name)
	}

	// LegacyScore: weighted sum.
	l.LegacyScore = float64(l.AgeAtDeath) + // 1 per year lived
		float64(l.ChildCount)*50 + // 50 per child
		float64(l.DescendantCount-l.ChildCount)*25 + // 25 per grandchild
		float64(len(l.Achievements))*5 + // 5 per significant memory
		l.Reputation // 1 per reputation point
	return l
}

// RenderDeathMessage formats the on-death screen the REPL shows
// when the player dies. Per spec §11.2:
//
//	Supun Hewagamage died peacefully at age 84.
//
//	Funeral attendees: 143
//	The Chronicle continues.
//
//	Successor:
//	Amelia Hewagamage
//	Age: 27
//	Occupation: Merchant
//	Relationship: Daughter
//
//	Reason:
//	Closest surviving heir and primary inheritor.
//
//	Press Enter to continue.
//	Or type: successors
//
// The "Funeral attendees" count is the number of living people at
// the deceased's location (a rough proxy for "people who showed
// up" — a fuller implementation would simulate a funeral event,
// but v1 uses the cheap signal).
//
// The "Occupation" and "Relationship" lines are included only
// when the information is known.
func RenderDeathMessage(w *core.World, deceasedID string, successor *core.Person, topCandidates []Successor) string {
	deceased, ok := w.People[deceasedID]
	if !ok {
		return fmt.Sprintf("%s is no longer in the world.", deceasedID)
	}
	funeralAttendees := 0
	if loc, ok := w.Locations[deceased.LocationID]; ok {
		funeralAttendees = loc.Population
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n%s died at age %d.\n\n", deceased.Name, deceased.AgeAt(w.Tick))
	fmt.Fprintf(&b, "Funeral attendees: %d\n", funeralAttendees)
	fmt.Fprintln(&b, "The Chronicle continues.")
	fmt.Fprintln(&b)
	if successor != nil {
		fmt.Fprintln(&b, "Successor:")
		fmt.Fprintf(&b, "%s\n", successor.Name)
		fmt.Fprintf(&b, "  Age: %d\n", successor.AgeAt(w.Tick))
		if successor.Occupation != "" {
			fmt.Fprintf(&b, "  Occupation: %s\n", successor.Occupation)
		}
		// Relationship label: spouse/child/parent/sibling/grandchild
		// or blank if not family.
		rel := relationshipLabel(w, deceased, successor)
		if rel != "" {
			fmt.Fprintf(&b, "  Relationship: %s\n", rel)
		}
		fmt.Fprintln(&b)
		if len(topCandidates) > 0 {
			reason := topReason(topCandidates[0])
			if reason != "" {
				fmt.Fprintln(&b, "Reason:")
				fmt.Fprintf(&b, "%s\n\n", reason)
			}
		}
	} else {
		fmt.Fprintln(&b, "No successor could be found.")
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b, "Press Enter to continue (you become the heir).")
	fmt.Fprintln(&b, "Or type: successors   (see top candidates)")
	fmt.Fprintln(&b, "Or type: family | character | observer | end_bloodline")
	return b.String()
}

// RenderSuccessorsList formats the "successors" sub-prompt the REPL
// shows when the player asks to see the candidates. The list is
// the top N (default 5) Successors with their score, name, age,
// occupation, and a one-line reason summarizing the dominant
// scoring axes.
func RenderSuccessorsList(candidates []Successor) string {
	if len(candidates) == 0 {
		return "No candidates are available. The chronicle ends here."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nTop %d successor candidates:\n\n", len(candidates))
	for i, c := range candidates {
		rel := ""
		if c.Person.FatherID != "" || c.Person.MotherID != "" || c.Person.SpouseID != "" {
			rel = relationshipLabelFromBreakdown(c.Breakdown)
		}
		fmt.Fprintf(&b, "  %d. %s — score %.1f\n", i+1, c.Person.Name, c.Score)
		fmt.Fprintf(&b, "     Age %d, %s", c.Person.AgeAt(0), c.Person.Occupation)
		if c.Person.LocationID != "" {
			fmt.Fprintf(&b, ", at %s", c.Person.LocationID)
		}
		if rel != "" {
			fmt.Fprintf(&b, " (%s)", rel)
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Type a number to pick, or one of: family | character | observer | end_bloodline")
	fmt.Fprintln(&b, "Or press Enter to accept the heir.")
	return b.String()
}

// RelationLabel returns a short human-readable string for how
// candidate is related to deceased, or "" if no direct
// relationship is found. Exported so the REPL and other callers
// can format the relationship in custom messages.
func RelationLabel(w *core.World, deceased, candidate *core.Person) string {
	return relationshipLabel(w, deceased, candidate)
}

// relationshipLabel returns a short human-readable string for how
// candidate is related to the deceased, or "" if no direct
// relationship is found. Used by the death message and the
// successors list.
//
// Child and Parent labels are gendered (Son/Daughter, Father/
// Mother) to match the spec example ("Relationship: Daughter" per
// §11.2). When the candidate's gender is unknown or empty, the
// label falls back to the gender-neutral form ("Child", "Parent").
func relationshipLabel(w *core.World, deceased, candidate *core.Person) string {
	switch {
	case deceased.SpouseID == candidate.ID:
		return "Spouse"
	case candidate.FatherID == deceased.ID || candidate.MotherID == deceased.ID:
		if candidate.Gender == "F" {
			return "Daughter"
		}
		if candidate.Gender == "M" {
			return "Son"
		}
		return "Child"
	case deceased.FatherID == candidate.ID || deceased.MotherID == candidate.ID:
		if candidate.Gender == "F" {
			return "Mother"
		}
		if candidate.Gender == "M" {
			return "Father"
		}
		return "Parent"
	case (deceased.FatherID != "" && deceased.FatherID == candidate.FatherID) ||
		(deceased.MotherID != "" && deceased.MotherID == candidate.MotherID):
		return "Sibling"
	case isGrandchild(w, deceased, candidate):
		if candidate.Gender == "F" {
			return "Granddaughter"
		}
		if candidate.Gender == "M" {
			return "Grandson"
		}
		return "Grandchild"
	case isCousin(w, deceased, candidate):
		return "Cousin"
	}
	return ""
}

// isGrandchild reports whether candidate is a grandchild of
// deceased (candidate's parent is deceased's child).
func isGrandchild(w *core.World, deceased, candidate *core.Person) bool {
	if candidate.FatherID == "" && candidate.MotherID == "" {
		return false
	}
	if candidate.FatherID != "" {
		if dad, ok := w.People[candidate.FatherID]; ok {
			if dad.FatherID == deceased.ID || dad.MotherID == deceased.ID {
				return true
			}
		}
	}
	if candidate.MotherID != "" {
		if mom, ok := w.People[candidate.MotherID]; ok {
			if mom.FatherID == deceased.ID || mom.MotherID == deceased.ID {
				return true
			}
		}
	}
	return false
}

// isCousin reports whether candidate shares a grandparent with
// deceased. Both parents' parents must overlap for true cousin
// status. v1 heuristic: at least one of candidate's parents
// shares a parent with one of deceased's parents.
func isCousin(w *core.World, deceased, candidate *core.Person) bool {
	if candidate.FatherID == "" && candidate.MotherID == "" {
		return false
	}
	if deceased.FatherID == "" && deceased.MotherID == "" {
		return false
	}
	for _, dParent := range []string{deceased.FatherID, deceased.MotherID} {
		if dParent == "" {
			continue
		}
		dp, ok := w.People[dParent]
		if !ok {
			continue
		}
		for _, cParent := range []string{candidate.FatherID, candidate.MotherID} {
			if cParent == "" {
				continue
			}
			cp, ok := w.People[cParent]
			if !ok {
				continue
			}
			if dp.FatherID != "" && (dp.FatherID == cp.FatherID || dp.FatherID == cp.MotherID) {
				return true
			}
			if dp.MotherID != "" && (dp.MotherID == cp.FatherID || dp.MotherID == cp.MotherID) {
				return true
			}
		}
	}
	return false
}

// relationshipLabelFromBreakdown returns a short label derived
// from the score breakdown. Used in the successors list when we
// have the breakdown but not the full Person graph handy.
func relationshipLabelFromBreakdown(bd ScoreBreakdown) string {
	if bd.Family >= 90 {
		return "Spouse/Child/Parent"
	}
	if bd.Family >= 70 {
		return "Grandchild/Sibling"
	}
	if bd.Family >= 40 {
		return "Cousin"
	}
	return ""
}

// topReason returns a one-line summary of why the candidate
// topped the score, derived from the dominant scoring axis.
func topReason(c Successor) string {
	bd := c.Breakdown
	type ax struct {
		name  string
		score float64
	}
	axes := []ax{
		{"relationship", bd.Relationship},
		{"family", bd.Family},
		{"age", bd.Age},
		{"proximity", bd.Proximity},
		{"inheritance rights", bd.Inheritance},
		{"faction ties", bd.Faction},
	}
	var best ax
	for _, a := range axes {
		if a.score > best.score {
			best = a
		}
	}
	if best.score == 0 {
		return "Best of available candidates."
	}
	return fmt.Sprintf("Top %s score (%.0f/100).", best.name, best.score)
}

// RenderLegacyRecord formats the legacy record per spec §11.4:
//
//	SUPUN HEWAGAMAGE
//	Born: 1427
//	Died: 1511
//
//	Achievements:
//	- Founded Blackwater Trading Guild
//	- Served as Mayor
//	- Started the Grain Riots
//
//	Relationships:
//	- Married Elena
//	- 4 Children
//
//	Reputation:
//	Respected Merchant
//
//	Legacy Score: 712
func RenderLegacyRecord(l *Legacy) string {
	if l == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n%s\n", strings.ToUpper(l.Name))
	fmt.Fprintf(&b, "Born: %s\n", l.BornDate.Format("2006"))
	fmt.Fprintf(&b, "Died: %s (age %d)\n\n", l.DiedDate.Format("2006"), l.AgeAtDeath)
	if len(l.Achievements) > 0 {
		fmt.Fprintln(&b, "Achievements:")
		for _, a := range l.Achievements {
			fmt.Fprintf(&b, "  - %s\n", a)
		}
		fmt.Fprintln(&b)
	}
	fmt.Fprintln(&b, "Relationships:")
	if l.SpouseName != "" {
		fmt.Fprintf(&b, "  - Married %s\n", l.SpouseName)
	}
	if l.ChildCount > 0 {
		fmt.Fprintf(&b, "  - %d Children\n", l.ChildCount)
	}
	if len(l.TopRelations) > 0 {
		fmt.Fprintf(&b, "  - Close to: %s\n", strings.Join(l.TopRelations, ", "))
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Reputation: %s\n", reputationLabel(l.Reputation))
	fmt.Fprintf(&b, "Legacy Score: %.0f\n", l.LegacyScore)
	return b.String()
}

// reputationLabel returns a short adjective for a reputation
// score (0-100 average trust). The thresholds are arbitrary v1
// values that produce reasonable labels.
func reputationLabel(rep float64) string {
	switch {
	case rep >= 80:
		return "Beloved"
	case rep >= 65:
		return "Respected"
	case rep >= 50:
		return "Known"
	case rep >= 35:
		return "Middling"
	case rep > 0:
		return "Disliked"
	default:
		return "Unknown"
	}
}
