# Rooms of two to four players

Status: **done** — 2026-09-08. Phases 1–5 delivered and verified, except the
Playwright suite, which could not run on this machine: the browser download
times out here. The specs are written and parse; they have not been executed.

A room seats up to `maxPlayers` (4, one constant) instead of exactly two. The
owner starts once every guest is ready; two seated players is enough. A player
who fails their turn is eliminated and the rest play on, last standing wins.

## The rules this settles

| Question | Answer |
|---|---|
| Room size | 2–4 seats. `maxPlayers` is one server constant, sent to the client in `RoomState` so the lobby renders whatever the server allows. |
| Starting | Owner only, and only when every other seated player is connected and ready. Two players is the minimum. |
| Kicking | Owner only, names a seat, refused when that player is ready — the existing rule, now per-target. |
| Failing a turn | The player who runs out of clock is **eliminated**; the syllable and the used set survive, the turn passes to the next living player. Last standing wins. |
| A dead-end position | The player to act still burns their clock and goes out, as today. Everyone after them is then eliminated at once rather than each waiting out a turn they cannot answer — which leaves the player who closed the position standing, exactly the 1v1 outcome. |
| Disconnecting mid-game | The seat's clock keeps running, so a player who drops on their own turn can time out and go out normally. Otherwise their reconnect window decides it: come back and play on, or be eliminated when it expires. |
| Being eliminated | They keep their seat and watch: live chain, live turns, chat still works, no word input. Everyone lands back in the same lobby when the game ends. |
| Game over | Full standings — every player, their score, ranked by finishing order (last standing first, then reverse elimination order). |

## Wire contract

This breaks the two-player shape of `RoomState`, `TurnUpdate`, `GameOver` and
`OpponentLeft`, so `ProtocolVersion` goes to **2** and the retired field tags go
to `reserved`. That is what the version number is for: an old client is refused
with a readable error rather than decoding a frame that means something else now.

## Phases

| # | Phase | Depends on |
|---|---|---|
| 1 | [Engine: elimination and standings](./phase-01-engine-elimination.md) | — |
| 2 | [Wire contract and codegen](./phase-02-wire-contract.md) | — |
| 3 | [Room: N seats, N grace windows](./phase-03-room-seats.md) | 1, 2 |
| 4 | [Client: the room as a list of players](./phase-04-client.md) | 2 |
| 5 | [Tests and docs](./phase-05-tests-docs.md) | 1–4 |

All five are complete. `go vet`, `go test ./... -race` and `npm test` pass;
`npm run check` reports no problems. `npx playwright test` is blocked on the
browser download, not on the code.

## Acceptance criteria

- A room seats 2–4; a fifth join is refused with `room_full`.
- Owner's start button is refused until every other seated player is ready.
- The owner can kick any named guest who is not ready.
- A 4-player game eliminates on timeout and continues; the last player standing
  wins and everyone sees the same standings.
- An eliminated player still sees the board and can chat, and returns to the
  lobby with the others.
- 1v1 behaviour is unchanged end to end, including reconnect and vs-bot.
- `go vet`, `go test -race`, `npm test` and the Playwright suite all pass.
