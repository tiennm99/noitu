# wsapi fixes

Scope: `server/internal/wsapi/` only, no commit. All listed findings are fixed; nothing skipped.

## What changed per finding

wsapi review:
1. Stale token resuming into a reused seat. `seat.heldFor` records the connection whose drop opened the window. `handleResume` accepts only the live prior or the connection the window is held for. A kicked player is answered `session_not_resumable`, and tokens are not revoked at kick.
2. Inputs queued behind a room's last one. `run` now registers `cancel` last (so it runs first) and `refusePending` first (so it runs last). It answers joins with `room_not_found`, resumes with `session_not_resumable`, create/bot start with `room_start_failed`, submit/resign/claim with `not_in_a_game`, and lobby/chat/report with `not_in_a_room`.
3. Control notices in the lossy inbox. Resign is charged on `submitLimiter`. `leaveRoom` uses the new blocking `room.sendReliably`. `attach` calls it from its own goroutine so two rooms cannot wait on each other. `resumeFrom` answers `busy` for a live but full room and `game_already_over` only for a finished one.
4. Quick-match `autoStart` outliving its pairing. The first `handleJoin` consumes the flag, including on early returns.
5. Idle window. It restarts only when the room changed (`lobbyChanged` read before the broadcast). It also starts or stops when the room crosses between lobby and game, because `beginGame` does not set `lobbyChanged`. A ready toggle counts as activity, as decided.
6. IPv6 and unmapped-IPv4 keying. `limiterKey` unmaps IPv4-mapped addresses, drops the zone, and folds IPv6 to its /64. `clientIP` applies it, so the join limiter, the connection cap and the room budget share one key.
7. Quick match while draining. `hub.quickMatch` returns `errDraining` first.
8. No `default` arm. It now answers `unknown_message`. The code is in `errcodes.go`, and `vi.js` already has it (added by the web implementer).
9. `TestChatDoesNotKeepARoomAlive` now fails if the idle close comes later than `IdleFor` + 200ms.
10. `TestIdleRoomReleasesItsSeats` now requires `not_in_a_room` after the idle close.
11. `TestPointKindMappingIsExhaustive` now checks each kind maps to the wire name derived from `String()`.
12. A resume calls `hub.cancelQuickMatch`.
13. `TestOneConnectionCannotStrandRooms` uses `awaitNoRooms`.

Security review:
- H1a. `session.run` arms a 10s `helloTimeout` timer that sends `handshake_required` and closes the socket. `handleHello` stops it. Tests override it through `hub.helloTimeout`, which is unexported.
- H1b. `hub.roomLimiter` is a keyed limiter on the client address. It sits alongside the per-connection budget, is charged in `allowRoom`, and is swept in `sweepLimiters`. Budget is 0.5/s with burst 30 (`addressRoomsPerSecond`, `addressRoomBurst`). The comment gives the NAT and no-trusted-proxy reasoning, and notes that 0.5/s over the 10 minute idle window is 300 rooms, about 30% of the default ceiling. The per-IP connection cap default is unchanged.
- L1. Same as wsapi #6.
- L2. New `corpuslog.go`: one process-wide bucket (20/s, burst 100) in front of all three `word_rejected` and `word_reported` sites. Suppressed lines feed a new expvar `noitu_corpus_log_suppressed`. The next line to get through carries `suppressed_before=N`. The per-session limit is kept.
- L3. `Server.ServeHTTP` sets `X-Content-Type-Options: nosniff`, `Content-Security-Policy: frame-ancestors 'self'` and `Referrer-Policy: strict-origin-when-cross-origin` on every response. The static handler lives in `server.go`, so `main.go` is untouched. HSTS is left to the proxy.
- N1. `blankLetters` drops U+115F, U+1160, U+3164, U+FFA0, U+2800, and the Khmer inherent vowels U+17B4 and U+17B5. A name made only of these falls back to the default nickname.

Server-core #3: `sanitizeText` maps every `unicode.IsSpace` rune to `' '` first. This also turns VT, FF, NEL and U+2028/2029 into spaces where they used to be dropped. "ngữ pháp" typed with NBSP now reaches the engine as two syllables.

Web #1: a resume into a lobby replays the seat's own GameOver right after RoomState, in the seat's own rendering. `broadcastGameOver` now renders for every human seat. A detached seat keeps its version in `seat.missedResult`. It is cleared when replayed, when the game overtakes it, or when the next game begins. The replay is queued in `room.resumeReplays` and flushed by the run loop right after `broadcastRoomState`.

## Resume frame order for the web client

Resume into a lobby after a game ended during the absence: `welcome`, `chat_history`, `room_state`, `game_over`.

- The other seats also get their own `room_state` broadcast.
- A resume into a running game is unchanged: `welcome`, `chat_history`, `game_started`, `turn_update` (if a move exists), then `room_state`.
- A resume into a lobby whose game the player saw live gets no extra frame.
- The replayed GameOver has the same shape as a live one. A client that already handles `game_over` while sitting on a lobby screen needs no change beyond accepting it after `room_state`.
- Resume errors: `busy` is now a possible answer to a resume (a full inbox). `unknown_message` is new. Both are already keys in `vi.js`.

## Verification

- `go vet ./...`, `gofmt -l .` and `golangci-lint run ./...` are all clean (0 issues).
- `go test ./internal/wsapi -race -count=3` passes (96s).
- `go test ./... -race -count=1` passes, all packages.
- Old-behaviour check: I copied the module to a scratch directory and reverted each fix by hand, one at a time. The corresponding test failed for every one of these:
  - heldFor, refusePending, resign limiter, reliable send, busy answer, autoStart, idle rule
  - limiterKey (both the mapped-address and /64 halves), quick-match drain, default arm, hello timer (two variants), address room budget, corpus limiter, headers
  - NBSP mapping, blank letters, missed GameOver, resume dequeue
  - chat resetting idle (the tightened chat test), `detachAll` removal, swapped PointKind arm
- Existing tests changed:
  - `TestFrameFloodClosesTheConnection` reads until the close instead of five frames, because in-burst empty frames now get `unknown_message` replies.
  - `newTestServer` takes optional `func(*Server)` options.
- No bare sleeps as synchronisation:
  - The idle tests use pacing sleeps with wall-clock assertions in the direction that a stall cannot fool.
  - The hello test orders itself through the silent socket's close.
  - The handler-level tests wait on the outbox.

## Notes for others

- `docs/deployment.md` (docs owner): document the new `noitu_corpus_log_suppressed` counter, the 10s hello deadline, and the per-address room budget. The per-IP connection cap default is unchanged, so H1's deployment half (set `NOITU_TRUSTED_PROXIES` and `NOITU_MAX_CONNECTIONS_PER_IP`) is still an operator task.
- Limitation in the exit fix: `room.send` checks `ctx.Done` and then enqueues, so a send that passes the check exactly as the room exits can still land after the final drain. This is a much narrower window than before (nanoseconds, not the whole queue), and closing it fully would need a lock on the hot path.
- No proto change.

Status: DONE
Summary: All 13 wsapi findings, security H1, L1, L2, L3 and N1, server-core #3 and the web resume GameOver replay are implemented in `server/internal/wsapi/` with tests. Vet, gofmt, golangci-lint, `wsapi -race -count=3` and the full suite are clean.
Concerns/Blockers: none blocking. The tiny residual window in room exit is noted above. The deployment half of H1 (trusted proxies and per-IP cap) still needs the operator.
