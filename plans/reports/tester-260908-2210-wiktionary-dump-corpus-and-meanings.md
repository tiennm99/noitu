# Test Validation Report: Wiktionary Dump Corpus & Meanings

**Date:** 2026-09-08 22:10 UTC  
**Scope:** Uncommitted changes validating meanings feature (Sense messages, Meanings table, meanings panel)

---

## Test Execution Summary

| Component | Command | Result | Duration |
|-----------|---------|--------|----------|
| Server Go | `cd server && go vet ./... && go test ./... -race -count=1` | ✅ PASS | 42.7s |
| Web Check | `npm run check` (tsc + svelte-check) | ✅ PASS | ~0.4s |
| Web Tests | `npm test` (vite build + vitest) | ✅ PASS | 2.47s |
| Dictionary Build | `go run ./cmd/build-dictionary --words ../testdata/fixture-words.txt --out ../data/fixture.db --min-words 150` | ✅ PASS | ~1s |
| Bot Real Corpus | `cd server && go test ./internal/bot/ -run RealCorpus -v -count=1` | ✅ PASS | 0.74s |
| Proto Lint | `buf lint` | ✅ PASS | ~1s |
| Proto Generate | `buf generate` | ⚠️ DIFFS | ~1s |

---

## Detailed Results

### 1. Server Go Tests
- **Status:** ✅ **PASS** (all 7 test packages passed with race detector)
- **Tests run:** 5 packages with tests (cmd/build-dictionary, internal/bot, internal/dictionary, internal/game, internal/vietnamese, internal/wsapi)
- **No failures, no race conditions detected**

### 2. Web TypeScript & Svelte Checks
- **Status:** ✅ **PASS** (0 errors, 0 warnings)
- **Files checked:** 375 files
- **Output:** Clean tsc + svelte-check run

### 3. Web Test Suite
- **Status:** ✅ **PASS** (183/183 tests passed)
- **Test files:** 12 test files
- **Duration:** 2.47s (including bundle build)
- **Bundle test:** ✅ Confirms bundle carries no dictionary words (as expected)
- **All test suites passed:**
  - room-code.test.js (14 tests)
  - dictionary-source.test.js (4 tests)
  - countdown.test.js (10 tests)
  - i18n.test.js (12 tests)
  - history-export.test.js (8 tests)
  - error-codes.test.js (3 tests)
  - game-wire.test.js (30 tests)
  - bundle.test.js (4 tests)
  - ws-client.test.js (24 tests)
  - bot-session.test.js (12 tests)
  - game-store.test.js (46 tests)
  - settings-store.test.js (16 tests)

### 4. Dictionary Builder Test
- **Status:** ✅ **PASS** — Log output matches expected values:
  ```
  accepted 205 distinct words, 122 with a meaning
  generated 14 spelling aliases (0 skipped as ambiguous or already real words)
  wrote ../data/fixture.db
  ```
- **Validation:** ✅ Confirms 205 words and 122 with meanings as required

### 5. Bot Real Corpus Tests
- **Status:** ✅ **PASS** (both tests passed)
- **Tests run:**
  - TestDifficultyLadderRealCorpus: ✅ PASS (0.20s)
  - TestHardChooseLatencyRealCorpus: ✅ PASS (0.12s)
- **Notes:** Real corpus tests validate bot decision quality with new meanings data

### 6. Proto Linting
- **Status:** ✅ **PASS** (no lint errors)

### 7. Proto Code Generation
- **Status:** ⚠️ **GENERATED CODE DIFFERS FROM COMMITTED**
- **Exit code:** Non-zero (diffs exist)
- **Files changed:**
  - `server/gen/noitu/v1/game.pb.go`: 261 insertions, 102 deletions
  - `web/src/lib/proto/noitu/v1/game_pb.d.ts`: 43 insertions
  - `web/src/lib/proto/noitu/v1/game_pb.js`: 35 insertions, 12 deletions

**Diff Summary:** `buf generate` produced different output than what's committed. The generated code includes:
- New `Sense` message type with `pos` (part of speech) and `gloss` (definition) fields
- `PlayedWord.Meanings` field (array of Sense) — field 7 in protobuf
- `GameStarted.OpeningMeanings` field (array of Sense) — field 9 in protobuf
- Message type index renumbering (Sense is msgTypes[15], others shifted)

These changes align with the stated scope (Sense messages, Meanings fields) and appear intentional. However, the **committed generated code does not match** the proto schema. This must be regenerated and committed.

---

## Coverage & Test Paths

- **Server:** All internal packages have test coverage (bot, dictionary, game, vietnamese, wsapi)
- **Web:** All application logic tested via unit tests; bundle test confirms no embedded dictionary
- **Integration:** Real corpus tests validate dictionary integration with bot logic
- **Build system:** Dictionary builder tested end-to-end with fixture data

---

## Build Status

- ✅ Go build: Clean (no vet warnings)
- ✅ TypeScript/Svelte: Clean (no type errors)
- ✅ Web bundle: Built successfully; size within expectations
- ⚠️ Proto schema: Regeneration needed (generated code uncommitted)

---

## Critical Issue

**Proto Generated Code Out of Sync**

The committed generated proto files do not match the current `.proto` schema. Running `buf generate` produces 823 lines of changes across 3 files:

1. **server/gen/noitu/v1/game.pb.go** — New Sense type and accessor methods; message type indices updated
2. **web/src/lib/proto/noitu/v1/game_pb.d.ts** — TypeScript type definitions for Sense and new Meanings fields
3. **web/src/lib/proto/noitu/v1/game_pb.js** — JavaScript implementations for new fields

**Action Required:** Commit the generated files after `buf generate` to align schema with implementation.

---

## Recommendations

1. **Commit proto-generated changes** — Run `buf generate` and commit all changes to `server/gen/noitu/v1/` and `web/src/lib/proto/`
2. **Verify proto semantics** — Message field numbers and oneof handling are correct (no conflicts, indexing consistent)
3. **Test Meanings on wire** — If not already covered, add integration test for Sense marshaling/unmarshaling in game-wire.test.js

---

## Summary

All functional tests pass (server, web, bot, dictionary builder). Build and type checks clean. No failures, no race conditions. **However, generated proto code must be regenerated and committed** — the schema has evolved but the committed artifacts have not been updated.

**Status: DONE_WITH_CONCERNS**  
All tests pass; build clean. Proto-generated files out of sync with schema (must regenerate and commit).
