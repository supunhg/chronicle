package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chronicle-dev/chronicle/internal/content"
	"github.com/chronicle-dev/chronicle/internal/endings"
	"github.com/chronicle-dev/chronicle/internal/engine"
	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
	"github.com/chronicle-dev/chronicle/internal/ui"
)

// stdinChoiceProvider wraps TTYRenderer.PromptChoice into the
// engine.ChoiceProvider interface. It maps the 1-based index
// returned by PromptChoice to the corresponding available choice.
type stdinChoiceProvider struct {
	renderer *ui.TTYRenderer
}

func (s *stdinChoiceProvider) Select(node story.StoryNode, available []story.Choice, ws state.WorldState) (story.Choice, error) {
	idx, err := s.renderer.PromptChoice(node, available, ws)
	if err != nil {
		return story.Choice{}, err
	}
	return available[idx-1], nil
}

// scriptedFileChoiceProvider reads choice IDs from a file (one
// per line) and matches them against available choices by ID.
// Used for §40.B scripted manual playthroughs.
type scriptedFileChoiceProvider struct {
	script []string
	i      int
}

func (s *scriptedFileChoiceProvider) Select(node story.StoryNode, available []story.Choice, ws state.WorldState) (story.Choice, error) {
	if s.i >= len(s.script) {
		return story.Choice{}, fmt.Errorf("scriptedFileChoiceProvider: script exhausted at node %q (step %d)", node.ID, s.i)
	}
	targetID := s.script[s.i]
	s.i++
	for _, c := range available {
		if c.ID == targetID {
			return c, nil
		}
	}
	availIDs := make([]string, len(available))
	for i, c := range available {
		availIDs[i] = c.ID
	}
	return story.Choice{}, fmt.Errorf("scriptedFileChoiceProvider: at node %q choice %q not in available [%s]", node.ID, targetID, strings.Join(availIDs, ", "))
}

// runPlay implements the `play` subcommand per §40.B manual
// playthrough. Loads the worldpack, shows protagonist selection,
// initialises the save state, and runs the §23 Runtime Flow
// until a finale node (no choices) is reached.
func runPlay(argv []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	fs.SetOutput(stderr)
	protagonistFlag := fs.String("protagonist", "", "protagonist name (Kael, Lyra, Raven, Aria)")
	worldpackFlag := fs.String("worldpack", "worldpacks/frontier", "path to worldpack directory")
	scriptFlag := fs.String("script", "", "path to a file containing one choice ID per line for scripted playthrough")
	if err := fs.Parse(argv); err != nil {
		return err
	}

	loaded, err := content.Load(*worldpackFlag)
	if err != nil {
		return fmt.Errorf("play: load worldpack: %w", err)
	}

	if len(loaded.Protagonists) == 0 {
		return fmt.Errorf("play: worldpack %q has no protagonists", *worldpackFlag)
	}

	var protag content.Protagonist
	if *protagonistFlag != "" {
		found := false
		for _, p := range loaded.Protagonists {
			if p.Name == *protagonistFlag {
				protag = p
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("play: protagonist %q not found", *protagonistFlag)
		}
	} else {
		fmt.Fprintln(stdout, "Choose a protagonist:")
		for i, p := range loaded.Protagonists {
			fmt.Fprintf(stdout, "  [%d] %s\n", i+1, p.Name)
		}
		tty := ui.NewTTYRendererWithIO(stdout, os.Stdin)
		avail := make([]story.Choice, len(loaded.Protagonists))
		for i, p := range loaded.Protagonists {
			avail[i] = story.Choice{ID: p.Name, Text: p.Name}
		}
		pick, err := tty.PromptChoice(story.StoryNode{}, avail, state.NewWorldState())
		if err != nil {
			return fmt.Errorf("play: read protagonist selection: %w", err)
		}
		protag = loaded.Protagonists[pick-1]
	}

	ws := state.NewWorldState()
	if err := initProtagonistStatePlay(&ws, protag); err != nil {
		return fmt.Errorf("play: init protagonist: %w", err)
	}
	sg := state.SaveGame{Version: state.CurrentVersion, WorldState: ws}

	var cp engine.ChoiceProvider
	tty := ui.NewTTYRendererWithIO(stdout, os.Stdin)
	tty.AnsiEnabled = false // clean, deterministic output for pipe/redirect

	if *scriptFlag != "" {
		lines, err := readScriptFile(*scriptFlag)
		if err != nil {
			return fmt.Errorf("play: read script: %w", err)
		}
		cp = &scriptedFileChoiceProvider{script: lines}
	} else {
		cp = &stdinChoiceProvider{renderer: tty}
	}

	eng := &engine.Engine{
		Renderer:       tty,
		ChoiceProvider: cp,
		Endings:        loaded.Endings,
		Events:         loaded.Events,
		// OnFinale is intentionally nil; finale surfacing happens
		// exactly once in the Step loop below (avoids duplicate
		// print from Step's maybeSurfaceEndings + the loop).
	}

	runner := &engine.Runner{
		Graph:  loaded.Graph,
		Engine: eng,
	}

	for {
		node, lookupErr := loaded.Graph.Lookup(sg.WorldState.CurrentNodeID)
		if lookupErr != nil {
			return fmt.Errorf("play: lookup node %q: %w", sg.WorldState.CurrentNodeID, lookupErr)
		}

		// Finale detection: a node with no choices is the end
		// of the authored graph. Surface the highest-priority
		// ending (if any) and stop.
		if len(node.Choices) == 0 {
			if e, ok := endings.Evaluate(sg.WorldState, loaded.Endings); ok {
				fmt.Fprintf(stdout, "\n*** ENDING: %s (priority %d) ***\n", e.ID, e.Priority)
			} else {
				fmt.Fprintln(stdout, "\n[Fin — no ending matched]")
			}
			fmt.Fprintln(stdout, "\n[Fin]")
			break
		}

		var stepErr error
		sg, stepErr = runner.Step(sg)
		if stepErr != nil {
			return fmt.Errorf("play: step error at %q: %w", sg.WorldState.CurrentNodeID, stepErr)
		}
	}

	return nil
}

// readScriptFile reads a file of choice IDs, one per line.
// Blank lines and lines starting with # are ignored.
func readScriptFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// initProtagonistStatePlay applies p's starting_* fields to ws,
// mirroring the test helper in internal/content/reachable_test.go.
func initProtagonistStatePlay(ws *state.WorldState, p content.Protagonist) error {
	if ws.Flags == nil {
		ws.Flags = make(map[string]bool)
	}
	if ws.Variables == nil {
		ws.Variables = make(map[string]int)
	}
	if ws.Relationships == nil {
		ws.Relationships = make(map[string]state.Relationship)
	}
	if ws.Inventory.Items == nil {
		ws.Inventory.Items = make(map[string]int)
	}
	ws.Protagonist = p.Name
	for _, f := range p.StartingFlags {
		ws.Flags[f] = true
	}
	for k, v := range p.StartingVariables {
		ws.Variables[k] = v
	}
	for _, item := range p.StartingInventory {
		ws.Inventory.Items[item]++
	}
	if len(p.ExclusiveNodes) == 0 {
		return fmt.Errorf("protagonist %q has no exclusive_nodes", p.Name)
	}
	ws.CurrentNodeID = p.ExclusiveNodes[0]
	return nil
}
