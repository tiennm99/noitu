# Phase 2 — Wire contract and codegen

`proto/noitu/v1/game.proto`, regenerated into `server/gen/` and
`web/src/lib/proto/`. `ProtocolVersion` → **2**.

## New messages

```proto
// PlayerSlot is one seat in the room, rendered for one recipient.
message PlayerSlot {
  string player_id = 1;   // "p1".."p4" — stable within a room
  string name = 2;
  bool is_me = 3;
  bool is_owner = 4;
  bool ready = 5;
  bool connected = 6;
}

// PlayerScore is one player in a running or finished game.
message PlayerScore {
  string player_id = 1;
  string name = 2;
  bool is_me = 3;
  uint32 score = 4;
  bool eliminated = 5;
  bool connected = 6;
  uint32 rank = 7;        // final placing, 1 = winner; 0 while in play
}

// PlayerEliminated is one player leaving a game that is still running.
message PlayerEliminated {
  string player_id = 1;
  string name = 2;
  bool is_me = 3;
  GameEndReason reason = 4;
  // What the position still had, sent only to the player who went out.
  repeated string suggestions = 5;
}
```

## Changed messages

| Message | Change |
|---|---|
| `RoomState` | `reserved 2, 4, 5, 6, 7, 8` (`i_am_owner`, `i_am_ready`, `opponent_*`). Adds `repeated PlayerSlot players = 9`, `uint32 max_players = 10`, `uint32 min_players = 11`, `uint32 grace_ms = 12`. `room_code` and `can_start` stay. |
| `GameStarted` | Adds `repeated PlayerScore players = 7`, `string turn_player_id = 8`. |
| `TurnUpdate` | `reserved 6, 7` (`my_score`, `opponent_score`). Adds `repeated PlayerScore players = 9`, `string turn_player_id = 10`. `played` may now be absent — an elimination advances the turn without a word. |
| `GameOver` | `reserved 3` (`my_score`). Adds `repeated PlayerScore standings = 6`, ordered by rank. |
| `PlayedWord` | Adds `string player_id = 6`, so a four-way chain says who played what without matching names. |
| `KickPlayer` | Adds `string player_id = 1` — the owner now names a seat. |
| `OpponentLeft` | Retired. `ServerMessage` reserves tag 8; presence travels on `RoomState.players[].connected` with `RoomState.grace_ms`, which is broadcast mid-game already. |
| `ServerMessage` | `reserved 2, 3, 8, 11`; adds `PlayerEliminated player_eliminated = 15`. |

`i_am_owner` / `i_am_ready` go because the recipient's own row already carries
them: two encodings of one fact are two ways for a client to disagree with the
server.

## Regeneration

`buf` is not on this machine — `go install github.com/bufbuild/buf/cmd/buf@latest`
first, then `make proto`. Both generated trees are committed.
