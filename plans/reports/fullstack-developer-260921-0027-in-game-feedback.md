# In-game feedback: score breakdown, dead-end claim, near-miss suggestion, word report

Branch: `worktree-agent-a582df720cc6e3dea`
Worktree: `/workspace/tiennm99/noitu/.claude/worktrees/agent-a582df720cc6e3dea`
Base: rebased onto `dev` at `865222d` (proto/generated code and cross-language
fixtures for all four features were already committed there; this work is the
implementation against that schema).

Commits: `16fd379` (game/dictionary/wsapi), `0003a45` (web), `e52b0ef` (docs).

## Scope delivered

**A2 — score breakdown.** `game.Move.Parts []PointPart{Kind, Value}`, computed
by `pointsFor`, one entry per non-zero term (base, chain, syllables, speed,
rarity), trimmed from the end (rarity, speed, syllables — never reaches chain
or base) when `maxPointsPerWord` bites, always summing exactly to `Points`.
`wsapi.PointKind`/`PointParts` map onto the wire, `PlayedWord.Parts` set from
them. Client: `ChainEntry.parts`, rendered as `+10 nền · +15 nhanh` chips in
`ChainHistory`; labels in `vi.js` keyed by `PointKind`. Bot rooms get it for
free — same `Submit` path.

**A3 — dead-end claim.** `ClaimDeadEnd` dispatched like `Resign` (seat via
`occupies`, rate-limited on `submitLimiter`). Room: no game → `game_not_started`;
not the claimant's turn → `not_your_turn`; `HasLegalMove()` true → `not_a_dead_end`,
nothing else changes; false → `NoMove` + `applyEliminations`, same as the
clock. `metrics.deadEndClaims` keyed `"true"`/`"false"`. Client: "Bí từ" button
next to the input, visible only on the player's own turn, armed like resign;
a false claim answers inline near the input (`state.claimError`) rather than
the general error banner.

**C3 — near-miss suggestion.** `dictionary.Store.stripped` index built at
`Open` (NFD, drop `Mn`, lowercase, đ/Đ→d/D — typing differences only).
`NearMiss(normalized) (string, bool)` returns the one candidate when exactly
one exists and is not the input itself. Added to `wsapi.Dictionary` and the
test doubles. Room fills `MoveRejected.suggestion` on
`ReasonNotInDictionary`. Client shows "Ý bạn là "…"?" as a button that fills
the (uncontrolled) field and focuses it — a user click, never a write during
composition.

**E3 — word report.** `ReportWord` rate-limited by `chatLimiter`, validated
entirely on the session (own rate budget, own per-session distinct-word set,
capped at 20) before being routed to the room only for the log line's
room-owned context (syllable in play, mode, code) — `handleReportWord` never
touches player identity. `word_reported` slog line documented next to
`word_rejected` in `docs/deployment.md`; `metrics.wordsReported` counter.
Client offers "Báo từ này là từ thật" after a `NOT_IN_DICTIONARY` rejection
and shows the confirmation on `WordReported`.

**Rules/README.** `/rules` dead-end and scoring copy mention the claim button
and the visible breakdown; README's "The frontend" and rules paragraphs each
gained one sentence. `errorMessages` gained `not_a_dead_end`,
`word_report_refused`, `word_report_limit`; `not_your_turn`'s copy was
generalized since resign and the claim now share the code.

## Files changed

Server: `server/internal/game/{state.go,engine.go,engine_test.go}`,
`server/internal/dictionary/{store.go,store_test.go}`,
`server/internal/wsapi/{codec.go,convert.go,convert_test.go,metrics.go,room.go,session.go,wsapi_test.go}`.

Web: `web/src/lib/{i18n/vi.js,stores/game.svelte.js,ws/messages.js}`,
`web/src/lib/components/{ChainHistory.svelte,GameBoard.svelte,WordInput.svelte}`,
`web/src/routes/{online,play}/+page.svelte`, `web/src/routes/rules/+page.svelte`,
`web/tests/{game-store.test.js,game-wire.test.js,i18n.test.js}`.

Docs: `README.md`, `docs/deployment.md`.

Not touched: `proto/`, `server/gen/`, `web/src/lib/proto/`, quick-match
(`QuickMatch`/`CancelQuickMatch`/`QuickMatchStatus`) — the sibling agent's arms.

## Verification

```
cd server && gofmt -l . && go vet ./... && golangci-lint run ./...   # clean, 0 issues
cd server && go test ./... -race -count=1                            # all packages ok
cd web && npm run lint                                                # 0 errors (33 pre-existing `any` warnings)
cd web && npm run check                                               # 0 errors, 0 warnings
cd web && npm test                                                    # 214 tests passed, build+bundle check included
```

Go test tail:
```
ok  github.com/tiennm99dev/noitu/server/cmd/build-dictionary
ok  github.com/tiennm99dev/noitu/server/internal/bot
ok  github.com/tiennm99dev/noitu/server/internal/dictionary
ok  github.com/tiennm99dev/noitu/server/internal/game
ok  github.com/tiennm99dev/noitu/server/internal/vietnamese
ok  github.com/tiennm99dev/noitu/server/internal/wsapi
```

Web test tail: `Test Files 12 passed (12)`, `Tests 214 passed (214)`.

## Tests added

- `game`: `TestSubmitScoringPartsMatchTheTotal`, `TestSubmitScoringPartsAreTrimmedAtTheCap`, `TestPointKindStrings`.
- `wsapi`: `TestPointKindMappingIsExhaustive`, `TestPlayedWordCarriesItsScoreBreakdown`,
  `TestClaimDeadEndEliminatesImmediately`, `TestClaimDeadEndRefusedWhenAMoveExists`,
  `TestClaimDeadEndOutOfTurnRefused`, `TestNearMissSuggestionOnWire`,
  `TestReportWordAcceptsAndEchoes`, `TestReportWordRefusesOneSyllable`,
  `TestReportWordEnforcesPerSessionCap`.
- `dictionary`: `TestStripDiacriticsFoldsDBreve`, `TestNearMissFindsAUniqueDiacriticTypo`,
  `TestNearMissRefusesAnAmbiguousStem`, `TestNearMissNeverSuggestsTheWordItself`,
  `TestNearMissRefusesAnUnrelatedWord`.
- `web`: score-breakdown/word-report/dead-end-claim `describe` blocks in
  `game-store.test.js`; near-miss/parts/wordReported assertions in
  `game-wire.test.js`; a `point kind labels` block in `i18n.test.js`.

## Deferred / not in scope

- Playwright/e2e coverage for the new buttons — this environment cannot run a
  browser (project constraint); left to the existing `test:e2e` suite to pick
  up when run elsewhere.
- Quick-match (owned by the sibling agent).

## Unresolved questions

- None blocking. One judgment call worth flagging: `not_your_turn`'s
  Vietnamese copy was generalized from "Chỉ đầu hàng được trong lượt của bạn."
  to "Chưa đến lượt bạn." because the code is now shared between `Resign` and
  `ClaimDeadEnd`. No test pinned the old string; if the merge with the
  sibling's changes wants a different wording, it's a one-line change in
  `vi.js`.
