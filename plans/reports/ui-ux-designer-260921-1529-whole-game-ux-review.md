# noitu — whole-game UX review (code-based, read-only)

Tree: `web/` at `dev` 5178a97, 2026-09-21. No browser on this host: every claim below is read off Svelte markup, CSS tokens and `vi.js`, with ratios computed from the hex in `app.css`. Prior 260910 reports checked against the tree first — their P1/P2 items (turn live region, `--border-strong`, accent darkened to `#12692f`, spacing/type ramps, `min-height` on the chain, focus on game-over, two-press resign/kick, seat colours, owner marker, monospace code, reduced-motion scroll) are all shipped and are not repeated.

## Verdict

Structurally one of the better-reasoned small game UIs I have reviewed: the board keeps syllable/clock/input fixed and lets only the chain scroll, the IME handling is right, the colour tokens hold AA in both themes (measured, table in §3), and nearly every control explains itself in `vi.js`. Two things are wrong at a level above polish: (1) the in-game "Luật chơi" link forfeits the game — the route change unmounts the board and its cleanup resigns/leaves; (2) a newcomer reaches the first running clock without ever being told the chain rule. Below those, today's additions (chips, pill, claim, suggestion/report) each work alone but together crowd the 360px board and hand the rejection row three sub-24px targets. The token set is disciplined but looks like nothing in particular — a single display face for the four hero numbers and a flatter type ramp would give it a face without a redesign.

## Top-10 ranked changes

| # | Change | Size | Screen | Why |
|---|---|---|---|---|
| 1 | In-room "Luật chơi" opens in a new tab (or `<dialog>` summary); never a same-tab route | S | board, lobby | `GameBoard.svelte:133`, `Lobby.svelte:90` are plain `<a href="/rules">`; `play/+page.svelte:39-44` cleanup sends `resign()`, `online/+page.svelte:185-212` sends `leaveRoom()` + `forgetSession()`. Reading the rules mid-game = forfeit. |
| 2 | State the chain rule before the first clock: one line + example on the landing, caption on the opening row | S | landing, board | `+page.svelte:387` tagline is the only rule text; `rulesLink` sits below the CTAs (`:397`). Opening row `ChainHistory.svelte:249-252` is dashed but unlabelled. |
| 3 | Slim the board header: badge text `sr-only` while open, rules as 44px "?" icon, room code moves under the turn prompt | M | board 360px | `GameBoard.svelte:129-154` puts badge (~105px) + code + "Luật chơi" + chat pill (~150px w/ badge) in 328px → two wrapped rows above the scoreboard. |
| 4 | Rejection row: suggestion and report become 36px-tall buttons on their own line; suggestion styled primary | S | board | `WordInput.svelte:304-313` pills are `2px` padding at 0.8rem ≈ 23px tall — under WCAG 2.2 24px minimum, and the one control that saves the turn. |
| 5 | "Bí từ" stops appearing/vanishing each handover; sits in one secondary row with "Đầu hàng" | S | board | `GameBoard.svelte:227-241` mounts the button only on `myTurn`, shifting everything under the input every turn — the exact thing `.resign`'s comment (`:437-443`) avoids. |
| 6 | Show elimination suggestions in the spectating box at knockout, not only at game over | M | board (out) | `elimination.suggestions` arrive with `playerEliminated` (`game.svelte.js:414-431`) but render only in `GameOverPanel.svelte:426-445`; a 4-seat spectator waits minutes. Also `.spectating` (`GameBoard:224`) and the `youAreOut` banner (`PlayerStatus:535`) say the same thing twice. |
| 7 | Fix `not_your_turn` for the race it actually describes; route it beside the button | S | board | Both callers are gated on `myTurn` (`GameBoard:69,82`), so the server can only say this when the turn moved mid-press. "Chưa đến lượt bạn" says "not yet" about a turn that has *passed*, in the top banner (`game.svelte.js:498`) ~250px from the button. |
| 8 | Quick-match wait: take the second counter out of the live region; nudge says it leaves the queue | S | online entry | `online/+page.svelte:494-503` `role="status"` wraps `{n} giây` → screen reader announces every second. `/play` link at default difficulty; cleanup cancels queue silently (`:207`). |
| 9 | Breakdown line: chips, only on the latest row / when a row is open; rename `dài` | M | board | `ChainHistory.svelte:113-118` prints `+10 nền · +4 chuỗi · +5 dài` at 0.75rem muted under *every* row in a 9rem-high list — three rows of history become three rows of arithmetic. |
| 10 | Visual identity: one display face for syllable/clock/score/code, ramp collapsed 9→7 steps, hover token, lifted `--bg` | M | all | §6. Nothing here is broken; it is `system-ui` at 600/700 in 8px boxes and reads as a default. |

