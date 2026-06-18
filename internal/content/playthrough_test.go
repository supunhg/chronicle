package content

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chronicle-dev/chronicle/internal/endings"
	"github.com/chronicle-dev/chronicle/internal/engine"
	"github.com/chronicle-dev/chronicle/internal/state"
	"github.com/chronicle-dev/chronicle/internal/story"
	"github.com/chronicle-dev/chronicle/internal/ui"
)

// TestPlaythroughScriptedSpine is the §40.B regression test.
// It walks each protagonist's committed scripted spine
// (playthroughs/<name>.txt) through the real engine.Runner.Step
// (not the CLI) and asserts that the walk reaches act3.the_end
// with the target ending at highest priority.
//
// This test guards against graph regressions (broken
// NextNodeIDs), event regressions (redirects changed or
// conditions drifted), and content regressions (choices
// removed or renamed).
func TestPlaythroughScriptedSpine(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	dir := filepath.Join(wd, "..", "..", "worldpacks", "frontier")
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		name    string
		script  string
		wantEnd string
	}{
		{"Kael", filepath.Join(wd, "..", "..", "playthroughs", "kael.txt"), "hero"},
		{"Lyra", filepath.Join(wd, "..", "..", "playthroughs", "lyra.txt"), "hero"},
		{"Raven", filepath.Join(wd, "..", "..", "playthroughs", "raven.txt"), "hero"},
		{"Aria", filepath.Join(wd, "..", "..", "playthroughs", "aria.txt"), "hero"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var protag Protagonist
			for _, p := range loaded.Protagonists {
				if p.Name == tc.name {
					protag = p
					break
				}
			}
			if protag.Name == "" {
				t.Fatalf("protagonist %q not found in loaded worldpack", tc.name)
			}

			ids, err := readChoiceScript(tc.script)
			if err != nil {
				t.Fatalf("read script %s: %v", tc.script, err)
			}

			// Build scripted choices for engine.NewScripted.
			scripted := make([]story.Choice, len(ids))
			for i, id := range ids {
				scripted[i] = story.Choice{ID: id}
			}

			ws := state.NewWorldState()
			if err := initProtagonistState(&ws, protag); err != nil {
				t.Fatalf("initProtagonistState: %v", err)
			}
			sg := state.SaveGame{Version: state.CurrentVersion, WorldState: ws}

			eng := &engine.Engine{
				Renderer:       ui.NewBufferRenderer(io.Discard),
				ChoiceProvider: engine.NewScripted(scripted),
				Endings:        loaded.Endings,
				Events:         loaded.Events,
			}
			runner := &engine.Runner{
				Graph:  loaded.Graph,
				Engine: eng,
			}

			for {
				node, lookupErr := loaded.Graph.Lookup(sg.WorldState.CurrentNodeID)
				if lookupErr != nil {
					t.Fatalf("lookup node %q: %v", sg.WorldState.CurrentNodeID, lookupErr)
				}

				// Finale detection — mirror the logic in cmd/chronicle/play.go.
				if len(node.Choices) == 0 {
					e, ok := endings.Evaluate(sg.WorldState, loaded.Endings)
					if !ok {
						t.Fatalf("no ending matched at finale node %q", sg.WorldState.CurrentNodeID)
					}
					if e.ID != tc.wantEnd {
						t.Errorf("ending = %q (priority %d); want %q", e.ID, e.Priority, tc.wantEnd)
					}
					break
				}

				sg, err = runner.Step(sg)
				if err != nil {
					t.Fatalf("step error at %q: %v", sg.WorldState.CurrentNodeID, err)
				}
			}
		})
	}
}

// readChoiceScript reads a file of choice IDs, one per line.
// Blank lines and lines starting with # are ignored.
func readChoiceScript(path string) ([]string, error) {
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
