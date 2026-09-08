---
title: "Room chat"
description: "Text chat between the two players in an online room, in the lobby and during a game"
status: completed
priority: P2
effort: ""
tags: [online, wire-contract, frontend]
created: 2026-09-07
---

# Room chat

## Overview

The two people in an online room can talk to each other. Chat lives with the
room, not with a game: it works in the lobby while they agree on a game, during
the game, and after it. A player is replayed what was said while they were
sitting in their seat, so a refresh does not wipe their conversation.

The room already broadcasts per-recipient state (`RoomState`) and already
sanitizes untrusted display text (`sanitizeNickname`). Chat reuses both shapes
rather than inventing new ones.

## Decisions taken

- **Scope: the whole room.** One panel, available from the moment a player is
  seated — lobby, game, and the lobby a finished game returns to.
- **History: the last 20 messages, scoped to the recipient's own occupancy.**
  A seat records where the conversation stood when it was filled, and a replay
  starts there. A player who reconnects gets their conversation back in full; a
  player who joins a room starts at silence. The room still keeps only 20, so a
  long-lived room cannot grow.
- **Abuse controls: server-side only.** A dedicated per-session token bucket, a
  rune cap, and the existing sanitizer extended with a combining-mark budget.
  No mute button, no filter list.
- **Chat is not room activity.** A chat message does not reset the lobby's idle
  timer, so the ten-minute bound the deployment doc promises stays true and a
  room cannot be held open indefinitely by typing into it.
- **A message that sanitizes to nothing is dropped without an error, and the
  client makes that unreachable** by trimming and stripping zero-width
  characters before it will enable Send. Every *other* refusal is answered and
  shown.
- **A chat line is best-effort; a history is not.** A full outbox drops a
  `ChatMessage` rather than closing the recipient's session — a lost line is
  recoverable and closing a session costs the victim the game. `ChatHistory`
  still goes through `send`, because it is the frame that corrects a whole
  panel and there is nothing behind it.
- **A replayed history counts as what it holds, not as nothing.** A resync
  keeps a folded panel's unread count honest instead of stranding it.
- **A vacated seat's messages keep their text and lose their author entirely** —
  no id and no name — so neither the next occupant nor the player who stayed can
  have a stranger's words attributed to them.

## Constraints

- Proto changes are additive; retired tags go to `reserved`, never reused.
- Chat text is untrusted input rendered in another player's browser. It passes
  the sanitizer before it is stored or sent, and is rendered as text, never as
  markup.
- Only the room goroutine touches room state; chat arrives as another input,
  and every chat action authorizes with `occupies` on that goroutine — a room
  code is a shared secret by design, and a connection the room has already
  retired must not be able to speak as its old seat.
- Vietnamese copy stays in `web/src/lib/i18n/vi.js`; error codes stay UI keys.
- The chat input is typed with a Telex or VNI input method, so Enter must not
  send mid-composition, and the field's value must never be written back into
  the DOM node — see `WordInput.svelte`'s comment for why.

## Non-goals

Bot games (nobody to talk to), spectators, emotes or canned phrases, message
editing or deletion, typing indicators, read receipts, chat that outlives the
room, a mute or report control, and any profanity list.

## Goals

| # | Goal | Priority |
|---|------|----------|
| 1 | Two seated players can exchange text in the lobby and during a game | P1 |
| 2 | A reconnect replays what that player was present for | P1 |
| 3 | Text is capped, rate-limited and sanitized before anyone sees it | P1 |
| 4 | Chat cannot cost a player their session, their room, or their game | P1 |
| 5 | The panel does not crowd the board on a phone | P2 |

## Phases

| # | Phase | Status |
|---|-------|--------|
| 1 | [Phase 1: Wire contract](./phase-01-start.md) | Done |
| 2 | [Phase 2: Room chat](./phase-02-room-chat-server.md) | Done |
| 3 | [Phase 3: Client chat panel](./phase-03-client-chat-panel.md) | Done |
| 4 | [Phase 4: Tests](./phase-04-tests.md) | Done |
| 5 | [Phase 5: Docs](./phase-05-docs.md) | Done |

Phases run in order. 2 depends on 1; 3 depends on 1 and 2; 4 depends on 2 and 3.

