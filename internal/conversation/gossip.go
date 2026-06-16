package conversation

import (
	"fmt"
	"strings"

	"github.com/chronicle-dev/chronicle/internal/core"
)

// GossipTopic represents something an NPC might gossip about.
type GossipTopic struct {
	Subject   string // who or what the gossip is about
	Relation  string // "about_person", "about_location", "about_event"
	Tone      string // "positive", "negative", "neutral", "concerned"
	Freshness int64  // tick when this became gossip-worthy
}

// BuildGossipContext builds a gossip context string from the world state
// for an NPC to reference during conversation. Returns recent events,
// relationships, and local happenings the NPC would know about.
func BuildGossipContext(w *core.World, npc *core.Person, playerID string) string {
	if w == nil || npc == nil {
		return ""
	}

	var b strings.Builder

	// What does this NPC know about the player?
	for _, r := range w.Relationships {
		if r.FromID == npc.ID && r.ToID == playerID {
			if r.Trust > 60 {
				b.WriteString("They regard you warmly.\n")
			} else if r.Trust < 30 {
				b.WriteString("They eye you with suspicion.\n")
			}
			break
		}
	}

	// Recent memories this NPC has
	memCount := 0
	for _, mem := range w.Memories {
		if mem.OwnerID == npc.ID && mem.Importance > 0.3 {
			if memCount >= 3 {
				break
			}
			b.WriteString(fmt.Sprintf("Recent memory: %s\n", mem.Description))
			memCount++
		}
	}

	// Who else is at this location?
	others := w.LivingPeopleAt(npc.LocationID)
	for _, other := range others {
		if other.ID == npc.ID || other.ID == playerID {
			continue
		}
		// Check if NPC has a relationship with this person
		for _, r := range w.Relationships {
			if r.FromID == npc.ID && r.ToID == other.ID {
				if r.Trust > 70 {
					b.WriteString(fmt.Sprintf("Close friend here: %s (%s)\n", other.Name, other.Occupation))
				} else if r.Fear > 60 {
					b.WriteString(fmt.Sprintf("Wary of: %s (%s)\n", other.Name, other.Occupation))
				} else if r.Attraction > 60 {
					b.WriteString(fmt.Sprintf("Fond of: %s (%s)\n", other.Name, other.Occupation))
				}
				break
			}
		}
	}

	// Local economy status
	if loc, ok := w.Locations[npc.LocationID]; ok {
		if loc.Settlement.Food < 20 {
			b.WriteString("Food stores are dangerously low.\n")
		}
		if loc.IsOvercrowded() {
			b.WriteString("The settlement feels overcrowded.\n")
		}
	}

	return b.String()
}

// BuildLocationGossip builds what an NPC might say about a specific
// location when asked. Includes knowledge of people there, conditions,
// and general reputation.
func BuildLocationGossip(w *core.World, npc *core.Person, locationID string) string {
	if w == nil || npc == nil {
		return ""
	}

	loc, ok := w.Locations[locationID]
	if !ok {
		return ""
	}

	var b strings.Builder
	people := w.LivingPeopleAt(locationID)

	if locationID == npc.LocationID {
		b.WriteString(fmt.Sprintf("You're already in %s. ", loc.Name))
	} else {
		b.WriteString(fmt.Sprintf("%s? ", loc.Name))
	}

	// How many people?
	switch {
	case len(people) > 20:
		b.WriteString("It's a busy place, full of folk going about their business. ")
	case len(people) > 5:
		b.WriteString("It's a decent-sized settlement with a handful of residents. ")
	default:
		b.WriteString("It's a quiet little place, not many souls there. ")
	}

	// Economy hint
	if loc.Settlement.Food < 30 {
		b.WriteString("I've heard they're struggling with food lately. ")
	}

	// Mention notable people
	notable := 0
	for _, p := range people {
		if p.Occupation == "mayor" || p.Occupation == "priest" || p.IsMerchant {
			if notable < 2 {
				b.WriteString(fmt.Sprintf("%s the %s is there, if you need anything. ", p.Name, p.Occupation))
				notable++
			}
		}
	}

	return b.String()
}

// BuildPersonGossip builds what an NPC might say about another person
// when asked. Includes their relationship, reputation, and local knowledge.
func BuildPersonGossip(w *core.World, npc *core.Person, target *core.Person) string {
	if w == nil || npc == nil || target == nil {
		return ""
	}

	var b strings.Builder

	// What's the NPC's relationship to the target?
	for _, r := range w.Relationships {
		if r.FromID == npc.ID && r.ToID == target.ID {
			switch {
			case r.Trust > 70 && r.Respect > 60:
				b.WriteString(fmt.Sprintf("Ah, %s? A fine person, one I trust completely. ", target.Name))
			case r.Trust > 50:
				b.WriteString(fmt.Sprintf("%s? Good folk, as far as I know. ", target.Name))
			case r.Trust < 30 && r.Fear > 50:
				b.WriteString(fmt.Sprintf("I'd watch yourself around %s, if I were you. ", target.Name))
			case r.Attraction > 60:
				b.WriteString(fmt.Sprintf("%s? *looks away* They're... impressive, I'll say that. ", target.Name))
			default:
				b.WriteString(fmt.Sprintf("I know %s, yes. ", target.Name))
			}
			break
		}
	}

	// Mention their occupation and location
	if target.LocationID == npc.LocationID {
		b.WriteString(fmt.Sprintf("They're a %s, lives right here. ", target.Occupation))
	} else if loc, ok := w.Locations[target.LocationID]; ok {
		b.WriteString(fmt.Sprintf("They're a %s, last I heard they were in %s. ", target.Occupation, loc.Name))
	}

	// Mention family
	if target.SpouseID != "" {
		if spouse, ok := w.People[target.SpouseID]; ok && spouse.Alive {
			b.WriteString(fmt.Sprintf("Married to %s. ", spouse.Name))
		}
	}

	return b.String()
}
