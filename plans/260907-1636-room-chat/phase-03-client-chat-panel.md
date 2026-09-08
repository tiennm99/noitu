---
phase: 3
title: "Client chat panel"
status: completed
priority: P1
effort: "4h"
dependencies: [1, 2]
---

# Phase 3: Client chat panel

## Overview

The panel the players actually use: a message list and an input, driven by the
store, rendered in the lobby and on the board. Expanded in the lobby, collapsed
behind an unread count during a game so it does not crowd a phone screen.

Depends on Phase 2 as well as Phase 1: every behavioural criterion below needs
a server that accepts `SendChat` and emits `ChatMessage`/`ChatHistory`.

## Requirements

- Functional: send a message; see both sides' messages in order with the
  author's name, or a "left the room" label where the author is empty; a
  replayed history replaces the list wholesale; an unread count while
  collapsed; every server refusal visible, including in the lobby.
- Non-functional: text rendered as text, never markup; Enter must not send
  mid-composition and the field's value must never be written back; the list
  must not steal the page's scroll; the list must not grow without bound.

## Architecture

**Store** (`game.svelte.js`). One new field and two new arms:

```js
/**
 * The room's conversation, oldest first, capped at the same window the server
 * keeps so the two can never disagree about it.
 *
 * @type {{ fromMe: boolean, author: string, text: string, atMs: number }[]}
 */
chat: [],
```

- `chatMessage` → push one entry (`atMs: Number(value.sentUnixMs)` — the field
  is an `int64` and arrives as a bigint, exactly like `deadlineUnixMs`), then
  trim the front past `CHAT_WINDOW` (20, mirroring `chatHistoryLimit`).
- `chatHistory` → replace the array wholesale. A snapshot replaces; it never
  merges. It also resets the unread count: the player is being resynchronised,
  not shown new mail.
- Add a `default:` arm to the `switch` that logs an unhandled payload case, so
  the next protocol addition degrades visibly instead of silently (Phase 1).

**Clearing is the subtle part.** `chat` joins the `kept` set, because a game
starting must not wipe the conversation — but `kept` means "survives
`reset()`", and the online route calls `reset()` on *mount and unmount* as well
(`online/+page.svelte`), so `kept` alone would carry room A's conversation into
room B. Two independent fixes, both cheap:

1. A `clearChat()` on the store, called in the online route's mount and
   unmount effect alongside `reset()` — leaving the screen is leaving the room.
2. Phase 2 sends a `ChatHistory` on every seating including `handleCreate`, so
   a stale array is overwritten by the server's truth even if the route forgets.

`leave()` clears it too, as it clears everything.

**`ChatPanel.svelte`** — props `{ collapsible?: boolean, onsend: (text: string) => void }`.

- Message list: `<ol>` with `overflow-y: auto` in its own scroll container,
  `class:mine={entry.fromMe}`, the author's name shown only on the other side's
  messages, `t.chatAuthorLeft` where `author` is empty, and time as `HH:mm` from
  `atMs`.
- Scroll: oldest-first, so it scrolls to the **bottom** on a new message, and
  only when already near it so it cannot yank a player out of scrollback. Write
  this explicitly — `ChainHistory.svelte` is *not* the precedent: it renders
  newest-first and scrolls unconditionally to `top: 0`.
- Input: **one-way mirroring only.** `oninput={(e) => (draft = e.currentTarget.value)}`
  and never write `draft` back to the element — `WordInput.svelte` is
  deliberately uncontrolled because writing the value back cancels Telex
  composition and mangles the accent, and a `bind:value` here would reintroduce
  exactly that. Keep its `oncompositionstart`/`oncompositionend` submit guard.
- Send is enabled when the draft is non-blank after trimming **and** stripping
  zero-width and format characters — the same emptiness the server tests for
  after sanitizing, which is what makes the server's silent drop unreachable
  rather than merely unlikely.
- Length: check the rune count (`[...draft].length`) in the send handler, not
  `maxlength` — `maxlength` counts UTF-16 code units, not runes, and interferes
  with composition in some engines. The server truncates anyway; this is only
  so the player is not surprised.
- Interpolated with `{entry.text}`, never `{@html}`.
- `collapsible`: a header button toggling the list, showing an unread count.
  Unread is client-only state — messages that arrived while collapsed, reset on
  expand and on a history replace. Per viewer, so nothing about it goes on the
  wire.