## Success Criteria

- [x] A seated player can send a message and the other player sees it, in the
      lobby and during a game.
- [x] Each side sees its own messages marked as its own, and the author's
      server-sanitized name on the other's.
- [x] A player who reloads is shown their conversation again; a player who
      joins a room is shown nothing said before they sat down.
- [x] History survives a game start and a game end; it dies with the room.
- [x] The client's list and the server's history hold the same 20 messages
      after a replay, and the unread badge cannot outlive them.
- [x] A message longer than the cap is truncated at a rune boundary, and a
      stack of combining marks is truncated with it.
- [x] Control and format characters cannot reach another player's screen, and
      no message is rendered as markup.
- [ ] Every refusal is answered and visible: `too_fast` on a burst, `busy` on a
      dropped input, `not_your_seat` from a retired connection — including in
      the lobby, which today renders no error at all.
- [x] A burst does not consume the sender's move budget, and sustained chat
      does not close the recipient's session.
- [x] Chat from a connection with no seat, from a connection the room has
      replaced, and from a bot room are all refused.
- [x] A room with chat traffic and no game still closes on the idle timer.
- [x] A seat that is vacated leaves its messages with no author at all, so
      neither player can be shown a stranger's words as somebody's.
- [ ] Enter mid-composition does not send a half-typed Vietnamese word.

## Verification gaps

Everything above is implemented. These paths are implemented but not proven by
a test, and are listed here rather than ticked:

- **Enter mid-composition** (criterion 13). The guard is `oncompositionstart` /
  `oncompositionend` around a submit that reads the element, identical in shape
  to `WordInput.svelte`'s. Playwright in this harness cannot synthesise
  composition events, which the plan anticipated; covered by review only.
- **`busy` on a dropped input** (part of criterion 8). Filling a 32-slot room
  inbox deterministically is not reachable from a test client. `too_fast` is
  covered on both sides — refused on the server, visible in the lobby panel —
  and `not_your_seat` is covered on the server.
- **`vacate`'s kick and grace-expiry paths.** The re-sync is asserted for a
  voluntary leave only; the other two callers were traced by reading.
- **The departed-author label.** The server clears attribution and two tests
  assert it; nothing asserts the client renders "Đã rời phòng" for it.
- **`ChatPanel.svelte` has no component test**, because the repo has no Svelte
  component suite. Everything in that file is covered end-to-end or not at all.
- **The 360px board layout** (goal 5). Bounded by `max-height` and
  `overflow-wrap: anywhere`, checked by hand rather than asserted.

## Risks

- **The panel eats the board on a phone.** Mitigation: collapsed by default
  during a game with an unread count; the lobby shows it expanded. Signal: the
  e2e board assertions start needing scrolling. Response: make it a sheet.
- **The unread badge is client-only state derived from a server counter.** Two
  reviewers found the same defect in it by reading, twice: first counting a
  capped list's length, then going quiet after a resync reset the counter under
  it. It is now `chatCount` set to the replayed length and a `seenAt` that
  follows a decrease. Signal: a badge that stops counting, or one that counts
  what was already read. Response: the arithmetic is four lines in one
  component — but add the store test before the fix, not after.
- **A client applies `ChatHistory` before it has a room.** The history is sent
  from the handler and therefore *precedes* that input's `RoomState`, which is
  broadcast from the run-loop tail. The client must not depend on the order —
  `chatHistory` replaces wholesale and reads no `RoomState`-owned field.
  Signal: a panel rendering with an empty room code. Response: render the panel
  from `phase`, never from the presence of messages.
- **The combining-mark budget is a judgement call, not a standard.** Signal: a
  legitimate Vietnamese message loses a mark it needed. Response: the budget is
  one constant in one function — raise it; do not remove the cap.

## Red Team Review

### Session — 2026-09-07
**Findings:** 15 distinct, deduplicated from 28 raw across 3 reviewers
(15 accepted, 0 rejected)
**Severity breakdown:** 3 Critical, 7 High, 5 Medium
**Reviewers:** Security Adversary (Fact Checker), Failure Mode Analyst (Flow
Tracer), Assumption Destroyer (Scope Auditor). Verification tier: Full.
**Evidence filter:** every finding carried `file:line` citations; none rejected
for want of evidence.

