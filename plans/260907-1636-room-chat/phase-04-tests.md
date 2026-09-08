---
phase: 4
title: "Tests"
status: completed
priority: P1
effort: "4h"
dependencies: [2, 3]
---

# Phase 4: Tests

## Overview

One test per success criterion in the plan, at the cheapest layer that can hold
it: Go for the rules, Vitest for the store, Playwright for the two things only
a browser proves — Vietnamese input and the panel not eating the board.

## Requirements

- Functional: every criterion in `plan.md` has a test that fails without the
  feature.
- Non-functional: no sleeps; wait on state the server produced, as the existing
  suites do.

## Architecture

**Harness first.** `payloadCase` (`wsapi_test.go`) needs `chat_message` and
`chat_history` arms before anything else — its own comment says a missing arm
makes every `await` time out with nothing to say about why. Add a `say(text)`
helper on `testClient`. Note `silentFor` lives in `regression_test.go`, not
`wsapi_test.go` — same package, so it is usable either way.

`await` **discards** every frame before the one it wants, so it cannot assert
ordering. Any ordering claim must be checked with `recv()` in sequence — and
per Phase 2 the only true ordering claim is that the history precedes that
input's `RoomState`, which the client is explicitly not allowed to depend on.
Do not write an ordering assertion at all; assert arrival, not sequence.

Chat is broadcast to both seats, so drain both sides before asserting on a
later frame — the discipline the lobby tests already needed.

**Go — `server/internal/wsapi/`.** Reuse `pvpLobby` and `pvpRoom`.

- `TestChatReachesBothSeatsRenderedPerRecipient` — `from_me` true for the
  sender, false with the author's name for the other.
- `TestChatWorksInTheLobbyAndInAGame` — one message before Start, one after.
- `TestAJoinerSeesNothingSaidBeforeTheySatDown` — owner talks alone, guest
  joins, guest's history is empty. The disclosure boundary.
- `TestChatHistoryIsReplayedOnResumeInTheLobby` — talk in the lobby, drop the
  socket, resume, assert the conversation comes back. The commonest refresh,
  and the case a hook after `handleResume`'s `inLobby()` early return misses.
- `TestChatHistoryIsReplayedOnResumeMidGame` — the same during a game.
- `TestCreatingARoomReplaysAnEmptyHistory` — what overwrites a stale client
  array.
- `TestChatHistoryIsCappedAndOrdered` — 25 messages, the window holds the last
  20, oldest first.
- `TestChatHistorySurvivesAGame` — talk in the lobby, play and finish a game,
  the history is still there.
- `TestChatTextIsSanitizedAndCapped` — a zero-width joiner and a bidi override
  are gone; a 300-rune Vietnamese message comes back 200 runes, still valid
  UTF-8; **a 199-mark stack after one base rune comes back with at most two.**
- `TestEmptyChatIsDroppedWithoutAnError` — send whitespace and a lone
  zero-width character; assert silence with `silentFor`, and that a real
  message still works afterwards.
- `TestChatIsRateLimitedOnItsOwnBudget` — a burst earns `too_fast`, and a
  `SubmitWord` immediately after is still accepted.
- `TestSustainedChatDoesNotCloseTheOpponentsSession` — the recipient's outbox
  under pressure drops chat frames rather than being closed. Drive it against a
  client that stops reading; assert the session survives and the game does not
  end in abandonment.
- `TestChatFromAReplacedConnectionIsRefused` — resume a seat, then send on the
  retired socket: `not_your_seat`. The `occupies` guard.
- `TestChatFromAKickedConnectionIsRefused` — kick a guest whose message is
  already queued, seat a replacement, assert the message is refused rather than
  attributed to the newcomer.
- `TestChatNeedsASeat` — a greeted connection in no room: `not_in_a_room`.
- `TestBotRoomHasNoChat` — refused, like the lobby actions.
- `TestChatDoesNotKeepARoomAlive` — short `IdleFor`; chat throughout; the room
  still closes with `room_idle_closed`.
- `TestVacatedSeatKeepsItsWordsButLosesItsAuthor` — guest talks, leaves, a third
  player joins: the old message arrives with `from_me` false, an empty author,
  and **no name**, checked from the remaining player's side as well as the
  newcomer's.

**Vitest — `web/tests/game-store.test.js`.**

- `chatMessage` appends in arrival order with `atMs` as a number, not a bigint.
- The list stops at the window: push 25, assert 20 and that the oldest is gone.
- `chatHistory` replaces rather than merges (apply twice, no duplicates) and
  resets the unread count.
- `reset()` keeps the conversation, `clearChat()` and `leave()` drop it, and
  two rooms in sequence do not carry messages over.

**Playwright — `web/e2e/pvp-game.spec.js`.**

- Two players talk in the lobby, both see it; they talk again mid-game.
- A Vietnamese word typed through the composition path arrives whole. Drive it
  with `insertCompositionText` via CDP or an IME-style key sequence; if the
  harness cannot fake composition, assert instead that a `compositionend`-then-
  Enter sequence sends exactly once and note the gap.
- A message containing markup renders as text (`getByText('<b>x</b>')`).
- After a reload the guest still sees their conversation.
- Navigating home and creating a new room shows an empty panel.
- At 360×640 the board has no horizontal scroll with the panel present.

## Related Code Files

- Modify: `server/internal/wsapi/wsapi_test.go` — `payloadCase` arms, `say`
  helper, the chat tests
- Modify: `web/tests/game-store.test.js`
- Modify: `web/e2e/pvp-game.spec.js`, `web/e2e/helpers.js` (a `chat` locator
  group beside `board`)

## Implementation Steps

1. Add the `payloadCase` arms and the `say` helper first — without them every
   chat `await` fails as a timeout with no diagnosis.
2. Add the Go tests; run `go test ./internal/wsapi -count=2` to catch a
   frame-ordering flake early, and `-race` for the goroutine-boundary cases.
3. Add the store tests; `npm test`.
4. Add the e2e cases; run with `PLAYWRIGHT_CHANNEL=chrome` when Playwright's
   own Chromium is not installed.
5. Full sweep: `go test ./... -race`, `npm test`, `npm run check`,
   `npx playwright test`.

## Success Criteria

- [x] Every `plan.md` criterion maps to a named test.
- [x] `go test ./... -count=2 -race` is green.
- [x] `npm test` and `npm run check` are green.
- [x] The whole Playwright suite is green, including the pre-existing cases.
- [x] No test sleeps to wait for chat, and no test asserts frame ordering.

## Risk Assessment

Faking IME composition in Playwright may not be reachable with the current
harness. Signal: the composition case cannot be written without CDP. Response:
keep the weaker `compositionend`-then-Enter assertion, state the gap in the
final report, and rely on the unit-level guard — do not delete the case and
call it covered.

`TestSustainedChatDoesNotCloseTheOpponentsSession` is the awkward one to write:
it needs a client that stops reading without closing its socket. Signal: it
cannot be made deterministic. Response: assert the narrower property — that a
chat frame to a full outbox returns false from `trySend` — as a unit test on the
session, and say so rather than dropping the criterion.