**Error surface.** `Lobby.svelte` renders no `game.state.error` today, and the
online route's error paragraph exists only in the pre-room branch — so a
`too_fast`, `busy` or `not_your_seat` refusal in the lobby is invisible. Render
the store's error inside `ChatPanel` (dismissible, `role="alert"`), which covers
every phase the panel appears in and incidentally gives the lobby's own
refusals — `must_unready_first`, `player_is_ready`, `not_the_owner` — somewhere
to land.

**Placement** (`online/+page.svelte`):

- `phase === 'lobby'` → `<ChatPanel onsend={say} />` under `<Lobby />`.
- `phase === 'playing' | 'over'` → `<ChatPanel collapsible onsend={say} />`
  passed to `GameBoard` as a `chat` snippet, rendered below the chain.
  `GameBoard` already takes optional snippets (`banner`, `gameOver`), so the
  bot route passes none and gets no panel — which is how the non-goal stays a
  non-goal rather than a runtime check.

**Copy** (`vi.js`): `chatTitle: 'Trò chuyện'`, `chatPlaceholder: 'Nhắn tin…'`,
`chatEmpty: 'Chưa có tin nhắn nào.'`, `chatUnread: '{n} tin mới'`,
`chatAuthorLeft: 'Đã rời phòng'`. Reuse `t.submit` for the send button.

## Related Code Files

- Create: `web/src/lib/components/ChatPanel.svelte`
- Modify: `web/src/lib/stores/game.svelte.js` — `chat`, two arms, the `default`
  arm, `kept`, `clearChat()`, `CHAT_WINDOW`
- Modify: `web/src/lib/ws/messages.js` — `sendChat(text)` builder
- Modify: `web/src/lib/components/GameBoard.svelte` — a `chat` snippet prop
- Modify: `web/src/routes/online/+page.svelte` — `say(text)`, `clearChat()` in
  the mount/unmount effect, the panel in both phases
- Modify: `web/src/lib/i18n/vi.js`

## Implementation Steps

1. Add the `sendChat` builder.
2. Add `chat`, the two arms with the window trim, the `default` arm,
   `clearChat()` and the `kept` entry to the store. Write the store test for
   `atMs` being a number here, not in Phase 4 — `initialState()` is typed
   `@returns {any}`, so `svelte-check` cannot catch a bigint or a misnamed
   field, and the `deadlineUnixMs` precedent is guarded by a test for exactly
   that reason.
3. Write `ChatPanel.svelte`: list, then the input with one-way mirroring and the
   composition guard, then the collapsible header, then the error line.
4. Add the `chat` snippet to `GameBoard`, render the panel from the online route
   in both phases, and call `clearChat()` where the route calls `reset()`.
5. Add the copy.
6. `npm run check`, then `npm test`.

## Success Criteria

- [x] A message typed in the lobby appears on both screens, in order.
- [x] Own messages are visually distinct; the other player's carry their name;
      a departed author's carry the "left the room" label.
- [x] A reload shows that player's conversation, and the client list matches the
      server's window exactly — no duplicate, no drift.
- [x] The list stops growing at the window; the unread badge cannot exceed it
      and is reset by a history replace.
- [x] Navigating away and creating a new room shows an empty panel.
- [ ] Enter mid-composition does not send, and a Vietnamese word typed through
      Telex arrives whole. *(guard implemented; harness cannot synthesise
      composition events — see plan.md, Verification gaps)*
- [x] Send stays disabled for whitespace and for a pasted zero-width character.
- [x] A message containing `<b>x</b>` renders as those characters.
- [x] A `too_fast` refusal is visible in the lobby.
- [x] The panel is collapsed on the board and shows an unread count; the bot
      screen has no panel at all.
- [ ] No horizontal page scroll at 360px wide; the list scrolls, the page does
      not. *(bounded by CSS, checked by hand rather than asserted)*
- [x] `npm run check` reports zero errors.

## Risk Assessment

The composition guard is the one that bites: without it Vietnamese input is
unusable and the bug reads as "chat drops words". The trap is reaching for
`bind:value` to drive the disabled state — that is the write-back the comment in
`WordInput.svelte` warns about. Signal: accents mangle while typing, or the
Phase 4 diacritic case fails. Response: one-way mirroring as specified; if the
disabled state proves fiddly, enable Send unconditionally and let the server
drop the empty message rather than reintroducing a controlled input.

The unread count is client-only, so if it proves fiddly it can be dropped
without touching the server or the contract.