## 1. First-run flow

Path: `/` → nickname (`NicknameInput`) → `DifficultyPicker` → "Chơi với máy" → `/play?difficulty=` → socket → `StartBotGame` → `TurnUpdate` with clock running.

- **No rule before the clock.** `+page.svelte:387` `tagline` = "Trò chơi nối từ tiếng Việt"; nothing says *what* nối từ requires. `rulesLink` (`:397`) is a muted underline under the CTAs — right that it is not a button, wrong that the *rule* itself needs a click. Newcomer's first read of the rule is `rejectMessages.WRONG_LINK` under a running clock.
  Sketch (landing, between tagline and nickname):
  ```svelte
  <p class="how">
    Tiếng đầu của từ bạn phải là tiếng cuối của từ trước —
    <span class="ex">ngôn ngữ → ngữ pháp → pháp luật</span>. Mỗi lượt có đồng hồ.
  </p>
  ```
  ```css
  .how { margin:0; font-size: var(--text-5); color: var(--text-muted); }
  .ex  { color: var(--text); font-weight: 600; white-space: nowrap; }
  ```
  Copy: `howToPlay: 'Tiếng đầu của từ bạn phải là tiếng cuối của từ trước — {example}. Mỗi lượt có đồng hồ.'`, `howToPlayExample: 'ngôn ngữ → ngữ pháp → pháp luật'`. Turn length is a server constant not known on the landing, so "có đồng hồ", not "30 giây".
- **Opening row unlabelled.** `ChainHistory.svelte:73,249-252` marks `.opening` dashed; a newcomer sees one grey word and a syllable. Add a caption on the opening row only: `openingCaption: 'Từ mở đầu — nối tiếp bằng tiếng cuối'` rendered like `.corrected` (`:310-314`). Stateless, helps every game, costs one 0.8rem line.
- **Rules discoverability is right on landing, absent on `/online` entry.** The join form (`online/+page.svelte:466-546`) has no `.rules-link`; a stranger arriving by invite link never sees one before being seated. Add the same link under `.back` (`:546`).
- **Difficulty picker: clear as a ladder, opaque as a choice.** `difficultyLabels` = Dễ/Trung bình/Khó + "Kỷ lục". Server: easy = random legal move, medium = 1-ply greedy, hard = multi-ply + 85% kill rate (`server/internal/bot/strategy_*.go`). One truthful caption each, replacing the `.best` line when best = 0:
  `difficultyHints: { EASY: 'Máy chọn từ ngẫu nhiên', MEDIUM: 'Máy chọn từ tốt nhất trước mắt', HARD: 'Máy tính trước vài nước, hay dồn bí từ' }`. Fits the 96px min column at 0.75rem over two lines.
