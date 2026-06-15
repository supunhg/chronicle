# Determinism in Chronicle

**Status:** Phase 25 v1
**Last updated:** 2026-06-15

This document describes Chronicle's determinism contract — the rules
that make the simulation reproducible. If you change any of the
behaviors documented here, you may invalidate existing saves,
break the branching system, and silently corrupt replays.

---

## 1. Why Determinism Matters

Chronicle is a persistent reality simulation. Players create worlds,
save them, branch them, and return to them weeks or months later.
For that workflow to work, the simulation must reproduce identical
state given identical inputs. Without it:

- **Saves become unreliable.** A loaded save could produce a
  different world than the one that was saved.
- **Branches become unreliable.** Two branches created from the
  same parent tick could diverge for reasons other than player
  choice.
- **Debugging becomes intractable.** A bug report ("NPC X died
  on day 1,234") cannot be reproduced from a save.
- **Future multiplayer/federation becomes impossible.** A
  distributed simulation can only stay in sync if each node
  produces the same output for the same input.

Determinism is the foundation under every other system that
involves state.

---

## 2. Determinism Contract

Chronicle's simulation is deterministic if and only if all of
the following hold:

1. **Same seed, same inputs, same state.** Two runs starting
   from a freshly-bootstrapped world with the same seed and
   running the same number of ticks produce byte-identical
   world state (as measured by `core.WorldHash`).
2. **Different seeds diverge.** Two runs with different seeds
   produce different world state (the RNG is not a no-op).
3. **Hash is stable across runs.** `core.WorldHash(w)` returns
   the same value for the same `w` regardless of process,
   platform, or Go runtime version.
4. **Save/load is round-trip-safe.** Saving a world and loading
   it produces a world with the same hash as the pre-save
   world.

The Phase 25 integration test suite (`internal/integration/replay_test.go`)
enforces (1) and (2). (3) is enforced by the hash function's
construction (see §5). (4) is enforced by the persistence layer
but not yet covered by an automated test — it is an open item for
Phase 26.

---

## 3. RNG Rules

The simulation uses **one** deterministic RNG contract. There
must be no other source of randomness in the simulation.

### 3.1 Tick-level RNG

```go
func TickRand(worldSeed, tick int64) *rand.Rand {
    return rand.New(rand.NewSource(worldSeed + tick))
}
```

Use this for tick-level randomness that is not tied to a specific
entity (e.g. global event probability rolls, world-wide cooldowns).

### 3.2 Per-entity RNG

```go
func EntityRand(worldSeed, tick int64, entityID string) *rand.Rand {
    h := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", worldSeed, tick, entityID)))
    seed := int64(binary.BigEndian.Uint64(h[:8]))
    return rand.New(rand.NewSource(seed))
}
```

Use this for any per-entity randomness (e.g. an NPC's daily death
roll, an action's tie-breaking noise, a travel destination pick).

### 3.3 Forbidden Patterns

The following are determinism violations and must not appear in
simulation code:

- `math/rand` global functions (`rand.Intn`, `rand.Float64`,
  `rand.Shuffle`, etc.).
- `time.Now()` as a seed or as a state-mutating input.
- `crypto/rand` for game state (use only for non-game entropy
  like world ID generation).
- Concurrent goroutines that race for randomness (the scheduler
  is non-deterministic; the engine is single-threaded for
  ordering reasons).
- Reading RNG state mid-tick from a different scope.

If you find yourself needing any of these, the right answer is
almost always "thread the existing seeded `*rand.Rand` through
the call site" or "use the tick/entity RNG helpers".

### 3.4 RNG Stream Discipline

Each call site must use a stable, descriptive suffix for its
EntityRand calls. Examples from the existing code:

```go
r := tick.EntityRand(w.Seed, w.Tick, person.ID+":death")
r := tick.EntityRand(w.Seed, w.Tick, mother.ID+":conceive")
r := tick.EntityRand(w.Seed, w.Tick, p.ID+":socialize-target")
```

The suffix disambiguates streams within the same
`(seed, tick, entityID)` triple. **Do not** change a suffix
without a determinism audit — the resulting stream will differ
from the previous run, and every downstream random choice
will shift.

---

## 4. Tick Ordering

The engine order is the **determinism contract**. Reordering any
two engines changes the final world state. The canonical v1 order
is the same one used by the Phase 24 acceptance test (and
therefore by `TestDeterministicReplay` and
`TestDifferentSeedsDiverge`):

```
Population → Relationship → Marriage → Memory → Goal → Economy → Event
```

> **Note:** This is the de-facto engine order pinned by the
> integration tests. `SIMULATION_TICK_SPEC.md` §2 documents a
> slightly different order (Aging → Economy → Population →
> Relationship → Goal → Event → Memory) that was specified
> before the 7-engine v1 pipeline was wired through the
> production tick loop. The two are **not in conflict at the
> code level** — the test pins the order that actually runs,
> and `DETERMINISM.md` documents that order. A future phase
> that wants to align the spec with the code should either
> update `SIMULATION_TICK_SPEC.md` to match, or update the
> integration test to match the spec, but should never
> silently leave them out of sync.

**Why this order:**

- **Population first** because it mutates the alive/dead set
  (mortality) and the people map (births). All downstream
  engines need to see the post-mortality, post-birth state.
- **Relationship second** because it consumes the alive set
  (co-located pairs form bonds) and the relationship
  delta cache must be fresh before the marriage engine
  reads it.
- **Marriage third** because it depends on the post-relationship
  state (trust scores feed future match-quality scoring) and
  because the resulting `SpouseID` values must be visible to
  the memory engine that records birth-parent memories.
- **Memory fourth** because it consumes the new births/deaths
  and applies memory-driven deltas to the relationship cache.
  If memory ran before marriage, the spouse on a birth-memory
  might not yet be set.
- **Goal fifth** because it needs the fresh post-everything
  state (need decay, action scoring, action execution) and
  because the EconomyEngine reads goal progress to inform
  its own behavior.
- **Economy sixth** because the production-consumption-price
  loop must observe the post-goal state (work actions have
  bumped goals, need values have been mutated).
- **Event seventh** because events are derived from the
  post-economy state (famine events fire on settlement
  food stocks; theft waves fire on aggregate hunger/wealth).

A future phase that needs a new engine must either fit it
into the existing order with a documented rationale, or it
must add a new "phase" of the tick (e.g. "Phase 2: weather")
with its own engine order. Reordering the existing seven
engines is forbidden.

### 4.1 Tick Lifecycle

For each tick, the orchestration layer:

1. Increments `w.Tick` and advances `w.Now` by 1 day.
2. Runs all 7 engines in the order above.
3. Returns control to the caller.

No engine is allowed to read or write state from a future
tick. No engine is allowed to call into another engine
directly — cross-engine effects go through `core.World`
state.

---

## 5. Hashing Methodology

`core.WorldHash(w)` returns a stable SHA256 hex digest of `w`'s
simulation state. The hash is the v1 fingerprint for replay
validation, save/load verification, and branch divergence
detection.

### 5.1 What is hashed

The hash includes every field that defines the simulation
state:

- `w.ID`, `w.Tick`, `w.Seed`, `w.Now.Unix()`
- `w.PlayerID`, `w.Coin`
- `w.Rules` (all fields)
- `w.Items` (catalog)
- `w.Inventory` (player's)
- For every location: id, name, region, population, cap,
  pressure, last shortage tick, settlement stock, prices.
- For every person: id, name, gender, birth/death ticks,
  alive, location, class, occupation, merchant flag, family
  IDs, traits, needs, goals, inventory.
- Every relationship (from, to, all 5 axes).
- Every memory (id, owner, event, cause, tick, all 5 score
  fields, description, sorted tags).
- Every event (id, type, tick, location, payload).

### 5.2 What is NOT hashed

- Pointer addresses (no `fmt.Sprintf("%p")`).
- Runtime caches (e.g. the `EventEngine.lastFired` cooldown
  map; the `RelationshipEngine`'s transient indexes).
- Logger state, channels, goroutine IDs.
- Transient services that hold non-deterministic state
  (e.g. the LLM client; the persistence layer's connection
  pool).

If you add a new field to `core.World` and it is part of
simulation state, add it to the hash. If it is a runtime
cache, do NOT add it to the hash.

### 5.3 Deterministic encoding

The hash is built in three steps:

1. **Pre-sort.** Every slice that originates as a map
   (people, locations, etc.) is pre-sorted by its canonical
   key (ID). Slices that originate as slices (relationships,
   memories, events) are pre-sorted by their canonical tuple
   (e.g. `(FromID, ToID)`, `(Tick, ID)`). Maps are sorted by
   `json.Marshal` automatically.
2. **Float normalization.** All `float64` values pass through
   `cleanFloat`:
   - `-0.0` is rewritten to `+0.0` (the IEEE-754 sign bit
     would otherwise leak into the JSON encoding).
   - `NaN` and `±Inf` are rewritten to `+0.0` (these
     should never appear in simulation state, but the
     normalization keeps the hash safe if they ever do).
3. **JSON marshal.** The pre-sorted, normalized tree is
   passed to `encoding/json`. The output is byte-stable for
   the same input across Go versions and platforms.

The resulting bytes are SHA256-hashed and hex-encoded.

### 5.4 Stability guarantees

The hash is stable across:

- **Process restarts.** No `time.Now()` or pointer addresses
  are read during hashing. No persistent state is consulted
  beyond `w` itself.
- **Go map iteration order.** Every collection that could be
  affected by map iteration is pre-sorted.
- **Platforms.** Integer and float encoding is consistent
  across IEEE-754-compliant platforms (which is all of
  them, by definition). String encoding is consistent
  because we do not touch any locale-sensitive code.

The hash is NOT stable across:

- **Changes to the `core.World` schema.** Adding a field,
  renaming a field, or changing a field's type changes the
  hash. This is intentional — a save from before the schema
  change would not hash-match a save from after.
- **Changes to the engine order or RNG stream suffixes.** The
  engine state depends on the engine order and the RNG call
  sites. Changing either changes the simulation, which
  changes the hash. The replay test catches this.
- **Changes to the worldpack.** A different worldpack
  produces a different world from the same seed, so the
  hash differs. This is also intentional.

---

## 6. Replay Guarantees

Given the determinism contract above, Chronicle provides the
following guarantees:

### 6.1 Bootstrap is deterministic

Loading a worldpack and bootstrapping a world with seed `S`
always produces the same initial state. The worldpack's YAML
files are read in a fixed order, the bootstrap RNG is
seeded with `S`, and the slot assignment is deterministic.

### 6.2 The tick loop is deterministic

For any two runs starting from the same bootstrap state,
running the same number of ticks with the same engine order
produces the same final state.

### 6.3 Replay is verifiable

`TestDeterministicReplay` (in
`internal/integration/replay_test.go`) bootstraps the frontier
worldpack twice with seed 42, runs each for 100 years, and
asserts the resulting `core.WorldHash` values are identical.
This is the v1 acceptance gate for the determinism contract.

### 6.4 Divergence is detectable

`TestDifferentSeedsDiverge` runs the same simulation with
seeds 42 and 43 and asserts the resulting hashes differ.
This is the negative control: it catches the case where the
RNG is disconnected from the engine (which would make the
replay test vacuously pass).

### 6.5 Save/load is round-trip-safe (planned)

A future phase will add a `TestSaveLoadRoundTrip` that
saves a world, loads it, and asserts the post-load hash
matches the pre-save hash. This is currently an open item
because the persistence layer is still being hardened.

---

## 7. Known Limitations

The v1 determinism contract is intentionally narrow. The
following are NOT guaranteed:

### 7.1 Float operations

Floating-point arithmetic in the production engine uses
`float64` for prices, settlement stock, and other continuous
quantities. IEEE-754 specifies that `float64` operations are
deterministic for the same operands, so the simulation
remains reproducible across platforms. However, the result
of `0.1 + 0.2` is `0.30000000000000004`, not `0.3`, and this
shows up in the hash. Two saves from different platforms
will hash-match (because the float bits are identical), but
the displayed values may look "ugly".

### 7.2 Hash is not a save format

`core.WorldHash` is a 64-character hex string. It is not a
serialization format and cannot be used to reconstruct a
world. Use the persistence layer (`internal/persistence`)
for save/load; use the hash only for verification.

### 7.3 LLM state is excluded

LLM outputs (intent parsing, narration, world-AI generation)
are explicitly excluded from the hash. Replaying the same
seed for the same number of ticks produces the same world
state, but the narrator may produce different prose across
runs. This is per the spec (`chronicle-spec.md` §4.5):
"Determinism applies to world state, not prose."

### 7.4 Branching is not yet tested for hash determinism

The branching system (per `chronicle-spec.md` §5.7) creates
branches from a parent tick and replays forward. A branch
created from tick T should hash-match the parent at tick T.
This is a planned test for Phase 26; the persistence layer's
copy-and-replay machinery is not yet exercised by the
replay suite.

### 7.5 Time zone for `w.Now`

`w.Now` is hashed as `w.Now.Unix()` (a UTC `int64`). The
bootstrap typically sets `Now` to a midnight UTC value, so
`Unix()` is stable. If a future phase changes the bootstrap
to a non-UTC `time.Time`, the hash will continue to work
because `Unix()` is always UTC, but the displayed date in
the REPL may differ from the previous build.

### 7.6 Latent engine non-determinism in slice order

The `MemoryEngine` and `RelationshipEngine` append to
`w.Memories` and `w.Relationships` while iterating `w.People`
(or other maps) in Go's randomized order. The final **set** of
memories and relationships is deterministic across runs (the
order of finds/creates does not change the cached state), and
`WorldHash` sorts before hashing so the hash is canonical.

There is one consumer of memory order that is provably
order-insensitive: `actions.go::memoryBonus` walks the last
`MemoryLookback` memories and *sums* a signal from each —
addition is commutative, so the bonus is the same regardless
of slice order. The replay tests pass because of this
invariant.

A future code path that reads `w.Memories` in an
order-sensitive way (e.g. "first memory" or "memory at index
N") would re-introduce non-determinism. The fix would be
either to sort the engine's iteration order, or to assert in
the engine that the consumer is order-insensitive. Phase 26+
may want to formalize this with an explicit iteration-order
audit on every `w.People` / `w.Memories` /
`w.Relationships` access in the engine code.

### 7.7 Long-running drift

The 5-Generation Integration Test runs 100 years
(36,500 ticks). The replay test runs the same. Phase 26's
10-seed stress test (mentioned in the spec) will exercise
longer horizons to catch any late-emerging non-determinism
(e.g. integer overflow, slice-capacity-dependent behavior).
Until that test exists, the replay contract is empirically
validated up to 100 years but not beyond.

---

## 8. How to Verify a Change is Determinism-Safe

Before merging a change to an engine, the bootstrap, or the
hash function itself:

1. **Run the replay test locally.**
   ```bash
   go test -count=1 -timeout 30m ./internal/integration/...
   ```
   Both `TestDeterministicReplay` and
   `TestDifferentSeedsDiverge` must pass. If you expected
   the hash to change (e.g. you added a new field to
   `core.World`), the test will fail — that's correct.
   Update the test or revert the change.

2. **Read your engine's RNG call sites.** Did you add a new
   `tick.EntityRand` or `tick.TickRand` call? Is the suffix
   stable? Does the suffix conflict with another call site
   in the same tick for the same entity? (A conflict is
   not a correctness bug but it changes the stream.)

3. **Read your engine's state mutations.** Did you mutate
   `w.Memories` or `w.Relationships` directly? If you
   append to a slice, the order of the append matters for
   the hash; sort before hashing. (`WorldHash` already
   does the sorting — but the engine's *behavior* should
   not depend on the order.)

4. **Check for new non-deterministic inputs.** Did you
   introduce a `time.Now()` or a `map[k]struct{}{}` set
   iteration? Both are determinism violations.

5. **Update the hash if you changed the schema.** If you
   added a new field to `core.World` or to any type that
   `WorldHash` reads, add the new field to the hash
   function. The replay test will catch a missing field
   (the test passes once, fails on the next change), but
   it's better to be explicit.

---

## 9. Open Items for Phase 26+

- [ ] `TestSaveLoadRoundTrip`: assert the post-load hash
  matches the pre-save hash.
- [ ] `TestBranchReplay`: assert a branch created at tick
  T has the same hash as the parent at tick T.
- [ ] 10-seed stress test: run 10 different seeds for 200
  years each and assert the replay-hash pairs are
  consistent. Catches any seed-conditional non-determinism.
- [ ] Memory/event retention caps: bound the growth of
  `w.Memories` and `w.Events`. The Phase 25 hash includes
  the entire memory and event log; an unbounded log makes
  the hash expensive to compute on large worlds.
- [ ] `TestEngineOrderMatters`: assert that reordering the
  7 engines produces a different hash. This is a
  meta-test that catches accidental engine reordering in
  refactors.

---

## 10. Summary

Chronicle's determinism contract is:

1. **One RNG source.** `tick.TickRand` and `tick.EntityRand`
   are the only sources of randomness.
2. **Fixed engine order.** Population → Relationship →
   Marriage → Memory → Goal → Economy → Event. Reordering
   changes the result.
3. **Stable hash.** `core.WorldHash` is the canonical
   fingerprint. It is stable across processes, platforms,
   and map iteration order.
4. **Verified by replay.** `TestDeterministicReplay` and
   `TestDifferentSeedsDiverge` are the v1 acceptance gates.

If you remember those four rules, the simulation stays
reproducible.
