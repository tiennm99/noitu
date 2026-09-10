# Online room experience — UI/UX review

Read-only advisory. Scope: `/online` surface, lobby, chat, connection, turn ownership, mobile.
Files read: `web/src/routes/online/+page.svelte`, `Lobby`, `ChatPanel`, `ScoreBoard`, `PlayerStatus`, `RoomCodePanel`, `ConnectionBadge`, `NicknameInput`, `GameBoard`, `WordInput`, `CountdownRing`, `GameOverPanel`, `i18n/vi.js`, `ws/{connection.svelte.js,client.js,messages.js}`, `stores/{game,settings}.svelte.js`, `room-code.js`, `app.css`, `+layout.svelte`, plus `server/internal/wsapi/{session,room,hub,nickname}.go` for the states the UI must cover.

## Verdict

The happy path is genuinely good: invite link → seated in 1 tap, room code is designed to be read aloud, chat/board/scoreboard coexist sanely at both breakpoints, and turn ownership already carries a text cue rather than colour alone. The damage is concentrated in two places — **the pre-room screen has no connection or in-flight feedback at all** (create/join can hang forever with a live-looking button), and **the lobby has no connection badge and pushes every server refusal into the chat panel below the fold**, so `Bắt đầu`/`Sẵn sàng` can look broken. Third theme: the invite-link path never asks for a nickname, so the invited friend is seated as "Người chơi" with no in-room way to fix it.

## What works (do not "fix")

- **Room code design.** Alphabet drops 0/O/1/I/L (`room-code.js:236`), display grouped 3+3 (`RoomCodePanel.svelte:18`), `aria-label` spells it out letter by letter (`:48`), and `normalizeRoomCode` accepts a pasted invite URL, spaces, dots and hyphens in any case (`room-code.js:261-267`). Typo recovery works: malformed → local `codeError` under the field (`+page.svelte:161-163,293`), well-formed but dead → server `room_not_found` in Vietnamese with the typed code still in the field (`:262-263`).
- **Invite link auto-joins** instead of making the guest press a button they did not ask for (`+page.svelte:99-101`). One tap from a group chat to a seat.
- **Request latch.** `pending` survives a not-yet-open socket and flushes on handshake (`+page.svelte:151-157`), so create/join during connect is not lost, and repeated taps while connecting collapse into one request.
- **Word input is gated on the socket, not just the turn** (`WordInput.svelte:16-18`) — a reconnecting player cannot type into a black hole.
- **Focus etiquette.** A turn arriving does not steal focus from the chat field (`WordInput.svelte:36-59`); IME composition is respected in both fields (`WordInput.svelte:79`, `ChatPanel.svelte:108`), and both are uncontrolled so Telex/VNI accents survive.
- **Unread chat without hijacking.** Folded panel + count badge, counted off the server's running total so the cap cannot kill the badge, seeded at mount so a fresh panel does not claim old mail (`ChatPanel.svelte:48-51,68-71`); autoscroll only when already at the bottom (`:76-81`).
- **Grace countdown for a dropped opponent** as a top-of-board banner with `role="status"` (`PlayerStatus.svelte:197-206`) — the single best "what am I waiting for" element in the app.
- **Chat lines coloured per seat from server-supplied seat index**, never by matching display names, `chatAuthorLeft` for a cleared seat (`ChatPanel.svelte:91-94,150`).
- **Lobby copy already states the rule** for every role/state combination (`Lobby.svelte:90-100`) and `leave` is disabled-with-reason rather than hidden (`:128-133`).
- Server refusals are all keyed to Vietnamese sentences in one file (`vi.js:177-211`) — no raw codes leak.

---

## Findings

### P1 — blocks or strands a player

**P1-1. Create/join can hang forever with no status, no spinner, no disabled button.**
`+page.svelte:256-297` — the pre-room branch renders title, nickname, error, `Tạo phòng`, code form, back link. Nothing renders `pending` (`:42`) and nothing renders `connection.status`. `ConnectionBadge` is mounted **only** inside `GameBoard.svelte:36`, i.e. never on this screen.
Player experience: server down / captive wifi / cold start. Tap `Tạo phòng` → the button stays enabled, the screen does not change, forever. `client.js:198-212` retries with backoff capped at 8s and never gives up, so status sits at `reconnecting` and no error is ever produced to render at `:262`. The player has no way to tell "connecting" from "broken".
Secondary harm: with the socket already open (after `Rời phòng`, which keeps the client alive — `+page.svelte:195-199` sends `LeaveRoom` only), each impatient tap sends a real `CreateRoom` (`:155`); `roomsPerSecond = 0.2, roomBurst = 5` (`server/internal/wsapi/session.go:54-55`) so the 6th tap answers `too_many_rooms` — "Bạn tạo phòng quá nhanh" to a player who thinks they tapped nothing.
Fix: (a) mount `<ConnectionBadge />` in the pre-room branch, next to `h1`; (b) `disabled={!!pending}` on both `Tạo phòng` (`:266`) and the join submit (`:291`), with the label swapped to `t.connecting` while pending; (c) add copy for a stall, e.g. `connectStalled: 'Chưa kết nối được máy chủ. Kiểm tra mạng rồi thử lại.'`, shown when `pending && connection.status !== Status.OPEN` for >5s.

