// Package conversation manages multi-turn NPC dialogue for Chronicle's
// immersive text adventure mode. When the player types "talk <npc>",
// the REPL enters conversation mode. All subsequent input goes through
// the Manager until the player exits (bye, leave, stop, goodbye).
//
// The Manager builds rich context for each LLM call: NPC identity,
// personality traits, relationship with the player, relevant memories,
// family ties, current location, and the full conversation history.
// This context is what makes NPCs feel like real people — they reference
// their past experiences, adjust their tone based on trust/fear, and
// stay in character throughout.
//
// When the LLM is unavailable, the Manager falls back to simple
// template responses so the game is still playable.
package conversation

import (
	"context"
	"fmt"
	"strings"

	"github.com/chronicle-dev/chronicle/internal/core"
	"github.com/chronicle-dev/chronicle/internal/llm"
)

// LLMClient is the subset of *llm.Client that the conversation
// manager uses. Matches the narrator.LLMClient interface so the
// same client instance can be shared.
type LLMClient interface {
	Chat(ctx context.Context, messages []llm.ChatMessage) (string, error)
}

// Exchange is one turn in a conversation.
type Exchange struct {
	// Speaker is "player" or the NPC's name.
	Speaker string
	// Text is what was said.
	Text string
}

// State tracks an active conversation with an NPC.
type State struct {
	// NPCID is the person being talked to.
	NPCID string
	// NPCName is cached for display.
	NPCName string
	// History is the conversation so far.
	History []Exchange
	// Active is true while the conversation is ongoing.
	Active bool
}

// Manager handles NPC conversations. It is NOT safe for concurrent
// use — the REPL is single-threaded.
type Manager struct {
	llm     LLMClient
	world   *core.World
	current *State
}

// New creates a conversation Manager. The LLM client may be nil
// for template-only mode.
func New(llmClient LLMClient, w *core.World) *Manager {
	return &Manager{llm: llmClient, world: w}
}

// Start begins a conversation with an NPC. Returns the NPC's
// greeting. If a conversation is already active, it is ended first.
func (m *Manager) Start(ctx context.Context, npc *core.Person) string {
	if m.current != nil && m.current.Active {
		m.current = nil
	}
	m.current = &State{
		NPCID:   npc.ID,
		NPCName: npc.Name,
		History: []Exchange{},
		Active:  true,
	}
	return m.generateNPCResponse(ctx, "")
}

// Continue processes a player message and returns the NPC's response.
// Returns a message if no conversation is active. Re-checks that
// the NPC is still alive (a tick may have killed them).
func (m *Manager) Continue(ctx context.Context, playerMsg string) string {
	if m.current == nil || !m.current.Active {
		return ""
	}
	// Re-check NPC alive status
	if npc, ok := m.world.People[m.current.NPCID]; !ok || !npc.Alive {
		m.current = nil
		return "Your conversation partner is no longer here."
	}
	m.current.History = append(m.current.History, Exchange{
		Speaker: "player",
		Text:    playerMsg,
	})
	return m.generateNPCResponse(ctx, playerMsg)
}

// End terminates the current conversation and returns a farewell message.
// Returns "" if no conversation is active.
func (m *Manager) End() string {
	if m.current == nil || !m.current.Active {
		return ""
	}
	name := m.current.NPCName
	m.current = nil
	return fmt.Sprintf("You take your leave of %s.", name)
}

// IsActive reports whether a conversation is in progress.
func (m *Manager) IsActive() bool {
	return m.current != nil && m.current.Active
}

// CurrentNPC returns the NPC being talked to, or nil.
func (m *Manager) CurrentNPC() *core.Person {
	if m.current == nil || !m.current.Active {
		return nil
	}
	p, ok := m.world.People[m.current.NPCID]
	if !ok || !p.Alive {
		return nil
	}
	return p
}

// CurrentNPCName returns the name of the NPC being talked to.
func (m *Manager) CurrentNPCName() string {
	if m.current == nil {
		return ""
	}
	return m.current.NPCName
}

