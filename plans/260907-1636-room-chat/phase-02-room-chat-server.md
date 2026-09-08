---
phase: 2
title: "Room chat"
status: completed
priority: P1
effort: "4h"
dependencies: [1]
---

# Phase 2: Room chat

## Overview

The server side: authorize the sender's seat, sanitize and cap the text,
rate-limit the sender without endangering the recipient, keep the last 20
messages per room, deliver each one rendered per recipient, and replay to a
seated player what was said while they held that seat.

## Requirements

- Functional: a seated player in a non-bot room can send text to the other
  seat; both see it. A seating and a resume replay the recipient's own slice of
  the history. History outlives a game and dies with the room.
- Non-functional: bounded memory per room; text sanitized before it is stored;
  chat rate-limited on its own budget so talking never costs a move, and sized
  so that talking cannot close the other player's session either; chat is not
  room activity, so the idle timer still bounds the room.

## Architecture

**Sanitizer.** `sanitizeNickname` is close to the filter chat needs — NFC,
control and format characters dropped, whitespace collapsed, rune cap — but two
things separate them. Its cap and its empty-input fallback are nickname policy,
and its character filter admits combining marks: `unicode.IsPrint` counts marks
as printable and NFC does not discard uncomposable ones. At 20 runes that is
cosmetic; at 200 it is a glyph cluster hundreds of lines tall that escapes the
panel and covers the board, and it would sit in the history being replayed.

Extract the shared body into `sanitizeText(raw string, maxRunes, maxMarks int)
string` in `nickname.go`, returning `""` when nothing survives:

- NFC, then the existing control/format/non-print filter;
- **new:** drop `unicode.Mn`/`unicode.Me` runes beyond `maxMarks` consecutive
  after a base rune (2 is enough for any Vietnamese cluster);
- collapse whitespace with `strings.Fields`, then the rune cap.

`sanitizeNickname` becomes that call plus the `defaultNickname` fallback and the
`denylist` hook. Its existing tests must pass unchanged — Vietnamese names carry
at most two marks per base rune, so the mark budget is invisible to them.

Collapsing whitespace also means a chat line cannot contain a newline, so no
message can be made to occupy a column of its own in the panel.

**Constants** (`room.go`, beside the other room constants):

```go
// chatHistoryLimit is how many messages a room keeps, and the same window the
// client holds. Enough to catch up on after a reload, few enough that a room
// that lives all day cannot grow.
const chatHistoryLimit = 20

// maxChatRunes caps one message. Runes, not bytes: Vietnamese is multi-byte
// and a byte cap would cut it far shorter than a Latin one.
const maxChatRunes = 200

// maxChatMarks is how many combining marks may follow one base rune. Two
// covers every Vietnamese cluster; more is a stacking attack, not a word.
const maxChatMarks = 2
```

**Rate limit** (`session.go`): a third bucket beside `submitLimiter` and
`roomLimiter` — `chatLimiter = newBucket(chatsPerSecond, chatBurst, ...)` with
`chatsPerSecond = 1`, `chatBurst = 3`. Its own budget so talking never costs a
move, and sized against the recipient rather than the sender: `outboxCap` is 32
and a single `Write` can stall for `writeTimeout` (10s), so the existing 5/s of
lobby and move traffic plus 1/s of chat stays comfortably inside the window a
stalled reader can drain.

**Delivery is best-effort.** `session.send` closes a session whose outbox is
full — correct for game frames, wrong for chat, because it hands one player a
way to disconnect the other into a grace-window abandonment loss. Add
`session.trySend(m)` that takes the same encode path but, on a full outbox,
logs and returns `false` instead of calling `close()`. Chat frames — and only
chat frames — go through it. A dropped chat frame is recoverable: the next
resume replays the history.

**Input.** `chatInput{sess *session, player game.PlayerID, text string}` on the
room's existing channel. In `dispatch`, gate on `chatLimiter` and `currentRoom()`
exactly as `toRoom` does, and answer a refused enqueue: `if !r.send(chatInput{…})
{ s.send(errorMsg("busy")) }`, matching `handleSubmit`. Nothing about the room's
own state is read on the session goroutine — `r.strategy` in particular is
room-goroutine-only, so the bot-room refusal belongs in the handler, not here.

**`handleChat` opens with authorization, like every other room mutation:**

```go
func (r *room) handleChat(m chatInput) {
	// The seat, not the claimed id. A connection the room has already
	// retired — kicked, or replaced by a reconnect — can still have a frame
	// in flight, and its seat may belong to somebody else by the time the
	// room drains it.
	if !r.occupies(m.sess, m.player) {
		m.sess.send(errorMsg("not_your_seat"))
		return
	}
	if r.strategy != nil {
		m.sess.send(errorMsg("not_in_a_room"))
		return
	}
	…
}
```

**Storage.** On the room:

```go
// chat is the room's recent conversation, oldest first, capped at
// chatHistoryLimit. It outlives each game the way the room does.
chat []chatEntry
// chatSeq counts every message the room has accepted, ever. A seat records
// the value it was filled at, which is what scopes a replay to what that
// player was actually present for.
chatSeq uint64

type chatEntry struct {
	seq uint64
	// author and name are cleared together when the seat is vacated: the
	// words stay, the attribution does not. Keeping the name would let the
	// next person to request that nickname inherit a stranger's messages,
	// since distinguish() only compares against the seat that is occupied.
	author game.PlayerID
	name   string
	text   string
	at     time.Time
}
```

