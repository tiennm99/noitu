# Phase 4 — Client: the room as a list of players

## Store — `web/src/lib/stores/game.svelte.js`

`opponentName` / `opponentScore` / `opponentPresent` / `opponentReady` /
`opponentConnected` / `isOwner` / `isReady` / `opponentLeft` all collapse into:

```js
/** @type {PlayerSlot[]} */ roomPlayers: [],   // from RoomState
/** @type {PlayerScore[]} */ gamePlayers: [],  // from GameStarted / TurnUpdate
/** @type {PlayerScore[]} */ standings: [],    // from GameOver
maxPlayers: 4,
minPlayers: 2,
graceMs: 0,
turnPlayerId: '',
/** @type {{ playerId, name, isMe, reason, suggestions } | null} */
lastElimination: null,
```

with derived getters `me`, `isOwner`, `isReady`, `myScore`, `iAmEliminated`.
A `turnUpdate` with no `played` is an elimination advancing the turn — push
nothing to the chain, apply the rest.

## Components

| File | Change |
|---|---|
| `Lobby.svelte` | Renders `roomPlayers` plus empty slots up to `maxPlayers`. Kick button per row, on any guest who is not ready. Hints keyed off how many are seated and how many are ready. |
| `ScoreBoard.svelte` | A row per player, active row = `turnPlayerId`, eliminated rows struck through. Replaces the two-sided layout. |
| `PlayerStatus.svelte` | Replaces `OpponentStatus.svelte`: one line per disconnected player counting down `graceMs`, plus an "eliminated" banner for this player. |
| `GameOverPanel.svelte` | Standings table from `standings`; `myScore` read from the row where `isMe`. Suggestions still only for a player who did not win. |
| `ChainHistory.svelte` | Labels each entry with its player's name via `playerId`. |
| `GameBoard.svelte` | Drops `opponentLabel`; word input hidden while `iAmEliminated`. |
| `online/+page.svelte` | `kick(playerId)`. |
| `messages.js` | `kickPlayer(playerId)`, `PROTOCOL_VERSION = 2`. |
| `history-export.js` | Names each mover from the player list rather than "you / opponent". |
| `i18n/vi.js` | New keys: standings, eliminated, spectating, per-player disconnect, `need_more_players`, `cannot_kick_self`, `player_offline`. |