**P1-2. The lobby has no connection badge, and lobby buttons fail silently when the socket is down.**
`Lobby.svelte` renders no connection state; `GameBoard` (the only `ConnectionBadge` host) is not mounted in the `lobby` phase (`+page.svelte:243-245`). All four lobby actions discard the send result: `ready` `:183`, `start` `:188`, `kick` `:192`, `leave` `:196` — and `send()` returns `false` when the socket is not open (`connection.svelte.js:40-42`, `client.js:305`).
Player experience: wifi blips in the lobby. Tap `Sẵn sàng` → the button does not change (state is server-owned by design, `Lobby.svelte:117-122`), no error, no badge. Tap `Bắt đầu` → nothing. The room looks dead and the player leaves.
Fix: mount `<ConnectionBadge />` in the lobby (top of `Lobby.svelte`'s `<section>`, or in the `.pane.game` wrapper so it covers both phases), and make the lobby buttons honour the send result: `disabled={connection.status !== Status.OPEN}` on the primary and, on a `false` return, set a local notice using existing copy `t.reconnecting`.

**P1-3. The invite-link path never asks for a nickname, and there is no way to set one from inside the room.**
`+page.svelte:87-108` connects and joins on arrival; the comment at `:94-98` states the nickname is typed on this screen and carried once by `Hello` — but the invite branch bypasses that screen entirely. `settings` defaults `nickname` to `''` (`settings.svelte.js:98`), the server falls back to `"Người chơi"` (`server/internal/wsapi/nickname.go:113`) plus a numeric suffix when taken. `NicknameInput` is rendered only in the `else` branch (`:260`), so it is gone the moment you are seated.
Player experience: a first-time invited friend is seated as "Người chơi 2", scoreboard/chat/standings all show that, and the only route to a real name is `Rời phòng` → name field → retype the code (or reopen the link). In a 3-4 player room with two anonymous joiners the chat colours are carrying identity on their own.
Fix, cheapest: render `<NicknameInput />` inside the lobby when `!settings.state.nickname`, and on blur re-`Hello`… — but `Hello` is once-per-socket, so instead: on the invite path, if `settings.state.nickname` is empty, show the name field + a single `Vào phòng` button first (hold `pending` instead of flushing) rather than auto-joining. One extra tap only for players who have no name yet; returning players keep the 1-tap link. Alternative if a rename in-room is wanted, it needs a server message and is out of scope here.

### P2 — noticeable friction

**P2-1. Lobby refusals render in the chat panel, which on a phone is below the fold.**
`+page.svelte:252` passes `errors={!playing}`, so `game.state.error` is drawn by `ChatPanel.svelte:159-166`. In the stacked layout the chat pane comes after the game pane (`:248`) under a divider (`:328-331`). A 360×640 lobby with 4 seats is already ~600px tall (room-code panel ~160 + count 20 + 4×48 seats + gaps + hint + actions 54 + leave 34), so the error box is off-screen.
Player experience: owner taps `Bắt đầu` with one guest not ready → `not_everyone_ready` ("Vẫn còn người chưa sẵn sàng") appears somewhere they cannot see; the button appears broken. Same for `too_fast`, `player_is_ready`, `player_offline`, `must_unready_first`, `protocol_version_mismatch` ("Hãy tải lại trang" — the one message that must be seen).
Fix: render the error where the action is. Add the error block to `Lobby.svelte` directly above `.actions` (reuse `GameBoard.svelte:44-49` markup incl. the 44px dismiss button), and drop `errors` for the lobby case so it is not shown twice.

**P2-2. During a mobile game the chat toggle is below the whole chain.**
DOM order: `GameBoard` (`+page.svelte:229-246`) ends with `<ChainHistory />` (`GameBoard.svelte:82`), then the chat pane. So the collapsible header — the only way to read chat and the only place the unread badge lives (`ChatPanel.svelte:127-133`) — sits under a list that grows one row per turn.
Player experience: turn 15, a teammate asks something, the badge exists but is two screens down; the player never scrolls there mid-turn. The unread mechanism is well built and effectively unreachable.
Fix: hoist the toggle. Put the collapsible chat header (title + badge) into `GameBoard`'s `.top` row beside `ConnectionBadge` — or move `.pane.talk` before `ChainHistory` in the stacked layout via `order` on a grid/flex parent. Minimum viable: a small pill button in `.top` showing the unread count that scrolls the chat panel into view.

**P2-3. Clipboard failure is invisible, and the invite URL is never shown as text.**
`RoomCodePanel.svelte:24-35` swallows every clipboard rejection; `navigator.clipboard` is undefined outside a secure context, which is exactly the self-hosted `http://<lan-ip>:port` case this single-origin Go binary invites. `navigator.share` fails the same way (`:37-43`).
Player experience: tap `Sao chép mã` / `Sao chép liên kết mời` → the label never changes to "Đã sao chép", nothing is on the clipboard, no explanation. The 6 characters are on screen (recoverable), but the *link* is not — it exists only inside `inviteUrl` (`:12-14`).
Fix: in the `catch`, select the code node (`getSelection().selectAllChildren(codeEl)`) and set a state that swaps the label to new copy, e.g. `copyFailed: 'Không sao chép được. Hãy chọn và sao chép thủ công.'`; and render `inviteUrl` as a selectable `<p class="link">` (or `<input readonly>`) under the buttons so a link is always shareable by hand.

**P2-4. "It is your turn" is the smallest, faintest text on the board, and is not announced.**
`GameBoard.svelte:57` renders `turnLabel` in `.who` — `font-size: 0.85rem; color: var(--text-muted)` (`:117-121`) — right next to a 1.6rem syllable (`:134-137`). Same styling whether the label says "Đến lượt bạn" or "Đến lượt Lan…". No `aria-live`, so nothing is spoken on a turn change. `ScoreBoard.svelte:73-78` marks the active row with border + background only (colour-only). The disabled word field keeps the inviting placeholder "Nhập từ của bạn" (`WordInput.svelte:111`) even when it is somebody else's turn.
Player experience: on ≥900px the player's eyes are in the right-hand chat column; the turn arriving is a small grey line and a colour change in a score row, and autofocus is deliberately declined while they type in chat (`WordInput.svelte:36-59`). Turns get burned. Conversely a player types a word into a dead input and only then notices.
Fix (three cheap changes): (1) `class:mine={game.state.myTurn}` on `.who` → `color: var(--text); font-weight: 700; font-size: 1rem;`; (2) `aria-live="polite"` on the turn indicator `<p>`; (3) placeholder driven by ownership — `placeholder={enabled ? t.wordInputPlaceholder : fill(t.playerTurn, { name })}` so the dead input states whose turn it is. Optional non-colour cue on the scoreboard: a `▸` marker or a bottom border rule on `.side.active`.

**P2-5. A player who drops mid-turn watches the clock run out with a 0.85rem pill and no retry.**
`CountdownRing.svelte:13,20-27` keeps animating off `deadlineMs` regardless of connection; `ConnectionBadge.svelte:128-138` is a muted 0.85rem pill in the corner. Reconnect delay is up to 8s and jittered (`client.js:18,199-203`), and there is no manual retry: `connection.svelte.js` exposes only `connect` (no-op if a client exists), `disconnect`, `forgetSession`.
Player experience: tunnel/lift. The ring keeps counting, the input greys out, and a player who is back online cannot force an attempt — they wait out the backoff and lose the turn. Nothing says the seat is being held (`graceMs` is known to the client — `game.svelte.js:105,268` — but only ever spent on *other* players' banners).
Fix: when `connection.status !== Status.OPEN`, render a banner above the input (same slot as `PlayerStatus`) with existing `t.reconnecting` plus a `Thử lại` button; add `reconnectNow()` to `connection.svelte.js` (cancel `reconnectTimer`, reset `attempt`, `open()`) — `client.js:206-212` already has all the pieces. Freeze or dim the ring while the socket is down so it stops implying a live clock.

**P2-6. In the lobby, a dropped player has no countdown and the hint keeps lying.**
`PlayerStatus` is mounted only as `GameBoard`'s banner snippet (`+page.svelte:232-234`), so in the `lobby` phase the grace countdown is not rendered at all — a dropped player is just `Mất kết nối` text on their row (`Lobby.svelte:56-57`). The hint chain (`Lobby.svelte:90-100`) ignores connectivity, so a ready guest keeps reading "Đang chờ chủ phòng bắt đầu…" while the owner is gone.
Fix: render `<PlayerStatus />` in the lobby too (it is phase-guarded internally and would just show the away countdowns), and add a branch to the hint: when the owner's row has `!connected`, show new copy `ownerAway: 'Chủ phòng đang mất kết nối. Chờ một chút hoặc rời phòng.'`.

**P2-7. Chat arriving in the lobby is completely silent.**
`collapsible={playing && !wide}` (`+page.svelte:251`), so in the lobby the panel is never collapsible → `open` is always true (`ChatPanel.svelte:41`) → `unread` is forced to 0 (`:51`) and no badge exists. But in the stacked lobby the log is below the fold (see P2-1).
Player experience: "sẵn sàng chưa?" is typed and nobody who is looking at the seat list ever knows.
Fix: make the panel collapsible whenever `!wide` (drop the `playing &&`), which restores the badge in the lobby, and combine with the hoisted toggle from P2-2.

**P2-8. The ready button's label is the same string as the "not ready" *state*.**
`Lobby.svelte:121` → `game.isReady ? t.unready : t.ready`, and `vi.js:99,101` set `unready: 'Chưa sẵn sàng'` and `notReady: 'Chưa sẵn sàng'` — identical. Styling compounds it: `.actions .primary.on` turns the ready player's button grey/muted (`Lobby.svelte:271-274`), so a ready player sees a greyed button reading "Chưa sẵn sàng" and reasonably concludes their readiness did not register.
Fix: `unready: 'Bỏ sẵn sàng'` — which also matches the two messages that already use that verb (`unreadyToLeave` `vi.js:111`, `must_unready_first` `:189`). Keep the muted style but the label now reads as an action.

**P2-9. Touch targets below 44px on the three controls that matter most in a room.**
- Chat collapsible header — the only way to open chat during a mobile game: `padding: 0`, `font-size: 0.85rem` → ~19px tall (`ChatPanel.svelte:204-223`).
- Kick button, destructive: `26×26` (`Lobby.svelte:229-238`).
- Room-code actions, the whole sharing story: `padding: 8px 14px` at `0.85rem` → ~34px (`RoomCodePanel.svelte:96-102`).
- Also `Lobby.svelte:283-289` `.leave` ~34px, and the chat error dismiss `×` has no size at all (`ChatPanel.svelte:301-307`) → ~18px.
Fix: `min-height: 44px` + `padding: 10px 0` on `.header`; kick to `36×36` with a `44px` hit area via `::after` inset expansion (or `min-width/height: 44px` and shrink the glyph); `min-height: 44px; padding: 10px 16px` on the room-code and leave buttons. For the dismiss `×`, copy the existing in-repo pattern at `GameBoard.svelte:152-165` (44×44 with negative margins so the banner keeps its height).

**P2-10. Refreshing mid-game from an invite URL shows a spurious red error.**
`+page.svelte:99-107` checks `isRoomCode(code)` **before** `hasStoredSession()`, so a reload at `/online?code=ABC123` sends `Hello` **with** the stored resume token (`client.js:228`) *and* a `JoinRoom`. The resume restores the seat; the join is then refused by the room with `game_in_progress` (`server/internal/wsapi/room.go:558-562`) or `cannot_join_own_room` (`:564-568`), which lands as a red banner on the restored board (`GameBoard.svelte:44-49`). Also `resuming` is `false` on this path (`:141`), so the quiet-drop logic at `:128-136` does not apply.
Player experience: reload to recover from a glitch → you are back in your game, with "Ván đấu đang diễn ra." accusing you of something.
Fix: prefer resume when a token exists, and fall back to the invite code only when the resume is refused — the machinery already exists at `:128-136` (it detects a refused resume). E.g. keep `pending = { kind: 'join', code }` un-flushed while `hasStoredSession()`, clear it when the phase reaches `lobby`/`playing` with a matching `roomCode`, flush it in the refused-resume effect.

### P3 — polish

- **P3-1. Room code is unreachable and unlabelled once the game starts.** `+page.svelte:231` passes it as `modeLabel`, rendered as muted 0.85rem text with no label and no copy button (`GameBoard.svelte:37,102-105`); the post-game lobby is `compact`, which drops `RoomCodePanel` entirely (`Lobby.svelte:33-35`) — exactly when a replacement player needs inviting. Fix: prefix with `t.roomCodeLabel` and make it a copy button; keep `RoomCodePanel` in the compact lobby (or a one-line variant).
- **P3-2. Code glyph pairs.** `.code` uses the body `system-ui` stack with `font-variant-numeric: tabular-nums` (`RoomCodePanel.svelte:81-87`), which does nothing for letters. The alphabet keeps 5/S, 2/Z, 8/B, 6/G, U/V (`room-code.js:236`) — fine spoken aloud in Vietnamese, harder to copy from a screenshot. Fix: `font-family: ui-monospace, 'SFMono-Regular', 'Cascadia Mono', Menlo, monospace` on `.code` only.
- **P3-3. Offline marker in the scoreboard is a `title`-only `⚠`** (`ScoreBoard.svelte:31-33`) — `title` does not exist on touch and is not reliably announced. Fix: `aria-label` plus an `.sr-only` span (`app.css:100-109` already has the utility).
- **P3-4. `yourRoomCode: 'Mã phòng của bạn'`** is shown to guests too (`Lobby.svelte:34`). Fix: neutral `'Mã phòng'`, or branch on `game.isOwner`.
- **P3-5. Owner's seat is permanently painted as ready** — `class:ready={player.isOwner || player.ready}` (`Lobby.svelte:43`) gives the owner row accent-soft forever while showing no state label (`:58-63`). Defensible, but scanning the list the highlight reads as a readiness claim. Fix: a distinct `.seat.owner` treatment (left border in `--accent`) instead of reusing `.ready`.
- **P3-6. Native `confirm()` for kick and resign** (`+page.svelte:192,215) blocks the JS thread, so the countdown ring freezes while the server clock keeps running — an accidental `Đầu hàng` tap can time the turn out during the dialog. Also unstyled and, in some in-app webviews, suppressed. Fix: an inline two-button confirmation row where the button is (matches the rest of the app's styling and does not block the frame loop).
- **P3-7. Blank nickname is accepted silently** (`NicknameInput.svelte:185-193`, no `required`); the player learns their name is "Người chơi" only after being seated. Fix: `placeholder`/hint already exist — add a hint line stating the fallback, e.g. append to `nicknameHint`: "Để trống sẽ được gọi là “Người chơi”."
- **P3-8. Stale `?code=` in the URL.** `codeInput` is seeded from the query (`:67`) and the query is never cleared after a successful join under a *different* code, so a later refresh re-joins the URL's room. Fix: `replaceState` to drop the param once `roomCode` is established.

---

## Quick wins (top 5, comfort per unit of effort)

1. **`unready: 'Bỏ sẵn sàng'`** (`vi.js:99`). One string. Kills the worst "did my tap register?" moment in the lobby. (P2-8)
2. **Mount `<ConnectionBadge />` on the pre-room screen and in the lobby**, and `disabled={!!pending}` on `Tạo phòng`/`Vào phòng`. ~6 lines, closes the only true dead end in the flow. (P1-1, P1-2)
3. **Move the error block into `Lobby.svelte` above `.actions`** (copy `GameBoard.svelte:44-49`) and stop routing lobby errors to the chat panel. ~8 lines, makes every server refusal visible where the button is. (P2-1)
4. **Emphasise your turn**: `class:mine` on `.who` + `aria-live="polite"` + ownership-driven placeholder on the disabled word field. ~10 lines across `GameBoard.svelte` and `WordInput.svelte`, recovers turns lost while reading chat. (P2-4)
5. **`min-height: 44px` on the chat toggle, room-code buttons and `.leave`; size the chat dismiss `×` like the board's.** Pure CSS, four rules. (P2-9)

Next tier, worth scheduling: hoist the chat toggle out from under the chain (P2-2), the nickname-before-invite-join gate (P1-3), clipboard fallback + visible invite URL (P2-3).

## Unresolved questions

1. **Nickname on the invite path** — is the extra tap acceptable for first-time guests (gate the join behind the name field), or is a rename-in-room server message preferred? The second option changes the protocol and is outside this review.
2. **Manual reconnect** — adding `reconnectNow()` touches `ws/connection.svelte.js` and its tests. Acceptable, or should P2-5 be limited to copy plus freezing the ring?
3. **Grace window in the lobby** — does an owner who merely disconnects (not leaves) lose the seat and hand the room on at grace expiry? `room.go:1441-1457` handles it on vacate; if lobby grace does not vacate, guests can be stranded until `room_idle_closed`, which would upgrade P2-6 to P1.
4. **Whether the 900px chat column should ever collapse** — on a 900×600 laptop with a keyboard-less browser it is fine, but a 1024×768 tablet in portrait falls into the stacked branch. No defect found; flagging in case a mid-breakpoint layout is wanted.
5. **`GameOverPanel` + compact lobby stacking on 360px** — not measured against a real device; the vertical budget after standings + stats + suggestions is likely over one screen, which would put the `Bắt đầu`/`Sẵn sàng` for the next game below the fold. Worth a screenshot pass.
