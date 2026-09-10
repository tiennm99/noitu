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
	import { fill, t } from '$lib/i18n/vi.js';
	import { isRoomCode, normalizeRoomCode, ROOM_CODE_LENGTH } from '$lib/room-code.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { settings } from '$lib/stores/settings.svelte.js';
	import {
		createRoom,
		joinRoom,
		kickPlayer,
		leaveRoom,
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
	 * What the player asked for, held until the socket can carry it. Same shape
	 * as the bot screen's request latch and for the same reason: a request is
	 * something the player did, not a condition to be re-derived from the board.
	 *
	 * @type {{ kind: 'create' } | { kind: 'join', code: string } | null}
	 */
	let pending = $state(null);

	/**
	 * How long a held request waits before the screen stops saying "connecting"
	 * and starts saying something the player can act on. The backoff is capped
	 * at eight seconds and never gives up, so without this the screen would
	 * claim to be connecting for as long as the player was willing to watch it.
	 */
	const STALL_MS = 5000;

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

	let codeInput = $state(normalizeRoomCode(page.url.searchParams.get('code') ?? ''));
	let codeError = $state('');
	// True while the only reason this screen has a socket is to reclaim a game
	// it might no longer be able to reclaim.
	let resuming = $state(false);
	// An invite link arrived before this player had a name. Asking is one extra
	// tap, and the alternative is being seated as "Người chơi" with no way to
	// fix it from inside the room.
	let needName = $state(false);
	let stalled = $state(false);

	const inviteCode = $derived(normalizeRoomCode(page.url.searchParams.get('code') ?? ''));
	const playing = $derived(game.state.phase === 'playing' || game.state.phase === 'over');
	// A seat in a room, whichever phase it is in. Both are the same layout —
	// the game or the lobby on one side, the conversation on the other.
	const inRoom = $derived(playing || game.state.phase === 'lobby');
	const named = $derived(settings.state.nickname.trim().length > 0);

	// The resume worked, so nothing that happens from here is its fault — and
	// an invite code held behind it has been answered by arriving in a room.
	$effect(() => {
		if (inRoom)
			untrack(() => {
				resuming = false;
				pending = null;
			});
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
				resuming = true;
				if (isRoomCode(code)) pending = { kind: 'join', code };
				connect();
			} else if (isRoomCode(code)) {
				if (named) {
					request({ kind: 'join', code });
				} else {
					// Held, not sent. The name field is already on this screen and
					// the code is already in its field, so this is one button.
					needName = true;
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
			pending = null;
			disconnect();
			game.reset();
			game.clearChat();
		};
	});

	// Held requests go out once the handshake has landed.
	$effect(() => {
		const open = connection.status === Status.OPEN;
		untrack(() => flush(open));
	});

	// A request that has been waiting on a socket for longer than a player will
	// believe. Timed from the request rather than from the status, because a
	// backoff cycles between "reconnecting" and "connecting" indefinitely and
	// neither of them is news.
	$effect(() => {
		const waiting = !!pending && connection.status !== Status.OPEN;
		if (!waiting) {
			stalled = false;
			return;
		}
		const timer = setTimeout(() => (stalled = true), STALL_MS);
		return () => clearTimeout(timer);
	});

	// A resume that the server cannot honour is not something the player did.
	// Reporting it would open the lobby with a red banner about a game they
	// have already left behind, so the token is dropped quietly instead — and
	// an invite code held behind the resume is spent now.
	$effect(() => {
		const failed = resuming && !!game.state.error;
		untrack(() => {
			if (!failed) return;
			resuming = false;
			game.clearError();
			forgetSession();
			if (pending && !named) {
				// The link was for somebody who has still not given a name.
				needName = true;
				pending = null;
				return;
			}
			flush(connection.status === Status.OPEN);
		});
	});

	/** @param {{ kind: 'create' } | { kind: 'join', code: string }} req */
	function request(req) {
		codeError = '';
		resuming = false;
		needName = false;
		stalled = false;
		game.clearError();
		pending = req;
		// The handshake carries the nickname as it stands now, which is why the
		// connection waits until the player has actually asked for a room.
		connect();
		flush(connection.status === Status.OPEN);
	}

	/** @param {boolean} isOpen */
	function flush(isOpen) {
		// Nothing goes out while a resume is in flight: the seat this tab is
		// reclaiming may be in the very room the held code names.
		if (!pending || !isOpen || resuming) return;
		// Cleared only once the socket has taken it, so a request made during a
		// reconnect is carried by the next open connection rather than lost.
		const sent = pending.kind === 'create' ? send(createRoom()) : send(joinRoom(pending.code));
		if (sent) pending = null;
	}

	function join() {
		const code = normalizeRoomCode(codeInput);
		if (!isRoomCode(code)) {
			codeError = t.roomCodeInvalid;
			return;
		}
		if (!named) {
			needName = true;
			return;
		}
		request({ kind: 'join', code });
	}

	function create() {
		request({ kind: 'create' });
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
	 *
	 * @param {boolean} ready
	 * @returns {boolean}
	 */
	function ready(ready) {
		return send(setReady(ready));
	}

	/** @returns {boolean} */
	function start() {
		return send(startGame());
	}

	/**
	 * @param {string} playerId
	 * @returns {boolean}
	 */
	function kick(playerId) {
		// The lobby arms this with a second press of the same button; a native
		// confirm() would block the frame loop the countdown runs on.
		return send(kickPlayer(playerId));
	}

	function leave() {
		send(leaveRoom());
		game.leave();
		pending = null;
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
				<GameBoard modeLabel={game.state.roomCode} onsubmit={play} onresign={giveUp}>
					{#snippet banner()}
						<PlayerStatus />
					{/snippet}
					{#snippet gameOver()}
						<!-- No rematch button on the panel: the room is still here, and
						     the next game is agreed in the lobby below exactly as the
						     last one was. -->
						<GameOverPanel isRecord={false} onhome={goHome} />
						<Lobby compact onready={ready} onstart={start} onkick={kick} onleave={leave} />
					{/snippet}
				</GameBoard>
			{:else}
				<Lobby onready={ready} onstart={start} onkick={kick} onleave={leave} />
			{/if}
		</div>

		<!-- errors are not routed here any more: the lobby draws its own, beside
		     the button that produced them.

		     Folded only during a game on a narrow screen. In the lobby the log
		     stays open: waiting in a room is mostly what the conversation is
		     for, and the pane below keeps it on screen now. -->
		<div class="pane talk">
			<ChatPanel collapsible={playing && !wide} column={wide} onsend={say} />
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

		{#if needName}
			<p class="notice" role="alert" data-testid="name-needed">{t.nicknameNeeded}</p>
		{/if}

		{#if game.state.error}
			<p class="error" role="alert" data-testid="join-error">{game.state.error}</p>
		{/if}

		{#if stalled}
			<p class="error" role="alert" data-testid="connect-stalled">{t.connectStalled}</p>
		{/if}

		<!-- Disabled while a request is in flight. Every impatient tap used to
		     send a real CreateRoom, and the fifth one came back as "you are
		     creating rooms too quickly" to a player who thought they had tapped
		     nothing at all. -->
		<button type="button" class="primary" disabled={!!pending} onclick={create}>
			{pending?.kind === 'create' ? t.connecting : t.createRoom}
		</button>

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
				<button type="submit" disabled={!!pending}>
					{pending?.kind === 'join' ? t.connecting : t.joinRoom}
				</button>
			</div>
			<p class="hint" class:invalid={codeError}>{codeError || t.roomCodeHint}</p>
		</form>

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

	.primary {
		min-height: 44px;
		padding: 14px;
		border: 0;
		border-radius: var(--radius-sm);
		background: var(--accent);
		color: var(--accent-text);
		font-weight: 600;
	}

	.primary:disabled {
		background: var(--surface-alt);
		color: var(--text-muted);
	}

	.join {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}

	label {
		font-weight: 600;
		font-size: var(--text-5);
	}

	.row {
		display: flex;
		gap: var(--space-2);
	}

	input {
		flex: 1;
		min-width: 0;
		padding: var(--space-3) 14px;
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface);
		font-size: var(--text-6);
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
		font-size: var(--text-3);
	}

	.hint.invalid {
		color: var(--danger);
	}

	.error,
	.notice {
		margin: 0;
		padding: 10px var(--space-3);
		border-radius: var(--radius-sm);
		font-size: var(--text-5);
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
		font-size: var(--text-5);
	}
</style>