// ShouldExit checks if the player's message is a conversation exit command.
func ShouldExit(msg string) bool {
	switch strings.ToLower(strings.TrimSpace(msg)) {
	case "bye", "goodbye", "leave", "stop", "exit", "quit", "farewell", "done":
		return true
	}
	return false
}

// generateNPCResponse builds the LLM prompt and gets a response.
func (m *Manager) generateNPCResponse(ctx context.Context, playerMsg string) string {
	npc, ok := m.world.People[m.current.NPCID]
	if !ok || !npc.Alive {
		return "They seem to have wandered off."
	}

	if m.llm == nil {
		return m.fallbackResponse(playerMsg)
	}

	messages := []llm.ChatMessage{
		{Role: "system", Content: m.buildSystemPrompt(npc)},
		{Role: "user", Content: m.buildUserPrompt(npc, playerMsg)},
	}

	resp, err := m.llm.Chat(ctx, messages)
	if err != nil || strings.TrimSpace(resp) == "" {
		return m.fallbackResponse(playerMsg)
	}

	// Record the NPC's response.
	m.current.History = append(m.current.History, Exchange{
		Speaker: npc.Name,
		Text:    resp,
	})

	return resp
}

// buildSystemPrompt creates the system prompt that defines the NPC's
// personality, knowledge, and behavioral rules. This is where the
// simulation data becomes the game — relationship scores, memories,
// traits, and family ties all shape how the NPC speaks.
func (m *Manager) buildSystemPrompt(npc *core.Person) string {
	var b strings.Builder

	b.WriteString("You are a character in a medieval frontier world called The Free Marches. ")
	b.WriteString("You are NOT an AI assistant. You are a living person in this world with your own desires, fears, and memories. ")
	b.WriteString("Stay in character at all times. Speak naturally and conversationally, as a real person would. ")
	b.WriteString("Use first person. Keep responses to 1-4 sentences unless the conversation warrants more detail. ")
	b.WriteString("Reference your memories and experiences naturally when relevant. ")
	b.WriteString("Express emotion through your words and tone. You may gossip, complain, boast, ask questions, or change the subject. ")
	b.WriteString("Never break character. Never mention being an AI, language model, or simulation.\n\n")

	// NPC identity
	b.WriteString("== YOUR IDENTITY ==\n")
	fmt.Fprintf(&b, "Name: %s\n", npc.Name)
	fmt.Fprintf(&b, "Gender: %s\n", npc.Gender)
	fmt.Fprintf(&b, "Age: %d years old\n", npc.AgeAt(m.world.Tick))
	if npc.Occupation != "" {
		fmt.Fprintf(&b, "Occupation: %s\n", npc.Occupation)
	}
	if npc.Class != "" {
		fmt.Fprintf(&b, "Social Class: %s\n", npc.Class)
	}

	// Personality traits
	if len(npc.Traits) > 0 {
		b.WriteString("Personality: ")
		traits := make([]string, 0)
		for trait, val := range npc.Traits {
			switch {
			case val > 80:
				traits = append(traits, "extremely "+trait)
			case val > 60:
				traits = append(traits, "quite "+trait)
			case val < 20:
				traits = append(traits, "not at all "+trait)
			case val < 40:
				traits = append(traits, "somewhat lacking in "+trait)
			}
		}
		if len(traits) > 0 {
			b.WriteString(strings.Join(traits, ", "))
		} else {
			b.WriteString("moderate in most regards")
		}
		b.WriteString(".\n")
	}

	// Family
	if npc.SpouseID != "" {
		if spouse, ok := m.world.People[npc.SpouseID]; ok {
			status := "alive"
			if !spouse.Alive {
				status = "deceased"
			}
			fmt.Fprintf(&b, "Spouse: %s (%s)\n", spouse.Name, status)
		}
	}
	var children []string
	for _, p := range m.world.People {
		if p.FatherID == npc.ID || p.MotherID == npc.ID {
			status := ""
			if !p.Alive {
				status = ", deceased"
			}
			children = append(children, fmt.Sprintf("%s (age %d%s)", p.Name, p.AgeAt(m.world.Tick), status))
		}
	}
	if len(children) > 0 {
		fmt.Fprintf(&b, "Children: %s\n", strings.Join(children, ", "))
	}

	// Relationship with player
	if m.world.PlayerID != "" {
		player := m.world.People[m.world.PlayerID]
		if player != nil {
			rel := m.findRelationship(npc.ID, m.world.PlayerID)
			relRev := m.findRelationship(m.world.PlayerID, npc.ID)
			fmt.Fprintf(&b, "\n== YOUR RELATIONSHIP WITH %s ==\n", strings.ToUpper(player.Name))
			if rel != nil {
				describeRelationship(&b, rel)
			} else {
				b.WriteString("You don't know this person well.\n")
			}
			if relRev != nil && relRev != rel {
				b.WriteString("(How they feel about you is separate from how you feel about them.)\n")
			}
		}
	}

	// Memories involving the player
	var playerMemories []core.Memory
	if m.world.PlayerID != "" {
		for _, mem := range m.world.Memories {
			if mem.OwnerID == npc.ID {
				// Include memories that mention the player or are about interactions
				if strings.Contains(mem.Description, m.world.PlayerID) ||
					strings.Contains(strings.ToLower(mem.Description), "talk") ||
					strings.Contains(strings.ToLower(mem.Description), "chat") ||
					mem.Importance >= 0.5 {
					playerMemories = append(playerMemories, mem)
				}
			}
		}
	}
	if len(playerMemories) > 0 {
		b.WriteString("\n== YOUR MEMORIES ==\n")
		start := 0
		if len(playerMemories) > 8 {
			start = len(playerMemories) - 8
		}
		for _, mem := range playerMemories[start:] {
			fmt.Fprintf(&b, "- %s\n", mem.Description)
		}
	}

	// Current needs/mood
	if len(npc.Needs) > 0 {
		b.WriteString("\n== YOUR CURRENT STATE ==\n")
		for need, val := range npc.Needs {
			mood := ""
			switch {
			case val < 20:
				mood = "desperate"
			case val < 40:
				mood = "struggling"
			case val < 60:
				mood = "managing"
			case val < 80:
				mood = "doing well"
			default:
				mood = "thriving"
			}
			fmt.Fprintf(&b, "%s: %s (%d/100)\n", need, mood, val)
		}
	}

	// Current location context
	if loc, ok := m.world.Locations[npc.LocationID]; ok {
		fmt.Fprintf(&b, "\nYou are currently in %s.\n", loc.Name)
	}

	// Gossip context — what this NPC knows and would share
	gossipCtx := BuildGossipContext(m.world, npc, m.world.PlayerID)
	if gossipCtx != "" {
		b.WriteString("\n== WHAT YOU KNOW ==\n")
		b.WriteString(gossipCtx)
	}

	b.WriteString("\nRespond ONLY as your character. Do not include stage directions, asterisks, or narrator commentary. Just speak naturally. " +
		"You may share gossip, ask questions, boast, complain, or talk about other people and places you know.\n")

	return b.String()
}

