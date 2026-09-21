<script>
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import ConnectionBadge from '$lib/components/ConnectionBadge.svelte';
	import GameBoard from '$lib/components/GameBoard.svelte';
	import GameOverPanel from '$lib/components/GameOverPanel.svelte';
	import ChatPanel from '$lib/components/ChatPanel.svelte';
	import Lobby from '$lib/components/Lobby.svelte';
	import NicknameInput from '$lib/components/NicknameInput.svelte';
	import PlayerStatus from '$lib/components/PlayerStatus.svelte';
	import { errorMessage, fill, t } from '$lib/i18n/vi.js';
	import { scrollBehavior } from '$lib/motion.js';
	import { isRoomCode, normalizeRoomCode, ROOM_CODE_LENGTH } from '$lib/room-code.js';
	import { createRoomSession } from '$lib/stores/room-session.svelte.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { settings } from '$lib/stores/settings.svelte.js';
	import {
		cancelQuickMatch,
		claimDeadEnd,
		createRoom,
		joinRoom,
		kickPlayer,
		leaveRoom,
		quickMatch,
		reportWord,
		resign,
		sendChat,
		setReady,
		startGame,
		submitWord
	} from '$lib/ws/messages.js';
	import {
		Status,
		connect,
		connection,
		disconnect,
		forgetSession,
		hasStoredSession,
		send
	} from '$lib/ws/connection.svelte.js';

	/**
	 * The join/resume/quick-match/leave machine. Extracted to its own store —
	 * see room-session.svelte.js — so the resume time-box below is something
	 * Vitest can drive directly instead of only through a mounted page.
	 */
	const session = createRoomSession();

	/**
	 * How long the wait runs before the screen offers the bot instead. A
	 * quick match with nobody to pair with would otherwise be a dead end of
	 * its own.
	 */
	const QUICK_MATCH_NUDGE_S = 20;

	// The rung last played, if this browser has one, so the nudge's bot game
	// resumes it rather than opening back on the ladder's own default.
	const quickMatchNudgeHref = $derived(
		settings.state.lastDifficulty != null ? `/play?difficulty=${settings.state.lastDifficulty}` : '/play'
	);

	/**
	 * How long a held request waits before the screen stops saying "connecting"
	 * and starts saying something the player can act on. The backoff is capped
	 * at eight seconds and never gives up, so without this the screen would
	 * claim to be connecting for as long as the player was willing to watch it.
	 */
	const STALL_MS = 5000;

	/**
	 * How long `resuming` waits, once the socket is actually open, before the
	 * join form comes back on its own. The server answering an unknown resume
	 * token used to mean silence — no Welcome, no error — which left every
	 * button on this screen reading "Đang kết nối…" forever, recoverable only
	 * by a reload that reproduced the same dead end. A server new enough to
	 * answer with `session_not_resumable` clears this sooner, through the
	 * ordinary error path below; this is the backstop for one that cannot.
	 */
	const RESUME_TIMEOUT_MS = 5000;

	/**
	 * Where the two-column layout starts. The media queries in the styles
	 * below are the same decision expressed in CSS, so the two must agree: the
	 * columns are drawn there, and what goes in them — a chat panel that folds
	 * or one with a column of its own — is decided here.
	 */
	const WIDE = '(min-width: 900px)';

	// Whether the room is being drawn as two columns. Read from the browser
	// rather than assumed, because the chat panel behaves differently in each:
	// stacked under the board it folds behind an unread count, and beside it
	// there is nothing to fold out of the way of.
	let wide = $state(false);

	$effect(() => {
		const mq = window.matchMedia(WIDE);
		wide = mq.matches;
		/** @param {MediaQueryListEvent} event */
		const onChange = (event) => (wide = event.matches);
		mq.addEventListener('change', onChange);
		return () => mq.removeEventListener('change', onChange);
	});

	// Lifted out of ChatPanel so the pill in GameBoard's top row — reachable
	// above the chain rather than below it — can unfold the panel and read its
	// count without the two components knowing about each other beyond this.
	// Open in the lobby, where talking is what people are there to do, and
	// folded the moment a game starts, so the board is not pushed off a phone
	// screen by the conversation under it. A lobby that starts folded would
	// hide the input behind a badge before anyone has said anything.
	let chatFolded = $state(false);
	let chatUnread = $state(0);
	/** @type {HTMLElement | undefined} */
	let talkPane = $state();

	// Folded going into a game, on a narrow screen where the two would crowd
	// each other; unfolded again once it ends, since the compact lobby that
	// appears beneath the result is the same "waiting in a room" situation
	// the chat is open for everywhere else. Phase never actually revisits
	// 'lobby' after the first game — the room goes over → (next start) →
	// playing directly — so folding was permanent after one game without this.
	$effect(() => {
		if (game.state.phase === 'playing') chatFolded = true;
		else if (game.state.phase === 'over') chatFolded = false;
	});

	function openChat() {
		chatFolded = false;
		talkPane?.scrollIntoView({ behavior: scrollBehavior(), block: 'start' });
	}

	let codeInput = $state(normalizeRoomCode(page.url.searchParams.get('code') ?? ''));
	let codeError = $state('');

	const inviteCode = $derived(normalizeRoomCode(page.url.searchParams.get('code') ?? ''));
	const playing = $derived(game.state.phase === 'playing' || game.state.phase === 'over');
	// A seat in a room, whichever phase it is in. Both are the same layout —
	// the game or the lobby on one side, the conversation on the other.
	const inRoom = $derived(playing || game.state.phase === 'lobby');
	const named = $derived(settings.state.nickname.trim().length > 0);

	// The resume worked, so nothing that happens from here is its fault — and
	// an invite code held behind it has been answered by arriving in a room.
	$effect(() => {
		if (inRoom) untrack(() => session.noteRoom());
	});

	// Owns the socket while this screen is on, exactly as the bot screen does.
	// An invite link is a request to join, so it is honoured on arrival rather
	// than making the player press a button they did not ask for.
	$effect(() => {
		const code = inviteCode;
		untrack(() => {
			game.reset();
			// Chat survives reset() so a game starting cannot wipe it, which
			// means arriving at this screen has to clear it explicitly — or a
			// conversation would follow the player into the next room.
			game.clearChat();
			// Deliberately not connecting yet. The nickname is typed on this
			// screen, and Hello carries it once — a socket opened on arrival
			// would introduce the player under whatever name was stored before
			// they got here.
			if (hasStoredSession()) {
				// This tab was already in a game. Reconnecting restores it, which
				// is what a player who refreshed mid-game is expecting; the
				// nickname is already settled, so there is nothing to wait for.
				//
				// The resume comes first even when the URL carries a code: Hello
				// takes the token with it either way, so joining as well would
				// have the room refuse a second seat to somebody it had just
				// given their old one back to — a red banner on a board that had
				// in fact been restored correctly. The code is held instead, and
				// only spent if the resume is refused.
				session.startResume();
				if (isRoomCode(code)) session.holdPendingJoin({ kind: 'join', code });
				connect();
			} else if (isRoomCode(code)) {
				if (named) {
					request({ kind: 'join', code });
				} else {
					// Held, not sent. The name field is already on this screen and
					// the code is already in its field, so this is one button.
					session.setNeedName(true);
				}
			}
		});

		return () => {
			// Leaving this screen is leaving the room, not pausing it: the seat
			// is freed, a game still running is told somebody left, and the
			// stored session goes with it so the next visit arrives as a
			// stranger who can join again rather than being resumed into a room
			// they walked out of.
			//
			// A reload is the other thing entirely and never reaches this
			// cleanup, so a refresh still restores the game.
			if (game.state.phase !== 'idle') {
				// Readiness first, because the room refuses to let a ready
				// player leave: that friction is there so somebody the others
				// are waiting on has to take it back deliberately, and this
				// player has just done something rather more deliberate than
				// that. Without it the seat would sit here ready and empty
				// until the reconnect window ran out.
				if (game.isReady) send(setReady(false));
				send(leaveRoom());
				forgetSession();
			}
			// Leaving mid-wait is leaving the queue too: nobody is left to pair
			// with a tab that has gone.
			if (game.state.queued) send(cancelQuickMatch());
			session.clearPending();
			session.clearAction();
			disconnect();
			game.reset();
			game.clearChat();
		};
	});

	// Held requests go out once the handshake has landed — both the one that
	// opens or joins a room, and any lobby action the socket refused while it
	// was down.
	$effect(() => {
		const open = connection.status === Status.OPEN;
		untrack(() => {
			flush(open);
			session.flushAction(open, dispatchAction);
		});
	});

	// Counts up while queued, for the waiting panel's elapsed time and the
	// bot nudge. Restarted from zero each time the wait begins, so a match
	// found and then a later, separate wait never inherits the first one's
	// clock.
	$effect(() => {
		if (!game.state.queued) {
			session.resetQueued();
			return;
		}
		session.resetQueued();
		const id = setInterval(() => session.tickQueued(), 1000);
		return () => clearInterval(id);
	});

	// A request that has been waiting on a socket for longer than a player will
	// believe. Timed from the request rather than from the status, because a
	// backoff cycles between "reconnecting" and "connecting" indefinitely and
	// neither of them is news.
	$effect(() => {
		const waiting = !!session.state.pending && connection.status !== Status.OPEN;
		if (!waiting) {
			session.setStalled(false);
			return;
		}
		const timer = setTimeout(() => session.setStalled(true), STALL_MS);
		return () => clearTimeout(timer);
	});

	// The bound this screen puts on how long a resume may run once the socket
	// is actually open. An older server answers a stale token with silence
	// rather than an error, which without this left `resuming` stuck true
	// forever — every button on the join form disabled, and the `stalled`
	// banner never firing because it only watches a socket that never opened,
	// not a handshake that opened and then went quiet.
	$effect(() => {
		if (!(session.state.resuming && connection.status === Status.OPEN)) return;
		const timer = setTimeout(() => {
			untrack(() => {
				session.noteResumeFailed(named);
				forgetSession();
				if (!session.state.needName) flush(connection.status === Status.OPEN);
			});
		}, RESUME_TIMEOUT_MS);
		return () => clearTimeout(timer);
	});

	// A resume that the server cannot honour is not something the player did.
	// Reporting it would open the lobby with a red banner about a game they
	// have already left behind, so the token is dropped quietly instead — and
	// an invite code held behind the resume is spent now. A server new enough
	// to answer a stale token with `session_not_resumable` lands here, ahead
	// of the time-box above.
	$effect(() => {
		const failed = session.state.resuming && !!game.state.error;
		untrack(() => {
			if (!failed) return;
			game.clearError();
			forgetSession();
			session.noteResumeFailed(named);
			if (!session.state.needName) flush(connection.status === Status.OPEN);
		});
	});

	/** @param {import('$lib/stores/room-session.svelte.js').RoomRequest} req */
	function request(req) {
		codeError = '';
		game.clearError();
		session.request(req);
		// The handshake carries the nickname as it stands now, which is why the
		// connection waits until the player has actually asked for a room.
		connect();
		flush(connection.status === Status.OPEN);
	}

	/** @param {boolean} isOpen */
	function flush(isOpen) {
		session.flush(isOpen, {
			create: () => send(createRoom()),
			join: (code) => send(joinRoom(code)),
			quickMatch: () => send(quickMatch())
		});
	}

	/**
	 * Resends a lobby action the socket refused the first time. Every one of
	 * these is safe to resend regardless of what happened in between: the
	 * server refuses whichever no longer apply rather than misapplying them.
	 * @param {import('$lib/stores/room-session.svelte.js').RoomAction} action
	 * @returns {boolean}
	 */
	function dispatchAction(action) {
		switch (action.kind) {
			case 'cancelQueue':
				return send(cancelQuickMatch());
			case 'leaveRoom':
				return send(leaveRoom());
			case 'setReady':
				return send(setReady(action.ready));
			case 'startGame':
				return send(startGame());
			case 'kickPlayer':
				return send(kickPlayer(action.playerId));
			default:
				return false;
		}
	}

	function join() {
		const code = normalizeRoomCode(codeInput);
		if (!isRoomCode(code)) {
			codeError = t.roomCodeInvalid;
			return;
		}
		if (!named) {
			session.setNeedName(true);
			return;
		}
		request({ kind: 'join', code });
	}

	function create() {
		request({ kind: 'create' });
	}

	function playQuickMatch() {
		request({ kind: 'quickMatch' });
	}

	/** Withdraws from the pairing queue without leaving the page. */
	function cancelQueue() {
		const sent = send(cancelQuickMatch());
		if (!sent) session.holdAction({ kind: 'cancelQueue' });
		session.clearPending();
	}

	function goHome() {
		goto('/');
	}

	/**
	 * Gives up the seat without leaving the page: the room may still be there
	 * to rejoin, and the lobby list is the natural place to land.
	 *
	 * The local state goes with it. The server sends nothing back to somebody
	 * who is no longer in the room to be told about, and the button is only
	 * enabled when this client already knows the rule allows it.
	 *
	 * Each of these reports whether the request actually reached the server, so
	 * the lobby can say so rather than looking like a button that does nothing.
	 * @param {boolean} ready
	 * @returns {boolean}
	 */
	function ready(ready) {
		const sent = send(setReady(ready));
		if (!sent) session.holdAction({ kind: 'setReady', ready });
		return sent;
	}

	/** @returns {boolean} */
	function start() {
		const sent = send(startGame());
		if (!sent) session.holdAction({ kind: 'startGame' });
		return sent;
	}

	/**
	 * @param {string} playerId
	 * @returns {boolean}
	 */
	function kick(playerId) {
		// The lobby arms this with a second press of the same button; a native
		// confirm() would block the frame loop the countdown runs on.
		const sent = send(kickPlayer(playerId));
		if (!sent) session.holdAction({ kind: 'kickPlayer', playerId });
		return sent;
	}

	function leave() {
		const sent = send(leaveRoom());
		if (!sent) session.holdAction({ kind: 'leaveRoom' });
		game.leave();
		session.clearPending();
		// Matches the page-teardown path: leaving deliberately must not leave
		// a token behind for the next load of /online to resume with — the
		// player just walked out of this room on purpose.
		forgetSession();
	}

	/** @param {string} text */
	function say(text) {
		send(sendChat(text));
	}

	/**
	 * @param {string} word
	 * @returns {boolean}
	 */
	function play(word) {
		return send(submitWord(word, game.state.turnSeq));
	}

	function giveUp() {
		// Armed by the board with a second press, for the same reason as kick.
		send(resign());
	}

	function claim() {
		// Armed by the board the same way giving up is: a second press, so a
		// stray tap cannot spend it.
		send(claimDeadEnd());
	}

	/** @param {string} word */
	function report(word) {
		send(reportWord(word));
	}
