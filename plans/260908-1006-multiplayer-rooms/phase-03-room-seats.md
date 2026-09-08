# Phase 3 — Room: N seats, N grace windows

`server/internal/wsapi/room.go`, `codec.go`, `hub.go`, `session.go`,
`nickname.go`.

## Seats

- `maxPlayers = 4`, `minPlayers = 2`, `seatIDs = [maxPlayers]game.PlayerID{"p1".."p4"}`.
- `seats [maxPlayers]*seat`. `freeSeat`, `occupied`, `seatOf` already loop.
- `distinguish(name, taken)` becomes `distinguish(name, taken []string)` and
  counts up (" 2", " 3", …) until the name is unused.
- Drop `otherNickname` / `opponentSeat`; add `seatsInOrder()` yielding the
  occupied seats in seat order, which is also engine turn order.

## Lobby

- `canStart()` — no bot strategy, in lobby, at least `minPlayers` seated, owner
  connected, every non-owner seat connected and ready.
- `lobbyStart` refuses with `need_more_players` / `player_offline` /
  `not_everyone_ready`.
- `lobbyKick` reads `KickPlayer.player_id`: refuse `no_one_to_kick` when the
  seat is empty, `cannot_kick_self` when it is the owner's own, and
  `player_is_ready` when that player is ready — the existing rule, per target.
- `handleJoin` refuses a full room exactly as now.

## Grace windows

One timer is no longer enough: any number of seats can be inside their window at
once. Each `seat` gains `graceUntil time.Time`; `resetGraceTimer()` arms a single
timer for the earliest of them, and on fire every seat past its deadline is
expired together. That keeps one timer and one wakeup regardless of how many
players dropped.

A dropped seat mid-game is **not** skipped: the engine's turn clock runs for it,
so a player who drops on their own turn is eliminated by timeout before their
window is up. The window only decides whether they are still in the game after it.

## Eliminations

`applyEliminations(before int)` — compare the engine's `outOrder` length against
what it was, send each newly eliminated player's `PlayerEliminated` to every
seat (with `suggestions` only in the copy that goes to the player who went out),
then:

- game over → `broadcastGameOver` with standings;
- otherwise → `r.turnSeq++` and `broadcastTurn(nil)`, so every client learns the
  new deadline and whose turn it is without a word having been played.

The room keeps `outWire map[game.PlayerID]noituv1.GameEndReason`, because
`OPPONENT_LEFT` is a transport fact the engine never learns: a grace expiry
eliminates with `EndResigned` and is reported as `OPPONENT_LEFT`.

`GameOver.reason` stays the game-level reason — the reason the **last**
elimination happened — which keeps 1v1 wording identical.

## Broadcasts

- `broadcastRoomState` builds `PlayerSlot` rows in seat order.
- `broadcastTurn(move *game.Move)` and `sendGameStarted` build `PlayerScore` rows.
- `gameOverFor` builds standings once and sets `is_me` per recipient.