// buildUserPrompt creates the user message including conversation history.
func (m *Manager) buildUserPrompt(npc *core.Person, playerMsg string) string {
	var b strings.Builder

	playerName := "A traveler"
	if m.world.PlayerID != "" {
		if player, ok := m.world.People[m.world.PlayerID]; ok {
			playerName = player.Name
		}
	}

	// Include conversation history
	if len(m.current.History) > 0 {
		b.WriteString("Conversation so far:\n")
		for _, ex := range m.current.History {
			fmt.Fprintf(&b, "%s: %s\n", ex.Speaker, ex.Text)
		}
		b.WriteString("\n")
	}

	if playerMsg == "" {
		// First message — the NPC greets the player
		fmt.Fprintf(&b, "%s has approached you to talk. Greet them naturally, in character. You may comment on the time of day, the weather, or something on your mind.", playerName)
	} else {
		fmt.Fprintf(&b, "%s just said to you: \"%s\"\n\nRespond naturally in character. You can answer their question, ask something back, share gossip, change the subject, or react emotionally — whatever feels right for your character.", playerName, playerMsg)
	}

	return b.String()
}

// fallbackResponse returns a template response when the LLM is unavailable.
func (m *Manager) fallbackResponse(playerMsg string) string {
	npc, ok := m.world.People[m.current.NPCID]
	if !ok {
		return "They seem to have wandered off."
	}

	if playerMsg == "" {
		// Greeting
		greetings := []string{
			fmt.Sprintf("%s nods at you. \"Good day. What brings you here?\"", npc.Name),
			fmt.Sprintf("%s looks up as you approach. \"Ah, hello there.\"", npc.Name),
			fmt.Sprintf("%s acknowledges you with a slight bow. \"Can I help you?\"", npc.Name),
		}
		// Pick based on tick for determinism
		idx := int(m.world.Tick) % len(greetings)
		resp := greetings[idx]
		m.current.History = append(m.current.History, Exchange{Speaker: npc.Name, Text: resp})
		return resp
	}

	// Check relationship for tone
	rel := m.findRelationship(npc.ID, m.world.PlayerID)
	tone := "neutral"
	if rel != nil {
		if rel.Trust > 70 {
			tone = "friendly"
		} else if rel.Trust < 30 {
			tone = "wary"
		}
	}

	var resp string
	switch tone {
	case "friendly":
		resp = fmt.Sprintf("%s smiles warmly. \"That's an interesting thought. Tell me more.\"", npc.Name)
	case "wary":
		resp = fmt.Sprintf("%s eyes you cautiously. \"Hmm. I'll think on that.\"", npc.Name)
	default:
		resp = fmt.Sprintf("%s considers what you said. \"I see. Is there anything else?\"", npc.Name)
	}

	m.current.History = append(m.current.History, Exchange{Speaker: npc.Name, Text: resp})
	return resp
}

