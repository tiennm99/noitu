---
phase: 5
title: "Docs"
status: completed
priority: P3
effort: "30m"
dependencies: [4]
---

# Phase 5: Docs

## Overview

Chat is user-visible behaviour and a change to the wire contract, so the two
documents that describe those get a paragraph each. Nothing else moves.

## Requirements

- Functional: a reader of the README learns that rooms have chat and what its
  bounds are; the deployment doc's list of fixed timings stays true — which it
  does only because Phase 2 excludes chat from the idle timer. If that decision
  is ever reversed, this requirement is the thing that breaks first.
- Non-functional: no new documents, no duplication of what the code already
  states — link to the constants rather than restating their values twice.

## Architecture

**`README.md`, "Online play".** After the lobby paragraphs, one short
paragraph: the two players can talk, the conversation belongs to the room
rather than to a game, a player is replayed what was said while they held their
seat (so a refresh restores it and a newcomer starts at silence), and text is
sanitized by the same filter that sanitizes nicknames before anyone sees it.
Say plainly that a bot game has no chat.

**`docs/deployment.md`.** The "one timing is not configurable" paragraph is
still true — say why it survived chat: the idle window measures *no game
started*, and chatting deliberately does not reset it, so a room full of
conversation still closes on schedule. Add the two fixed chat constants
(history window and rune cap) beside it. No new section, and no env var, since
none is being added.

Check while there: does anything in either document still describe the room as
one game's container? The lobby work already fixed the obvious cases, but chat
belonging to the room is a second claim resting on the same fact.

## Related Code Files

- Modify: `README.md`
- Modify: `docs/deployment.md`

## Implementation Steps

1. Write the README paragraph in the "Online play" section.
2. Add the constants note and the idle-window clarification to
   `docs/deployment.md`.
3. Re-read both sections around the edits for a stale claim about rooms.

## Success Criteria

- [x] The README says chat exists, that it is per room, that a replay is scoped
      to the recipient's own occupancy, and that bot games have none.
- [x] `docs/deployment.md` states that chat does not reset the idle window,
      lists the fixed chat constants, and adds no env var that does not exist.
- [x] No other document contradicts either.

## Risk Assessment

Negligible on its own, but this phase is where a reversal of Phase 2's
idle-timer decision would surface as a documentation lie rather than a bug.
Signal: `TestChatDoesNotKeepARoomAlive` removed or skipped. Response: rewrite
the deployment paragraph in the same change, never after it.