On `seat`: `chatFrom uint64`, set to `r.chatSeq` when the seat is filled
(`handleCreate`, `handleJoin`, `handleStartBot` for symmetry). A resume does not
touch it — the seat persists, so the player gets their conversation back.

`vacate` clears both `author` and `name` on that seat's entries. `beginGame`
does **not** touch the history: the room's conversation is not the game's.

**Idle timer.** `resetIdleTimer()` runs at the tail of every run-loop iteration,
so a chat message would restart the ten-minute lobby window and a room could be
held open forever by typing into it once every nine minutes. Chat is explicitly
not activity: track it per iteration —

```go
case chatInput:
	r.handleChat(m)
	// Deliberately no resetTurnTimer and no idle reset: talking is not
	// playing, and the room's ten-minute bound has to survive a
	// conversation.
	idleActivity = false
```

— and guard the tail call with it. Skipping the reset leaves the running timer
alone, which is what "the clock keeps ticking" means.

**Replay.** `chatHistoryFor(s *seat)` sends one `ChatHistory` holding the
entries with `seq >= s.chatFrom`, oldest first. Called from `handleCreate`,
`handleJoin`, and `handleResume` — **before** `handleResume`'s
`if r.inLobby() { return }`, or a lobby refresh (the commonest resume) never
gets one. `handleCreate` sends an empty history on purpose: it is what
overwrites a stale array in a client that has been in another room.

The history therefore *precedes* that input's `RoomState`, which is broadcast
from the run-loop tail after the handler returns. That is fine and the client
must not depend on the order: `chatHistory` replaces wholesale and reads no
`RoomState`-owned field. Do not claim the opposite anywhere.

## Related Code Files

- Modify: `server/internal/wsapi/nickname.go` — extract `sanitizeText` with the
  mark budget
- Modify: `server/internal/wsapi/room.go` — constants, `chatEntry`, `chat`,
  `chatSeq`, `seat.chatFrom`, `handleChat`, `chatHistoryFor`, `vacate`
  attribution clearing, the `chatInput` arm and the idle-activity guard in `run`
- Modify: `server/internal/wsapi/session.go` — `chatLimiter`, `trySend`, the
  `ClientMessage_SendChat` arm in `dispatch`
- Modify: `server/internal/wsapi/codec.go` — builders if the room's existing
  inline style does not fit

## Implementation Steps

1. Extract `sanitizeText(raw, maxRunes, maxMarks)` with the mark budget and
   re-express `sanitizeNickname` on top of it. Run the existing nickname tests —
   they must not change.
2. Add the constants, `chatEntry`, `chat`, `chatSeq` and `seat.chatFrom`; set
   `chatFrom` at every seating site.
3. Add `chatLimiter` and `trySend`; add the `dispatch` arm with the `busy`
   answer.
4. Implement `handleChat`: `occupies`, bot-room refusal, sanitize, drop empty,
   append with the cap and a rising `chatSeq`, broadcast per recipient via
   `trySend`.
5. Add the `chatInput` arm to `run` and the idle-activity guard around
   `resetIdleTimer()`.
6. Clear `author` and `name` in `vacate`; leave `beginGame` alone.
7. Add `chatHistoryFor` and call it from `handleCreate`, `handleJoin`, and
   `handleResume` before the lobby early return.

## Success Criteria

- [x] Two seated players exchange messages in the lobby and mid-game.
- [x] `from_me` is true only for the sender's own copy.
- [x] The 21st message pushes the 1st out.
- [x] A joiner's replay holds nothing said before they were seated; a resuming
      player's holds everything they were present for.
- [x] A creator receives an empty history, so a stale client array is replaced.
- [x] A lobby resume replays, not only a mid-game one.
- [x] Text over the cap is truncated at a rune boundary; a 199-mark stack is
      truncated; control and format characters never reach the other player.
- [x] A burst is refused with `too_fast` and leaves the move budget intact.
- [x] Sustained chat against a stalled reader drops frames rather than closing
      that session.
- [x] A dropped `chatInput` answers `busy`.
- [x] An unseated connection, a connection the room has replaced, and a bot
      room are all refused.
- [x] A room with chat traffic and no game still closes on the idle timer.
- [x] After a seat is vacated, that seat's messages carry no author and no
      name.

## Risk Assessment

The authorization and the attribution rules are the two that bite. Skip
`occupies` and a retired connection speaks as a seat somebody else now holds;
keep the departed player's name and a newcomer can request that nickname and
inherit their messages, because `distinguish` only compares against the seated
opponent. Signal: the Phase 4 replaced-connection and vacate tests. Response:
both are a few lines at a single chokepoint — `handleChat`'s first statement
and `vacate`'s loop — so fix in place.

The mark budget is a judgement call: two marks per base rune covers Vietnamese,
but it is a rule about text, not a standard. Signal: a legitimate message losing
a mark. Response: raise `maxChatMarks`; do not remove it.

Memory is bounded by construction (20 entries × 200 runes per room). Room
*lifetime* is bounded by the idle timer only because chat is excluded from it —
that exclusion is the load-bearing part, not the byte count.