// findRelationship finds a directed relationship.
func (m *Manager) findRelationship(fromID, toID string) *core.Relationship {
	for i := range m.world.Relationships {
		r := &m.world.Relationships[i]
		if r.FromID == fromID && r.ToID == toID {
			return r
		}
	}
	return nil
}

// describeRelationship writes a human-readable description of a
// relationship's axes to the string builder.
func describeRelationship(b *strings.Builder, rel *core.Relationship) {
	trust := level(rel.Trust)
	respect := level(rel.Respect)
	fear := level(rel.Fear)
	attraction := level(rel.Attraction)
	loyalty := level(rel.Loyalty)

	fmt.Fprintf(b, "Trust: %s (%.0f/100)\n", trust, rel.Trust)
	fmt.Fprintf(b, "Respect: %s (%.0f/100)\n", respect, rel.Respect)
	fmt.Fprintf(b, "Fear: %s (%.0f/100)\n", fear, rel.Fear)
	fmt.Fprintf(b, "Attraction: %s (%.0f/100)\n", attraction, rel.Attraction)
	fmt.Fprintf(b, "Loyalty: %s (%.0f/100)\n", loyalty, rel.Loyalty)

	b.WriteString("Adjust your tone and openness based on these values. ")
	if rel.Trust > 70 {
		b.WriteString("You trust this person and are comfortable around them. ")
	} else if rel.Trust < 30 {
		b.WriteString("You are wary of this person and guard your words. ")
	}
	if rel.Fear > 60 {
		b.WriteString("You are somewhat afraid of them. ")
	}
	if rel.Attraction > 70 {
		b.WriteString("You find them attractive and are a bit flustered. ")
	}
	b.WriteString("\n")
}

// level returns a word description for a 0-100 score.
func level(v float64) string {
	switch {
	case v >= 90:
		return "extreme"
	case v >= 75:
		return "high"
	case v >= 60:
		return "moderate"
	case v >= 40:
		return "neutral"
	case v >= 25:
		return "low"
	default:
		return "very low"
	}
}