- **Nickname hint is three sentences on a 360px screen** (`vi.js:22-23`, rendered `NicknameInput:411`). Collapse: `'Tối đa 20 ký tự. Để trống sẽ là “Người chơi”.'` — server truncation is an edge case the server already reports by displaying the sanitized name.
- **Rules page itself** (`rules/+page.svelte`): good — one anchored page, `scroll-margin-top`, live scoring constants. Two nits: `.toc a` (`:248-256`) is ~27px tall (`4px` padding, 0.8rem) — raise to `padding: var(--space-2) var(--space-3); min-height: 32px`; `.back` (`:220`) always goes `/` — when opened from a game in a new tab (fix #1) the natural action is "close", so hide `.back` when `window.opener || history.length <= 1`, or label it "Đóng" via `history.back()` fallback.

## 2. In-game board

**Hierarchy (360–420px).** Reading order in `GameBoard.svelte:128-272`: header row (badge, code, rules, pill) → `ScoreBoard` → banners → error → `.turn` (80px ring + "Đến lượt bạn" + syllable 1.6rem) → `WordInput` → rejection → "Bí từ" → claim error → "Đầu hàng" (right) → chain (`min-height: 9rem`). Priority is right: syllable and clock share a row, input beneath. Problems are density above the ring and churn beneath the input.

- **Header crowding (#3).** At 360 the inner width is 328px. "Đã kết nối" badge ≈ 105px, `K7M 2QP` ≈ 60px, "Luật chơi" ≈ 60px, chat pill with "3 tin mới" ≈ 150px. `.meta` wraps (`:291-297`) so nothing breaks, but the board opens on two rows of chrome. Sketch:
  ```svelte
  <div class="top">
    <ConnectionBadge compact />           <!-- dot only while OPEN; label sr-only -->
    <div class="meta">
      <a class="icon-button rules" href="/rules" target="_blank" rel="noopener" aria-label={t.rulesLink}>?</a>
      {#if onchatopen}<button class="chat-pill" …>💬 <span class="sr-only">{t.chatTitle}</span>{#if chatUnread}<span class="pill-badge">{chatUnread}</span>{/if}</button>{/if}
    </div>
  </div>
  …
  <p class="syllable"><span class="label">{t.currentSyllable}{#if modeLabel} · {modeLabel}{/if}</span><strong>…</strong></p>
  ```
  `ConnectionBadge` gains `compact` → when `status==='open'` render `.dot` + `.sr-only` label; any other status shows the text (that is when it matters). Pill count drops the word: `chatUnread` becomes `'{n}'` visually with `aria-label={fill(t.chatUnread,{n})}`. Header then fits one row at 320px.
- **Input row is not crowded — what follows it is.** `WordInput` row = input + "Gửi" (`:175-213`), fine. Below it, in order and each conditional: `.rejection` (word — message + up to two pills, `:220-232`), `.report-confirmation` (`:235-239`), "Bí từ" (`GameBoard:232-240`), `.claim-error` (`:243-251`), then "Đầu hàng" right-aligned (`:260-268`). Worst case is five stacked blocks between input and chain, four of them pop in and out. Sketch — one persistent secondary row, everything else inline in a single message slot:
  ```svelte
  <WordInput … />
  <div class="secondary">
    <button class="claim-dead-end" disabled={!canClaimDeadEnd} …>{claimArming ? t.claimDeadEndSure : t.claimDeadEnd}</button>
    <button class="resign" disabled={!canResign} …>{arming ? t.resignSure : t.resign}</button>
  </div>
  {#if message}<p class="notice" class:danger role="alert">…</p>{/if}   <!-- rejection | claimError | reportConfirmation, one at a time -->
  ```
  ```css
  .secondary { display:flex; justify-content:space-between; gap:var(--space-2); }
  ```
  Both buttons keep their existing arming styles and disabled looks; both stay mounted so the row height is constant (what `.resign` already argues for at `:437-443`).
- **Rejection affordances (#4).** `.suggestion, .report` (`WordInput:304-313`): `padding: 2px 8px`, 0.8rem → ≈23×90px. Under the clock these are the two most valuable taps on the board. Sketch:
  ```svelte
  <p class="rejection" id="word-rejection" role="alert">
    <strong>{word}</strong> — {message}
  </p>
  {#if suggestion || reason === NOT_IN_DICTIONARY}
  <div class="fixes">
    {#if suggestion}<button class="fix primary" onclick={useSuggestion}>{fill(t.suggestionPrompt,{word:suggestion})}</button>{/if}
    {#if reason === NOT_IN_DICTIONARY}<button class="fix" onclick={report}>{t.reportWord}</button>{/if}
  </div>
  {/if}
  ```
  ```css
  .fixes { display:flex; flex-wrap:wrap; gap:var(--space-2); margin-top:var(--space-2); }
  .fix { min-height:36px; padding:var(--space-1) var(--space-3); border:1px solid var(--border-strong); border-radius:var(--radius-pill); background:var(--surface); color:var(--text); font-size:var(--text-4); font-weight:600; }
  .fix.primary { border-color:var(--accent); background:var(--accent-soft); }
  ```
  Consider `useSuggestion()` also submitting — one tap, not tap + Enter — since the server already vouched the word exists; only ambiguity is the player may want to change it. Keep as fill-only if unsure; then label `'Dùng “{word}”'` is more honest than a question.
- **Breakdown chips (#9).** `.parts` (`ChainHistory:113-118, 316-322`) is a joined string, not chips, under every row. In a 9rem list that is ~3 rows, each now ~64px with parts. Show parts only on `.latest` and on an `open` row (row is already a toggle, `:79-86`), and render as chips:
  ```svelte
  {#if entry.parts.length && (index === 0 || open)}
    <ul class="parts" aria-label={t.pointsBreakdown}>{#each entry.parts as p}<li>+{p.value} <span>{pointKindLabels[p.kind]}</span></li>{/each}</ul>
  {/if}
  ```
  ```css
  .parts { display:flex; flex-wrap:wrap; gap:var(--space-1); margin:0; padding:0 12px 8px; list-style:none; }
  .parts li { padding:0 var(--space-2); border-radius:var(--radius-pill); background:var(--surface-alt); color:var(--text-muted); font-size:var(--text-2); font-variant-numeric:tabular-nums; }
  ```
  Labels: keep `nền/chuỗi/nhanh/hiếm` (they match `/rules`); change `SYLLABLES: 'dài'` → `'từ dài'` — "+5 dài" is not a phrase; "+5 từ dài" is. Add `pointsBreakdown: 'Cách tính điểm'`.
- **Wide screen.** Bot game uses the 560px shell — fine, it is a reading column. Online room at ≥900px is `1fr / 320px` (`online/+page.svelte:610-616`): game pane can reach 700px, so `.turn` (ring left, prompt right) leaves the right half empty and the chain rows stretch to 700px with `.meta` pushed far right of `.word`. Cap the board: `.pane.game { max-width: 560px; }` or centre `.turn`. Cheap, and it keeps the syllable near the input on a 1440 monitor.
- **Motion / reduced motion.** Inventory: `pulse` on the reconnecting dot (`ConnectionBadge:58`), two 150ms background transitions (`GameBoard:430,465`), rAF ring, smooth scroll via `motion.js`. Global kill switch `app.css:217-229` is correct (iteration-count fix noted). Motion is nearly absent, which is a defensible choice; two micro-interactions would earn their place and are auto-covered by the global rule: accepted word entering (`li.latest { animation: rise 160ms ease-out }` with `@keyframes rise { from { opacity:0; transform: translateY(4px) } }`) and a 2-frame horizontal nudge on `.rejection` insertion (`translateX(-3px)→3px→0`, 180ms). Nothing loops.
- **5 seconds left.** `CountdownRing:365,418-420,450-456`: `urgent` = `mine && !stalled && left ≤ 5` → `--danger`, stroke 6→9, value 1.5→1.75rem, spoken at 10 and 5. Colour-independent, doesn't move, opponent's clock stays neutral — correct. Gap: the eye is on the caret, 80px to the right. Lift `urgent` to the board (`data-urgent` on `.board`, or a store derived) and add `.board[data-urgent] .input-row input { border-color: var(--danger); box-shadow: inset 0 0 0 1px var(--danger); }`. Small, no motion.
- **Elimination / spectating (#6).** `game.iAmOut` → `PlayerStatus:535` banner "Bạn đã bị loại. Ván đấu vẫn đang tiếp tục." *and* `GameBoard:224` box "Bạn đang xem ván đấu." Same news twice, 60px apart. Merge into the spectating box and put the suggestions there — the player has minutes to look at them:
  ```svelte
  <div class="spectating" role="status">
    <p>{t.youAreOut}</p>
    {#if game.state.elimination?.suggestions.length}
      <p class="could">{t.suggestionsTitle}: {game.state.elimination.suggestions.join(' · ')}</p>
    {:else if game.state.elimination}
      <p class="could">{fill(t.noSuggestions,{syllable})}</p>   <!-- capture syllable at knockout; currentSyllable moves on -->
    {/if}
  </div>
  ```
  Note the existing `GameOverPanel:442` fills `noSuggestions` with `game.state.currentSyllable` — by game over in a 4-seat room that is somebody else's syllable. Store the syllable in `elimination` at `game.svelte.js:425-430`.
- **Game-over standings.** `GameOverPanel:396-424`: standings list *then* `.stats` "Điểm cuối cùng / Số từ trong chuỗi" — the score is already on the player's own standings row (`:408`); drop `finalScore` from `.stats`, keep `chainLength`. `🏆` on a bare `<span aria-label>` (`:409`) is not reliably announced — `role="img"`. `h2` gets `aria-live="polite"` on insertion (`:390`) plus panel focus (`:365-367`); either is enough, keep the focus, drop the live attribute to avoid a double announcement.

## 3. Online

- **Entry choice architecture.** `online/+page.svelte:509-543`: two identical `.primary` filled buttons ("Chơi ngay", "Tạo phòng") stacked, then the join form. Two primaries = no primary. `onlineIntro` (`vi.js:100`) still describes only create/join. Sketch: "Chơi ngay" stays filled; "Tạo phòng" becomes outlined (`border:1px solid var(--border-strong); background:var(--surface); color:inherit`); join form unchanged. Copy: `onlineIntro: 'Chơi ngay với người lạ, tạo phòng cho bạn bè, hoặc nhập mã bạn được mời.'` Add sub-labels under the two buttons at `--text-3` muted: `quickMatchHint: 'Ghép với người đang chờ, ván đầu tự bắt đầu'`, `createRoomHint: 'Nhận mã sáu ký tự để gửi cho bạn bè'`.
- **Waiting panel (#8).** `:494-503`. Split: `<p aria-live="polite">{t.quickMatchSearching}</p>` announced once, and `<p aria-hidden="true">{n} giây</p>` for the eye; or keep one line with `aria-live="off"` and a separate `sr-only` status announcing at 0s and 20s. Nudge copy: `quickMatchNudge: 'Chưa có ai chờ.'`, `quickMatchNudgeLink: 'Chơi với máy trong lúc đợi (sẽ rời hàng chờ)'`; link to `/play?difficulty={settings' last or MEDIUM}`. Also `.waiting` has no `.cancel` styling differentiation — fine as is.
- **Lobby (2–4 seats).** `Lobby.svelte:97-162`: count line, seat rows with role pill, wins, state text, owner-only 36px `×` (44px hit via `::after`, `:361-379`). Readiness = tint + text label (`:294-297,129-131`) — colour-independent. Owner = 3px accent left border (`:299-301`). Hints cover every role/state (`:172-184`). Two additions: (a) **seat colour appears nowhere except chat.** Move the owner marker to the role pill (it already says "Chủ phòng") and use the left border for the seat colour: `.seat { border-inline-start: 3px solid var(--seat) }` with `style:--seat={`var(--player-${game.seatIndexOf(player.playerId)})`}`; the chat log's colours then mean something the first time they are seen. (b) `kick:disabled` at `opacity .35` with no reason — `title` is not enough; when `player.ready` add `.sr-only` "{t.player_is_ready}" text or drop the button for ready seats and keep the row height (`visibility:hidden`).
- **Chat log contrast (computed from `app.css:38-41, 93-96`).** Light, on `--surface`/`--surface-alt`: p1 7.05/6.08, p2 7.15/6.17, p3 6.72/5.80, p4 5.95/**5.13**. Dark: p1 7.92/7.05, p2 6.77/6.02, p3 7.89/7.02, p4 9.13/8.13. All pass AA; p4 light is the tight one — `--player-4: #0b5f6a` lifts it to ~5.8 without touching hue. Distinguishability between seats is low for CVD (p1 vs p4 1.18, p1 vs p2 1.02 luminance ratio) — acceptable only because every line also carries the name (`ChatPanel:170`); keep that invariant. Timestamp `.at` at `--text-1` (0.7rem = 11.2px) muted on every line is noise: show it only when the minute changes from the previous line, or on hover/focus of the line.
- **Chat as log.** `ChatPanel:159-176` one line per message, `aria-live="polite" aria-relevant="additions"` — reads author, text *and* timestamp for every message; move `.at` outside the live text or `aria-hidden` it. `chatAuthorLeft: 'Đã rời phòng'` renders as `Đã rời phòng: hello` — reads as a sentence, not a name. Use `'(đã rời phòng)'`. Folded header button (`:142-147`) is 44px, good; the board pill (#3) and this header are two ways to the same thing on a phone — acceptable since the pill is above the chain.
- **Reconnect surfaces.** Three layers, no conflict: `ConnectionBadge` live label; `.offline` line with 44px "Thử lại" (`GameBoard:172-177`, warn on surface-alt 5.11 light / 8.13 dark); per-player grace banners with countdown (`PlayerStatus:540-549`). Lobby `ownerAway` hint (`Lobby:173-174`) is the right override. One gap: the ring at `stalled` goes to `opacity .4` (`CountdownRing:428-430`) but the value keeps counting — the number should read "–" or the label should say offline, else the player reads a live clock they cannot beat.
- **Invite link.** `RoomCodePanel`: 3+3 grouping, letter-by-letter `aria-label`, monospace, copy code / copy link / native share, clipboard-failure fallback selects the code, full URL visible and `user-select: all`. Nothing to add; the compact variant correctly keeps only "Sao chép mã".

## 4. Copy

Tone: consistent second-person "bạn", imperative "Hãy …" for recovery, no exclamation except `won`, `newRecord`, `TIMEOUT`. Explains rather than blames in almost every rejection (`NOT_IN_DICTIONARY`, `WRONG_LINK` with the syllable filled). Issues:

- `not_your_turn` / `NOT_YOUR_TURN` "Chưa đến lượt bạn." — for resign and claim this can only fire on a race (both buttons are turn-gated), where the truth is the turn *just left*. Propose for the server-error key: `not_your_turn: 'Lượt vừa chuyển đi, thao tác này không còn hiệu lực.'`; keep the reject-reason string for a raced word submit but extend it: `'Chưa đến lượt bạn — từ này chưa được gửi.'`. Route the error to the same slot as `claimError` (#7).
- `not_a_dead_end: 'Vẫn còn từ nối được. Hãy thử lại.'` — "thử lại" invites re-pressing "Bí từ". → `'Vẫn còn từ nối được với tiếng này. Nghĩ thêm chút nữa!'`
- `winsLabel: 'Tỉ số'` beside a per-game `score` (`ScoreBoard:361-365`) — two numbers, one called "điểm", one "tỉ số", both look like scores. → `'Ván thắng'` (lobby) / `'Thắng {n}'` compact on the board.
- `need_more_players` hardcodes "hai" while `ownerNeedsMore` interpolates `{n}` — server constant leaks in prose. → `'Chưa đủ người để bắt đầu.'`
- `opponentTurn: 'Đối thủ đang suy nghĩ…'` fine for bot; online it is the fallback when `nameOf` is empty — reword neutral: `'Đang chờ người chơi khác…'`.
- `nicknameHint` — see §1. `app.html:246` meta description says "đấu 1v1"; the game is 2–4: `'Trò chơi nối từ tiếng Việt: chơi với máy hoặc đấu trực tuyến 2–4 người.'`
- `too_fast: 'Bạn thao tác quá nhanh. Chậm lại một chút nhé.'` — the only "nhé" in the file; either adopt the warmer register elsewhere (`not_a_dead_end` above does) or drop it: `'Thao tác quá nhanh. Đợi một chút rồi thử lại.'`
- Length at 360: `claimDeadEndSure 'Chắc chắn bí từ?'` and `resignSure` fit the 32px buttons; `reportWord 'Báo từ này là từ thật'` (~150px at 0.8rem) wraps the rejection row — with #4 it moves to its own line. `quickMatchNudgeLink` + `quickMatchNudge` on one line wrap at 360; the sketch in §3 accepts that.
- Rules body paragraphs are single 90–120-word sentences (`rulesDeadEndBody`, `rulesRoomBody`). Split each at the first "—" into two sentences; content is fine.

## 5. Accessibility

- **Focus order**: header (brand, theme) → skip link target `#main` → badge (not focusable) → pill → scoreboard (static) → ring (static) → input → Gửi → suggestion/report → Bí từ → Đầu hàng → chain rows. Logical. Game over: panel takes focus (`GameOverPanel:365`), good. The input keeps focus off-turn with hidden caret (`WordInput:266-271`) and `aria-disabled` — correct trade for the keyboard; the placeholder swap tells a screen reader nothing because placeholder isn't re-announced. Add `aria-describedby` to a `sr-only` "Chưa đến lượt bạn" status when `!enabled`, or announce via the existing turn `role="status"` (already does — fine, no change).
- **Labels**: word input `aria-label` stable (`:204`); chat input labelled; kick has state-aware `aria-label` (`Lobby:146`); theme toggle `aria-pressed` + sr text. `.chat-pill` reads "Trò chuyện 3 tin mới" — good. `🏆` span — `role="img"` (§2).
- **Live regions**: turn (`GameBoard:207-209` polite, atomic), badge (polite), spoken clock at 10/5 (`CountdownRing:400-402`), errors `role="alert"`, chat `aria-live` additions, quick-match counter every second (**fix**, #8), game-over `h2` + focus (double, §2). Count of simultaneously-live nodes on the board is five; acceptable because each fires on a distinct event.
- **Keyboard-only play**: fully possible. Two-press arming for resign/claim/kick works with Enter/Space; the 4s disarm (`GameBoard:46`) is short for a switch-access user — 8s costs nothing.
- **Colour-independent state**: ring thickness+size at urgent; scoreboard active row = border + tint + 3px underline (`ScoreBoard:403-413`); ready seat = tint + text; chain `.mine` = border + tint; `.latest` = 3px bar; connection = text label. Pass. `li.mine` tint vs surface is 1.17 light — the accent border carries it.
- **Targets**: pass — inputs ≈52px, Gửi, Ready/Start/Copy 44px, kick 44 via `::after`, icon dismiss 44. Fail/tight — `.suggestion/.report` ≈23px (#4), `.toc a` ≈27px (§1), `ThemeToggle` 40px (`:86-97`; make 44), `.chat-pill` 32px and `.claim-dead-end`/`.resign` 32px by stated intent (`GameBoard:418-423`) — WCAG 2.2 2.5.8 minimum is 24, so compliant; I would still take `.claim-dead-end` to 40 since it is an in-turn action, unlike resign.
- **Contrast** (computed): all text pairs in use ≥4.88 light / ≥5.14 dark; `--border-strong` on surface 3.64/3.82 and on surface-alt 3.14/3.40 (≥3:1 for UI boundaries, pass); ring track vs arc 1.87/2.51 — non-text but the arc is the information; the thickness/number carry it, and the arc is `currentColor` on accent so it is legible against the track by lightness in dark mode only. Consider `.track { stroke: var(--border); }` (fainter track = arc reads higher).

## 6. Visual identity

What `app.css` adds up to: a coherent *palette* (paper with a green cast, one accent that is also the "text green", four seat hues) and disciplined *tokens*, drawn in `system-ui` with 600/700 weights inside 8px-radius outlined boxes. It is clean and reads as "well-built default". Verified drift: 34 spacing declarations still use 6/10/14px literals (17×10px, 8×14px, 9×6px) beside the 4px ramp; four font-size literals off the ramp (`GameBoard:365` 1.6rem, `CountdownRing:132,149` 1.75/1.5rem, `online:636` 1.3rem); the ramp's first five steps span 0.7–0.9rem (11.2→14.4px in 0.8px increments) — too fine to be a scale. Light theme surfaces are near-flat: `--surface-alt`/`--surface` 1.16, `--bg`/`--surface` 1.08.

Restrained direction — one face, one accent, fewer steps:

- **Display face for the four hero glyphs only** (syllable, clock value, score, room code): `Be Vietnam Pro` 700 — designed for Vietnamese, full diacritic stacking, Google Fonts, self-hostable as two `woff2` (vietnamese+latin subset ≈ 45KB total). Body stays `system-ui`. Fallback chain keeps the current look if the font fails.
  ```css
  --font-display: 'Be Vietnam Pro', var(--font);
  /* GameBoard .syllable strong, CountdownRing .value, ScoreBoard .score, RoomCodePanel .code (keep monospace there? — no: the display face's 5/S 2/Z are distinct enough; if in doubt keep ui-monospace) */
  ```
  Alternative at zero bytes: keep `system-ui`, but give the syllable the room a hero deserves — `font-size: var(--text-10); letter-spacing: -0.01em; line-height: 1.25`.
- **Type ramp 9 → 7**, mapped:
  ```css
  --text-1: 0.75rem;  /* was text-1/text-2 (0.7, 0.75): badges, series, timestamps */
  --text-2: 0.875rem; /* was text-3/text-4/text-5 (0.8, 0.85, 0.9): meta, hints, labels */
  --text-3: 1rem;     /* was text-6: body, inputs (iOS floor) */
  --text-4: 1.125rem; /* was text-7 (1.1): scoreboard score, h2 */
  --text-5: 1.5rem;   /* was text-8 (1.4) + ring value 1.5 */
  --text-6: 2rem;     /* was text-9: room code */
  --text-7: 2.25rem;  /* new: the syllable (was 1.6 literal) */
  ```
  Losing 0.7rem (11.2px) removes the smallest text in the app (pill badge, series, chat time) — all of which are currently at the edge of legible on a phone.
- **Accent hover / pressed tokens** — hovers today borrow `--surface-alt`, which is grey, so the accent never darkens under the pointer:
  ```css
  :root { --accent-hover: #0e5626; --accent-pressed: #0a4520; }
  [data-theme='dark'] { --accent-hover: #7ae6a1; --accent-pressed: #93edb3; }
  .primary:hover { background: var(--accent-hover); } .primary:active { background: var(--accent-pressed); }
  ```
  Both keep white/dark ink ≥ 7:1.
- **Lift the page from the cards** (light only): `--bg: #eef3ef` (surface/bg 1.25, cards read as cards; text-muted on bg stays 5.0+). Dark is already layered (1.35/1.49).
- **Spacing**: replace the 34 literals with the ramp (`6→--space-2` or `--space-1`, `10→--space-3` or `--space-2`, `14→--space-4`), then the ramp is the rhythm rather than a suggestion. Radius: keep 12/8/pill.
- **Where the identity will actually show**: the `.turn` block. With the syllable at 2.25rem in the display face, the ring beside it, and the chain in body face below, the board has one obvious focal point — which is also the one glyph read every turn (`GameBoard:362-367` already argues this).

## Unresolved questions

1. Fix #1: new tab vs in-page `<dialog>` summary of the rules? A dialog keeps the clock visible but needs a second copy of the rules or an iframe of `/rules`; a new tab is one attribute.
2. Bot game: does the human take the first turn after the opening word (engine `players[0]`)? Determines whether the landing one-liner should mention the clock at all.
3. Are the two micro-interactions (word entrance, rejection nudge) wanted, given the current near-zero-motion stance reads as deliberate?
4. Font: is a ~45KB self-hosted display face acceptable for a single-binary deploy, or is the zero-byte `system-ui` variant preferred?
5. `useSuggestion` — fill only (current) or fill-and-submit?
6. Storing the knockout syllable in `elimination` is a store change beyond CSS/markup — in scope for the owner's "accept redesigns", or should it be filed separately?

Status: DONE
Summary: Full read-only review written to `plans/reports/ui-ux-designer-260921-1529-whole-game-ux-review.md`; two structural findings (rules link forfeits the game; no rule before first clock) plus a ranked top-10 with markup/CSS/copy sketches, contrast figures computed from tokens.
Concerns/Blockers: Contrast and layout widths are computed/estimated from source, not rendered; §2 header-width estimates assume system-ui metrics.
