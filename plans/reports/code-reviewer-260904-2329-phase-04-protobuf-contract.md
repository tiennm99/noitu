---
title: "Code Review — Phase 4: Protobuf Contract and Codegen"
date: 2026-09-04
reviewer: code-reviewer
scope: phase-04
status: DONE_WITH_CONCERNS
---

# Code Review — Phase 4: Protobuf Contract and Codegen

## Scope

- Hand-written: `buf.yaml`, `buf.gen.yaml`, `proto/noitu/v1/game.proto` (209),
  `server/internal/wsapi/convert.go` (113), `convert_test.go` (168), `wire_test.go` (289),
  `web/tests/game-wire.test.js` (94), `web/package.json`, `.github/workflows/proto.yml` (77),
  `Makefile`, `README.md`, `server/go.mod`/`go.sum`. ~980 LOC.
- Generated + committed: `server/gen/noitu/v1/game.pb.go` (1874),
  `web/src/lib/proto/noitu/v1/game_pb.{js,d.ts}` (969). Config reviewed, style not.
- Fixtures: 17 files in `proto/testdata/`.
- Checks actually run: `go vet ./...`, `go test ./... -race`, `npm test` (23 pass),
  `buf lint` (clean), `buf generate` + byte-for-byte hash comparison, plus two
  reproduction experiments in a scratch git repo (see Critical #1 and Edge Case #1).

## Overall Assessment

The contract itself is good work: one schema, both targets generated from it, no
hand-written message type anywhere, an explicit engine↔wire mapping instead of a cast, and
a genuine cross-language fixture test rather than two self-consistent round trips. Comments
explain invariants and carry no plan/phase IDs, matching `game/state.go` and
`dictionary/store.go`. Phases 1-3 are untouched.

Two defects are blocking, both in the guard rails rather than the contract: `buf breaking`
cannot resolve its baseline on any pull request, and the regen-diff check cannot see a
missing generated file. Both mean a check reported as passing is not actually protecting
anything. Beyond those, one exported helper has no wire field to feed and no caller, and
`ServerError` carries two fields with identical documented meaning — cheap to fix now,
breaking to fix after a client ships.

---

## Acceptance Criteria

| # | Criterion | Verdict | Evidence |
|---|---|---|---|
| a | `make proto` regenerates both targets with zero diff | **MET** | Hashed all 3 generated files, ran `buf generate`, re-hashed: identical. |
| b | `buf lint` clean; `buf breaking` wired into CI | **PARTIAL** | `buf lint` exits 0 with no output. `buf breaking` is wired but cannot run — see Critical #1. |
| c | Go round trip covers every oneof variant, both directions | **MET** | `wire_test.go:126` `TestRoundTripEveryVariant` over 7 client + 10 server arms; `TestVariantTablesCoverEveryOneofArm` asserts arm coverage reflectively (with a gap — High #3). |
| d | JS decodes a Go-produced binary fixture, reads correct values | **MET** | `web/tests/game-wire.test.js`; 23 tests pass. Asserts UTF-8 diacritics, int64→bigint, enum numbering, empty-arm tag survival. |
| e | Mapping exhaustive; test fails on a new engine reason | **PARTIAL** | `convert_test.go:41,77` walk engine constants and also assert every wire value is reachable — good, both directions. But the walk terminates on `String() == "unknown"`, so a new constant added without a `String()` case is invisible. See High #4. |
| f | No hand-written message struct in `server/` or `web/` | **MET** | `grep "^type .* struct" server/internal/wsapi/` → none; `web/src` contains only the generated tree. |
| g | Protocol version field in the handshake | **MET (underspecified)** | `game.proto:62` `Hello.protocol_version`, `:112` `Welcome.protocol_version`. Semantics undefined — see Medium #6. |
| h | Generated code is committed | **MET** | `server/gen/`, `web/src/lib/proto/` present in the change; `.gitignore` does not exclude them. |

---

## Critical / MUST-FIX

### 1. `buf breaking` fails on every pull request — the check protects nothing

`.github/workflows/proto.yml:60`

```yaml
run: buf breaking --against '.git#branch=main'
```

`actions/checkout` on a `pull_request` event leaves a detached HEAD with **no local branch
`main`** — only `refs/remotes/origin/main`. buf's git input clones the local `.git` as a
remote and asks for the ref by name, so `branch=main` is not found.

Reproduced in a scratch clone (detached HEAD, local `main` deleted, `origin/main` present):

```
Failure: could not clone file://...\.git: exit status 128
fatal: couldn't find remote ref main
```

Meanwhile the guard immediately above (`:51`) probes `origin/main:proto/...`, which *does*
resolve — so the two steps disagree on how to name the baseline. Once the schema lands on
main the guard says `exists=true` and the breaking step then hard-fails every PR.

The `push` case is the mirror image: checkout creates a local `main` at the commit being
built, so `--against branch=main` compares the schema against itself and passes
unconditionally. Net effect: the check either errors or is a no-op; it never catches a break.

Fix — both forms verified working in the same scratch clone (exit 0):

```yaml
run: buf breaking --against '.git#ref=origin/main'
```

and align the guard on the same ref so the two steps cannot diverge again.

### 2. The regen-diff check cannot see a missing generated file

`.github/workflows/proto.yml:67` and `Makefile:57`

```
git diff --exit-code -- server/gen web/src/lib/proto
```

`git diff` never reports untracked files. Verified: `touch server/internal/game/zzz_fake.go`
inside a fully tracked directory, then `git diff --exit-code -- server/internal/game` → exit 0.

So the failure mode the step's own comment names ("a committed generated tree that no longer
matches the schema is worse than no generated tree at all") is only half covered. Modified
and deleted files are caught; a *new* output file is not. That is not hypothetical here — a
second `.proto`, a plugin option change that splits output, or a protoc-gen-es version that
emits an extra file all produce new untracked files and a green CI with an incomplete
committed tree, which then fails at build time in phase 6.

Fix (catches all three states):

```sh
buf generate
git add --intent-to-add -- server/gen web/src/lib/proto
git diff --exit-code -- server/gen web/src/lib/proto
```

or `test -z "$(git status --porcelain -- server/gen web/src/lib/proto)"`.

---

## High Priority

### 3. `clientNames()`/`serverNames()` are a hand-maintained duplicate of the variant tables

`wire_test.go:261,268` feed `assertCoversOneof` (`:246`), which only asserts
`proto arms ⊆ covered`. `covered` is a second hardcoded literal list, unconnected to
`clientVariants()`/`serverVariants()`. Drift path: delete an arm from the variants table,
run `-update` (which also deletes the orphan fixture), and both the fixture-count check
(`:161`) and `assertCoversOneof` still pass — that arm is now untested in both languages
while the test's own doc comment claims otherwise.

Fix: derive the covered set from the tables instead of restating it —
`m.ProtoReflect().WhichOneof(oneof).Name()` per variant. Removes 12 lines and closes the hole.

### 4. The exhaustiveness guard depends on `String()`, not on the constant set

`convert_test.go:15-37` enumerate engine reasons by incrementing until
`String() == "unknown"`. If a contributor adds `ReasonFoo` to the `iota` block but forgets
the `String()` case — the same edit, one file, easy to half-do — the walk stops *before*
`ReasonFoo`, both exhaustiveness tests pass, and `convert.go` silently returns
`REJECT_REASON_UNSPECIFIED` at runtime. This is exactly the failure criterion (e) exists to
prevent, guarded by a proxy rather than by the thing itself.

Fix: add a sentinel to the engine (a trailing `reasonCount` in the `iota` block, and
`endReasonCount` likewise) and assert `len(engineRejectReasons()) == int(game.ReasonCount)`.
Then a missing `String()` case fails loudly instead of shrinking the test's domain.

### 5. `WireDifficulty` has no caller and no wire field to carry its result

`convert.go:85-99`. Its comment says it exists "so a server that picked or clamped a
difficulty can report what it actually used" — but no server→client message has a
`Difficulty` field. `grep WireDifficulty` outside `internal/wsapi` → no hits; its only
consumer is `convert_test.go:110`. Exported API that exists to satisfy its own test.

Pick one: add `Difficulty difficulty = 7;` to `GameStarted` (additive, free now, and the
client genuinely needs it to label a resumed bot game), or delete `WireDifficulty` and its
test. Do not leave it as-is — the comment documents a capability the schema cannot express.

---

## Medium Priority

### 6. `protocol_version` has no defined semantics and no constant

`game.proto:62,112`. A bare `uint32` with no statement of the current value or whether it is
a major, a major.minor packed integer, or a monotonic counter. The phase requirement is
"server rejects mismatched **majors**", which a single field cannot express without a stated
convention. Right now `1` appears only as a literal in `wire_test.go:31,59`.

Since a version field's meaning is part of the contract, pin it in the schema comment now
("incompatible-change counter; bump only when an old client can no longer be served; current
value 1"). Phase 5 can then define `const ProtocolVersion = 1` against a written rule instead
of inventing one.

### 7. `ServerError.code` and `ServerError.message` are documented as the same thing

`game.proto:184-189`. The comment says `message` is "a UI key such as `room_not_found`, never
prose", and `code` is a code — two fields, one semantic, and the fixture sets both to
`"room_not_found"` (`wire_test.go:110`). The field *named* `message` will attract prose in
phase 5 (that is what the name means everywhere else), and prose on this path is the classic
internal-detail leak to an untrusted consumer.

Removing a field after a client ships is the breaking change this whole phase exists to
avoid, so resolve it now: either drop `message` and keep `code` alone, or give the two
distinct roles (`code` = machine-stable identifier; `message` = optional structured detail,
explicitly never rendered) and say so in the comment.

### 8. No message can restore a resumed game

`Hello.resume_token` exists, and the plan's success criteria require "reconnect within grace
window restores the game". Nothing in `ServerMessage` carries the word history, the scores,
or the current position as a snapshot: `GameStarted` has only the opening word and
`TurnUpdate` only the last move. A reconnecting client can decode everything it receives and
still render an empty board. Also missing: an `OpponentReconnected` counterpart to
`OpponentLeft`.

Both are purely additive, so this is not a schema defect today — but phase 5/7 will need
them, and adding them while the fixture tables are already open is cheaper than a second pass.

### 9. `log.Printf` from an internal package is a new convention

`convert.go:44,64,96`. Before this change, `log` appears only in `cmd/build-dictionary`
(a `main` package); `internal/{game,bot,dictionary,vietnamese}` return errors and never log.
The global logger in a request-path library is unsilenceable, has no request context, and is
awkward to assert on in a test. The intent (an unmapped value is a bug, so make noise) is
right; the mechanism should be `slog` or an injected logger, decided in phase 5 when the
server acquires a logging story. Worth deciding before four more packages copy this.

---

## Low Priority

10. **`-update` deletes before it writes.** `wire_test.go:204` removes every `*.bin`, then
    `:210` writes them. A `t.Fatalf` on marshal or write leaves the fixture directory empty
    or half-populated *and* the test failing — recoverable via git, but noisy. Write to a
    temp dir and rename, or delete only names not in `allVariants()`.
11. **`go test ./... -update` does not work.** The flag is defined only in the `wsapi` test
    binary; every other package's binary rejects it. README correctly documents
    `go test ./internal/wsapi -update`; worth a one-line note on the flag itself.
12. **`make web` is broken.** `Makefile:66` runs `npm run build` and `web/package.json` has
    no `build` script, while `README.md` advertises the target. Pre-existing, and phase 6
    fixes it, but the diff touched this target without noticing.
13. **`clean: true` in `buf.gen.yaml` wipes whole output directories.** Fine today; in phase 6
    a hand-written `web/src/lib/proto/index.js` barrel would be silently deleted by
    `make proto`. Worth a comment next to the `out:` lines.
14. **No `gofmt` check in CI**, and `proto.yml` runs only `go test ./internal/wsapi/...` —
    the repo now has exactly one workflow, so phases 1-3 have no CI coverage at all. The
    dictionary tests do not need `data/noitu.db` (verified: `data/` holds only the licence
    files, and the full suite passes locally without it), so broadening to
    `go vet ./... && go test ./... -race` is free.
15. **No `buf.lock`.** The phase file listed one under "Create". Correct to omit — `buf.yaml`
    declares no `deps`, so buf generates no lock. No action; noted so it is not read as missing.
16. **No `concurrency:` group** in the workflow; superseded PR runs keep burning minutes.

---

## Edge Cases Scouted

1. **CRLF mangling of the binary fixtures — checked, not a defect.** `core.autocrlf=true` is
   set both globally and in this repo, there is no `.gitattributes`, and 15 of the 17
   fixtures contain no NUL byte while 9 contain `0x0a`. That is the shape of a file git
   converts. Empirically it does not: committed all 17 into a scratch repo with
   `autocrlf=true` and re-cloned — `git ls-files --eol` reports `i/-text w/-text` for all 17
   (a control `plain.txt` in the same repo came back CRLF), and every SHA-256 matched. Git's
   heuristic counts the UTF-8 continuation bytes and protobuf tag bytes as non-printable and
   classifies the files binary. **NICE-TO-HAVE:** add `proto/testdata/*.bin binary` to a
   `.gitattributes` anyway — the classification is content-dependent, so a future all-ASCII
   fixture could flip to text and be corrupted silently on a Windows checkout.
2. **Will the workflow pass on the PR that first introduces `proto/`?** Yes. The baseline
   guard (`:51`) finds no schema on `origin/main` and skips the breaking step; `buf lint` is
   clean; the regen check has the generated trees tracked within that same PR so it compares
   correctly; both test suites pass. The failure begins on the *second* PR — see Critical #1.
3. **Fixture drift in the other direction.** The scheme is sound for value and field changes:
   a stale fixture fails `proto.Equal` in Go, and the JS assertions are independent hardcoded
   values, so regenerating fixtures without updating the JS test fails the JS suite. The only
   silent path is the one in High #3.
4. **`Difficulty()` fails safe on hostile input.** Returns `(0, false)`; `bot.Difficulty(0)`
   is not a valid strategy and `bot.New` rejects it, so even a caller that ignores `ok` cannot
   be handed a default opponent. Correct, and correctly justified in the comment.
5. **Enum renumbering resistance.** Every mapping in `convert.go` switches on symbols, so
   reordering the engine's `iota` blocks cannot change a wire value. This is the property the
   phase set out to buy, and it holds.
6. **int64 as bigint in JS.** `deadline_unix_ms` / `client_time_ms` surface as `bigint`, which
   the test asserts deliberately. Phase 6 will need `Number(...)` at every countdown site;
   `[jstype = JS_NUMBER]` on those fields is an option if that becomes noisy. Informational.

## Enum Mapping Correctness (explicit check)

All 8 `game.RejectReason` and all 4 `game.EndReason` constants are mapped; no wire value is
produced by two engine values; no non-transport wire value is unreachable; `ReasonNone` and
`EndNone` land on UNSPECIFIED by design and are excluded from the reachability assertion.
`GAME_END_REASON_OPPONENT_LEFT` is correctly allow-listed as transport-only. Nothing is
mis-mapped and nothing collapses to UNSPECIFIED where it would be wrong at runtime. The one
soft spot is the *guard*, not the mapping — High #4.

## Regression Check (phases 1-3)

- `git status --porcelain`: only `Makefile`, `README.md`, `server/go.mod`, `server/go.sum`
  modified. No file under `server/internal/{game,bot,dictionary,vietnamese}` or
  `server/cmd/build-dictionary` changed. No public contract touched.
- `go.mod`: `+ google.golang.org/protobuf v1.36.12` (direct) and
  `tool google.golang.org/protobuf/cmd/protoc-gen-go`. No existing require changed, nothing
  moved between direct and indirect. `go 1.25.0` satisfies the 1.24+ `tool` requirement.
- `go.sum`: 4 added lines only (`protobuf`, and `go-cmp` as its test dependency). No existing
  hash altered. `tool` directives are main-module-only, so no dependent inherits protoc-gen-go.
- `go vet ./...` clean; `go test ./... -race` all packages ok.
- Accepted deviations (buf STANDARD enum prefixes; minimal npm `web/`) introduce no
  inconsistency elsewhere: the JS `tsEnum` strips the prefix, so `RejectReason.WRONG_LINK`
  reads the same on both sides, and the plan/phase files' old unprefixed names appear only in
  historical plan prose, not in code.

## Recommended Actions

1. Fix the `buf breaking` baseline (`.git#ref=origin/main`) and align the guard step on the
   same ref. **Blocking** — currently the check is inert or red on every PR.
2. Make the regen-diff check see untracked files (`git add --intent-to-add`, or
   `git status --porcelain`), in both `proto.yml:67` and `Makefile:57`. **Blocking.**
3. Derive `clientNames()`/`serverNames()` from the variant tables via `WhichOneof`.
4. Add reason-count sentinels to the engine and assert against them, so the exhaustiveness
   guard stops depending on `String()`.
5. Decide `WireDifficulty`: add `GameStarted.difficulty`, or delete the helper.
6. Resolve `ServerError.code` vs `.message` before anything ships against this schema.
7. Document what `protocol_version` counts and what its current value is, in the schema.
8. Decide whether the resume snapshot / `OpponentReconnected` messages land now or in phase 5.
9. Low-priority sweep: `.gitattributes` for `*.bin`, broaden CI to `go vet ./... && go test ./...`,
   add a `concurrency:` group, note the `make web` gap for phase 6.

## Metrics

- `buf lint`: clean, zero carve-outs, `STANDARD` category, no `//buf:lint:ignore` anywhere.
- `go vet ./...`: clean. `go test ./... -race`: all packages pass.
- `internal/wsapi` statement coverage: **80.0%** (the uncovered statements are the three
  unreachable-by-design `log.Printf` default branches and their returns).
- `npm test`: 23/23 pass (1 file). No JS lint or typecheck configured yet (phase 6).
- `buf generate` reproducibility: byte-identical on all 3 generated files.
- Hand-written message structs: 0.

## Plan Status

Phase 4 implementation steps 1-7 are all present. Success criteria: 5 met, 3 partial (b, e,
and the CI half of step 7). I have not edited the plan file — recommend the lead mark phase 4
complete only after Critical #1 and #2, since both success criteria that reference CI are
currently unenforced.

## Unresolved Questions

1. `ServerError.message` — is it meant to be a second UI key, structured telemetry detail, or
   was it left over from the sketch? The answer decides whether the field survives, and it
   has to be answered before a client ships against this schema.
2. Does the resume flow (phase 5/7) get a state-snapshot message, or will `GameStarted` be
   overloaded with history and scores? Both are additive; picking now avoids a second fixture pass.
3. Should `proto.yml` become the repo's general CI (covering the phase 1-3 suites), or will a
   separate workflow land later? Right now nothing else in the repo is tested in CI.
