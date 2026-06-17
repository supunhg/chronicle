// Package content loads and validates v2 authored content from
// a content directory containing YAML files. It is the v2
// counterpart to the v1 worldpack loader (now retired per
// Phase 35.D); see ARCHITECTURE.md §21 for the on-disk
// layout.
//
// Canonical content directory layout (PHASES.md §36.E +
// ARCHITECTURE.md §21):
//
//	nodes.yaml         (required) — list of nodes, each with
//	                     inline choices. See ARCHITECTURE.md §5–§6.
//	events.yaml        (required) — list of authored events.
//	                     See ARCHITECTURE.md §13.
//	endings.yaml       (required) — list of authored endings.
//	                     See §19.
//	companions.yaml    (optional) — list of Companion IDs.
//	                     Referenced by protagonists.yaml.starting_party.
//	choices.yaml       (optional) — Phase 37 schema work.
//	                     Phase 36.E reads inline choices only;
//	                     a choices.yaml at the loader's entry
//	                     point is accepted as a no-op (present
//	                     but unused) so authors can move their
//	                     inline choices there during the
//	                     consolidation step.
//	protagonists.yaml  (required) — list of CharacterProfiles.
//	                     See §15.
//
// Fail-fast contract:
//
//	Load returns a clear error before allocating any artifact
//	when ANY of the following is true:
//	  - a required file is missing or unparseable;
//	  - a Choice.NextNodeID does not match any Node.ID;
//	  - a TriggerEvent effect targets an Event.ID that is
//	    not present in events.yaml;
//	  - a Protagonist.starting_party references a companion
//	    ID that is not present in companions.yaml.
//
// Conditions and effects use a single-key-map polymorphism
// convention so YAML stays compact:
//
//	conditions:
//	  - flag: "FoundTemple"                        # story.Flag
//	  - variable: { key: "Courage", value: 30 }    # VariableGE
//	  - relationship: { character: "Elara",        # RelationshipGE
//	                    axis: trust, value: 50 }
//	  - has_item: "DragonKey"                      # HasItem
//	  - has_ending: "hero"                         # HasEnding
//	  - or:  [ ... ]                               # Or
//	  - and: [ ... ]                               # And
//	  - not: { flag: "rested" }                    # Not
//
//	effects:
//	  - set_flag: "FoundTemple"                    # SetFlag
//	  - clear_flag: "stale"                        # ClearFlag
//	  - modify_variable: { key: "Courage",         # ModifyVariable
//	                       value: 50 }
//	  - modify_relationship: {                     # ModifyRelationship
//	      character: "Elara", axis: respect, value: 10 }
//	  - modify_reputation: {                       # ModifyReputation
//	      faction: kingdom, value: 5 }
//	  - add_item:    { key: "DragonKey", count: 1 } # AddItem
//	  - remove_item: { key: "Torch",    count: 1 } # RemoveItem
//	  - trigger_event: "ally_call"                 # TriggerEvent
//
// Each condition/effect is a single-key map; multi-key maps
// surface a clear error so authoring typos are loud, not silent.
package content
