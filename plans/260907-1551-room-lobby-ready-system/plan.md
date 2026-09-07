# Room lobby with owner/guest roles and ready-up

**Status:** delivered

## Outcome

An online room is a persistent lobby rather than a wrapper around one game. The
first occupant is the owner, the second is the guest. The guest readies; the
owner starts once they are; a finished game returns both to the lobby, where the
guest must ready again. Players can leave, the owner can kick an unready guest,
and an owner who leaves hands the room to whoever is left.

## Decisions taken

- **The owner has no ready flag.** Pressing Start *is* their readiness, so only
  the guest can be ready, blocked from leaving, or kicked.
- **Room lifetime:** closes when the last seat empties, or after 10 minutes in
  the lobby with no game started. A running game is bounded by the turn clock.
- **The rematch handshake is retired.** `RequestRematch`/`RematchState` stop
  being sent and their proto tags are reserved; the lobby is the only place a
  next game is negotiated.
- **A kick only frees the seat.** The kicked player may rejoin with the code.

## Constraints

- Proto changes are additive; retired tags go to `reserved`, never reused.
- The engine stays two-player; `seats [2]*seat` stays the room's shape.
- Readiness, roles, start and kick are server state. The client renders one
  snapshot message and derives nothing.
- Only the room goroutine touches room state; new actions arrive as inputs.
- Every lobby action authorizes against the seat, not the claimed id: a room
  code is a shared secret by design.
- Vietnamese copy stays in `web/src/lib/i18n/vi.js`; error codes stay UI keys.

## Non-goals

Bot games (start immediately, no readiness), spectators, more than two seats,
kick bans, accounts, and resuming into a room whose game already finished.

## Acceptance criteria

1. A guest who joins sees a lobby, not a board. No engine exists until Start.
2. Start is refused unless the guest is seated, connected and ready.
3. Game over returns both to the lobby with the guest unreadied; a second game
   needs a fresh ready.
4. Leave while ready is refused; leave while unready frees the seat and the room
   survives.
5. Kicking a ready guest is refused; kicking an unready one frees the seat and
   tells them why.
6. An owner who leaves, or drops for good, promotes the other player, who is
   unreadied and can start once someone joins.
7. The last player out closes the room; an idle lobby closes on the TTL.
8. A reconnect lands back in the lobby with roles and readiness intact.
9. Start racing an unready or a leave, and a kick racing a ready, resolve to an
   error for the loser and never a half-started game.

## Phases

1. **Wire contract.** `SetReady`, `StartGame`, `KickPlayer`, `LeaveRoom`,
   `RoomState`; reserve the rematch tags; regenerate both trees; extend the
   cross-language fixtures.
2. **Room lifecycle.** Owner/guest roles, ready flag, start, kick, leave,
   promotion, idle TTL, resume into a lobby, retire the rematch path. Route and
   rate-limit the new messages in `session.go`.
3. **Client.** A `lobby` phase in the game store driven by `RoomState`, a Lobby
   component, retire `RematchPrompt`, copy, and the `/online` route wiring.
4. **Tests.** Room tests for each acceptance criterion, store tests for the new
   phase, e2e for join → ready → start → play → back to lobby, leave, kick and
   promotion.
5. **Docs.** The online-play section of the README.