| # | Finding | Severity | Disposition | Applied To |
|---|---------|----------|-------------|------------|
| 1 | `chat` in the store's `kept` set leaks one room's conversation into the next; `reset()` also runs on route mount/unmount, and `handleCreate` sends no history to heal it | Critical | Accept | Phase 2, 3, 4 |
| 2 | Replay ordering inverted — `broadcastRoomState` runs in the run-loop tail — and `handleResume` early-returns for a lobby resume, so the commonest refresh never replays | Critical | Accept | Phase 2, 4, plan.md |
| 3 | `handleChat` specified without `occupies`; the claim that `toRoom` does a seat check is false. Bot-room check cannot live in `dispatch` — `r.strategy` is room-goroutine-only | Critical | Accept | Phase 2, 4 |
| 4 | Chat resets the lobby idle timer, so a room can be held open forever and Phase 5's "timings stay true" is unsatisfiable | High | Accept | Phase 2, 4, 5 |
| 5 | A separate chat budget is additive to the existing ones, and a full outbox closes the recipient's session — sustained chat can disconnect an opponent into an abandonment loss | High | Accept | Phase 2, 4 |
| 6 | "Empty message is unreachable" is false — spaces or a pasted zero-width character pass the blank check | High | Accept (user decision: fix the client's blank check, keep the silent drop) | Phase 3, 4, plan.md |
| 7 | The 20-message replay discloses a private conversation to any stranger who redeems the code, and lets an owner pre-load a payload for joiners | High | Accept (user decision: replay scoped to the recipient's own occupancy) | Phase 2, 4, plan.md |
| 8 | The sanitizer drops control and format characters but not combining marks; at 200 runes that is a render bomb that persists in history | High | Accept | Phase 2, 4 |
| 9 | A dropped `chatInput` answers nothing, unlike every other input; and the lobby renders no error surface at all | High | Accept | Phase 2, 3, 4 |
| 10 | A controlled chat input cannot copy `WordInput`'s deliberately-uncontrolled composition guard verbatim; `maxlength` counts UTF-16 units, not runes | High | Accept | Phase 3 |
| 11 | The client array is unbounded (the cap is server history only) and the unread badge diverges from the list on a replace | Medium | Accept | Phase 3, 4 |
| 12 | Keeping a departed player's `name` lets a newcomer take that nickname — `distinguish` only checks the seated opponent — and contradicts Phase 1's own proto comment | Medium | Accept | Phase 1, 2, 4 |
| 13 | The `ProtocolVersion` justification is false: chat is pushed to old clients, which drop it silently for want of a `default:` arm | Medium | Accept | Phase 1, 3 |
| 14 | Phase 3's success criteria mostly require Phase 2's server, but its dependencies said `[1]` | Medium | Accept | Phase 3, plan.md |
| 15 | Four false citations: `ChainHistory` scrolls to top unconditionally; tag maxima are 12/12; `silentFor` lives in `regression_test.go`; `payloadCase` needs new arms — plus "JSDoc keeps the panel honest" when the store's state is typed `any` | Medium | Accept | Phase 1, 3, 4 |

### Whole-Plan Consistency Sweep
- Files reread: plan.md, phase-01-start.md, phase-02-room-chat-server.md,
  phase-03-client-chat-panel.md, phase-04-tests.md, phase-05-docs.md
- Decision deltas checked: 7 — occupancy-scoped replay; chat excluded from idle
  activity; best-effort chat delivery; `occupies` on every chat input; author
  cleared entirely on vacate; client blank check strict; client list capped to
  the server's window
- Reconciled stale references: 9 — the replay-ordering claim in plan.md Risks
  and Phase 2; "last 20 on join" in plan.md Decisions, Goals and Phase 4; the
  `toRoom` seat-check claim in Phase 2; the `ProtocolVersion` justification and
  tag maxima in Phase 1; the `name`-retention rule in Phase 2 against Phase 1's
  proto comment; the `ChainHistory` scroll precedent and the JSDoc claim in
  Phase 3; Phase 3's dependency list; Phase 5's "timings stay true"
- Unresolved contradictions: 0