</script>

<svelte:head>
	<title>{inRoom && game.state.roomCode ? fill(t.titleRoom, { code: game.state.roomCode }) : t.titleOnline}</title>
</svelte:head>

<section class="online" class:room={inRoom} class:in-game={playing}>
	{#if inRoom}
		<!-- The screen's heading, so the document does not start at h2 once the
		     join form's h1 is gone. Not the room code: that is on screen and
		     already spelled out letter by letter for a screen reader. -->
		<h1 class="sr-only">{t.onlineTitle}</h1>

		<!-- Two columns where there is room for them: the game on one side and
		     the conversation on the other, so neither has to be scrolled past
		     to reach the other. One column, game first, where there is not.

		     One chat panel across both phases, kept by staying in the same
		     place in the markup: a panel remounted on the way into a game
		     would reopen having read nothing, and the lobby's conversation
		     would come back as unread mail. -->
		<div class="pane game">
			{#if playing}
				<GameBoard
					modeLabel={game.state.roomCode}
					onsubmit={play}
					onresign={giveUp}
					onclaimdeadend={claim}
					onreportword={report}
					chatUnread={wide ? 0 : chatUnread}
					onchatopen={wide ? undefined : openChat}
				>
					{#snippet banner()}
						<PlayerStatus />
					{/snippet}
					{#snippet gameOver()}
						<!-- No rematch button on the panel: the room is still here, and
						     the next game is agreed in the lobby below exactly as the
						     last one was. -->
						<GameOverPanel isRecord={false} onhome={goHome} />
						<Lobby compact actionHeld={!!session.state.heldAction} onready={ready} onstart={start} onkick={kick} onleave={leave} />
					{/snippet}
				</GameBoard>
			{:else}
				<Lobby actionHeld={!!session.state.heldAction} onready={ready} onstart={start} onkick={kick} onleave={leave} />
			{/if}
		</div>

		<!-- errors are not routed here any more: the lobby draws its own, beside
		     the button that produced them.

		     Collapsible on any narrow screen, lobby included, so chat arriving
		     in a lobby whose panel the player folded still shows a count; it
		     starts open there and folds itself when a game begins. -->
		<div class="pane talk" bind:this={talkPane}>
			<ChatPanel
				collapsible={!wide}
				column={wide}
				onsend={say}
				bind:folded={chatFolded}
				bind:unread={chatUnread}
			/>
		</div>
	{:else}
		<h1>{t.onlineTitle}</h1>
		<p class="intro">{t.onlineIntro}</p>

		<!-- The one screen where creating and joining happen, and until now the
		     only one with no connection state on it at all: a server that was
		     down left an enabled button and a screen that never changed. -->
		<div class="status">
			<ConnectionBadge />
		</div>

		<NicknameInput />

		{#if session.state.needName}
			<p class="notice" role="alert" data-testid="name-needed">{t.nicknameNeeded}</p>
		{/if}

		{#if game.state.error}
			<p class="error" role="alert" data-testid="join-error">{game.state.error}</p>
		{/if}

		{#if session.state.resumeFailed}
			<!-- Not an error() from the store: a resume outcome is never a
			     ServerMessage the page decides how to react to in the ordinary
			     way, and the message is the same whether the server actually said
			     `session_not_resumable` or simply never answered. -->
			<p class="notice" role="alert" data-testid="resume-failed">
				{errorMessage('session_not_resumable')}
			</p>
		{/if}

		{#if session.state.stalled}
			<p class="error" role="alert" data-testid="connect-stalled">{t.connectStalled}</p>
		{/if}

		{#if game.state.queued}
			<!-- The wait itself. Ends on its own — RoomState replaces this whole
			     branch the moment a match is found — so the only button here is
			     the way out. The running seconds sit outside the live region:
			     inside it, a screen reader announced "n giây" on every tick. -->
			<div class="waiting">
				<p role="status" aria-live="polite">{t.quickMatchWaiting}</p>
				<p class="counter" aria-hidden="true">
					{fill(t.secondsLeft, { n: session.state.queuedForS })}
				</p>
				<button type="button" onclick={cancelQueue}>{t.quickMatchCancel}</button>
				{#if session.state.queuedForS >= QUICK_MATCH_NUDGE_S}
					<p class="hint">
						{t.quickMatchNudge}
						<a href={quickMatchNudgeHref}>{t.quickMatchNudgeLink}</a>
					</p>
				{/if}
			</div>
		{:else}
			<!-- Disabled while a request is in flight. Every impatient tap used to
			     send a real CreateRoom, and the fifth one came back as "you are
			     creating rooms too quickly" to a player who thought they had tapped
			     nothing at all. -->
			<div class="choice">
				<button
					type="button"
					class="primary"
					disabled={!!session.state.pending}
					onclick={playQuickMatch}
				>
					{session.state.pending?.kind === 'quickMatch' ? t.connecting : t.quickMatch}
				</button>
				<p class="choice-hint">{t.quickMatchHint}</p>
			</div>

			<div class="choice">
				<button type="button" class="primary" disabled={!!session.state.pending} onclick={create}>
					{session.state.pending?.kind === 'create' ? t.connecting : t.createRoom}
				</button>
				<p class="choice-hint">{t.createRoomHint}</p>
			</div>

			<form
				class="join"
				onsubmit={(event) => {
					event.preventDefault();
					join();
				}}
			>
				<label for="room-code">{t.roomCodeLabel}</label>
				<div class="row">
					<input
						id="room-code"
						type="text"
						inputmode="text"
						autocapitalize="characters"
						autocomplete="off"
						spellcheck="false"
						maxlength={ROOM_CODE_LENGTH * 2}
						placeholder={t.roomCodePlaceholder}
						bind:value={codeInput}
						oninput={() => (codeError = '')}
					/>
					<button type="submit" disabled={!!session.state.pending}>
						{session.state.pending?.kind === 'join' ? t.connecting : t.joinRoom}
					</button>
				</div>
				<p class="hint" class:invalid={codeError}>{codeError || t.roomCodeHint}</p>
			</form>
		{/if}

		<a class="back" href="/">{t.back}</a>
	{/if}
</section>

<style>
	.online {
		display: flex;
		flex-direction: column;
		flex: 1;
		gap: var(--space-4);
		min-height: 0;
		padding-top: var(--space-3);
	}

	/* Joining is a form, not a room: it keeps a form's width whatever the
	   screen the two columns were widened for, and sits in the middle of it
	   rather than against the left edge of a 1040px shell. */
	.online:not(.room) {
		width: 100%;
		max-width: 480px;
		margin-inline: auto;
	}

	.status {
		display: flex;
		align-items: center;
	}

	.pane {
		display: flex;
		flex-direction: column;
		min-width: 0;
		min-height: 0;
	}

	/*
	 * Stacked, the game takes the height that is going and its chain scrolls
	 * inside itself — which is what keeps the conversation on screen. Without
	 * this the chain grew the page one row per turn and pushed the only way
	 * into the chat below the fold exactly as the game got long enough to talk
	 * about, and a four-seat lobby did the same thing with its seat list.
	 *
	 * The lobby has no scroller of its own, so it is given one here. The board
	 * does not want one: the chain is the part that grows and it already
	 * scrolls, and a second scroller around it would move the word field.
	 */
	.pane.game {
		flex: 1;
		gap: var(--space-3);
		min-height: 0;
	}

	.online.room:not(.in-game) .pane.game {
		overflow-y: auto;
	}

	/* Stacked: a divider does the work the second column's whitespace does. */
	.pane.talk {
		flex: none;
		padding-top: var(--space-3);
		border-top: 1px solid var(--border);
	}

	/* Must match WIDE in the script above. */
	@media (min-width: 900px) {
		.online.room {
			display: grid;
			grid-template-columns: minmax(0, 1fr) minmax(0, 320px);
			align-items: stretch;
			gap: var(--space-6);
		}

		/* Each column scrolls on its own, so a long chain does not push the
		   conversation off the screen and a long conversation does not push
		   the word field off it. */
		.pane {
			overflow-y: auto;
		}

		.pane.talk {
			padding-top: 0;
			padding-left: var(--space-6);
			border-top: 0;
			border-left: 1px solid var(--border);
			overflow: hidden;
		}
	}

	h1 {
		margin: 0;
		font-size: 1.3rem;
	}

	.intro {
		margin: 0;
		color: var(--text-muted);
	}

	.choice {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}

	.choice-hint {
		margin: 0;
		color: var(--text-muted);
		font-size: var(--text-1);
	}

	.primary {
		min-height: 44px;
		padding: var(--space-4);
		border: 0;
		border-radius: var(--radius-sm);
		background: var(--accent);
		color: var(--accent-text);
		font-weight: 600;
		transition: background-color 150ms ease-out;
	}

	.primary:hover:not(:disabled) {
		background: var(--accent-hover);
	}

	.primary:active:not(:disabled) {
		background: var(--accent-pressed);
	}

	.primary:disabled {
		background: var(--surface-alt);
		color: var(--text-muted);
	}

	.join {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}

	.waiting {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-2);
		padding: var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface-alt);
	}

	.waiting p {
		margin: 0;
	}

	.waiting .counter {
		color: var(--text-muted);
		font-variant-numeric: tabular-nums;
	}

	.waiting button {
		min-height: 44px;
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface);
		font-weight: 600;
	}

	label {
		font-weight: 600;
		font-size: var(--text-2);
	}

	.row {
		display: flex;
		gap: var(--space-2);
	}

	input {
		flex: 1;
		min-width: 0;
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface);
		font-size: var(--text-3);
		letter-spacing: 0.1em;
		text-transform: uppercase;
	}

	.row button {
		min-height: 44px;
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface-alt);
		font-weight: 600;
	}

	.row button:disabled {
		color: var(--text-muted);
	}

	.hint {
		margin: 0;
		color: var(--text-muted);
		font-size: var(--text-2);
	}

	.hint.invalid {
		color: var(--danger);
	}

	.error,
	.notice {
		margin: 0;
		padding: var(--space-3) var(--space-3);
		border-radius: var(--radius-sm);
		font-size: var(--text-2);
	}

	.error {
		background: var(--danger-soft);
		color: var(--danger);
	}

	.notice {
		background: var(--surface-alt);
		color: var(--warn);
	}

	.back {
		align-self: flex-start;
		color: var(--text-muted);
		font-size: var(--text-2);
	}
</style>
