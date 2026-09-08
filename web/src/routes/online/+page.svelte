<script>
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import GameBoard from '$lib/components/GameBoard.svelte';
	import GameOverPanel from '$lib/components/GameOverPanel.svelte';
	import ChatPanel from '$lib/components/ChatPanel.svelte';
	import Lobby from '$lib/components/Lobby.svelte';
	import NicknameInput from '$lib/components/NicknameInput.svelte';
	import PlayerStatus from '$lib/components/PlayerStatus.svelte';
	import { t } from '$lib/i18n/vi.js';
	import { isRoomCode, normalizeRoomCode, ROOM_CODE_LENGTH } from '$lib/room-code.js';
	import { game } from '$lib/stores/game.svelte.js';
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

	const inviteCode = $derived(normalizeRoomCode(page.url.searchParams.get('code') ?? ''));
	const playing = $derived(game.state.phase === 'playing' || game.state.phase === 'over');
	// A seat in a room, whichever phase it is in. Both are the same layout —
	// the game or the lobby on one side, the conversation on the other.
	const inRoom = $derived(playing || game.state.phase === 'lobby');

	// The resume worked, so nothing that happens from here is its fault.
	$effect(() => {
		if (playing || game.state.phase === 'lobby') untrack(() => (resuming = false));
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
			if (isRoomCode(code)) {
				request({ kind: 'join', code });
			} else if (hasStoredSession()) {
				// This tab was already in a game. Reconnecting restores it, which
				// is what a player who refreshed mid-game is expecting; the
				// nickname is already settled, so there is nothing to wait for.
				resuming = true;
				connect();
			}
		});

		return () => {
			if (game.state.phase === 'playing') send(resign());
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

	// A resume that the server cannot honour is not something the player did.
	// Reporting it would open the lobby with a red banner about a game they
	// have already left behind, so the token is dropped quietly instead.
	$effect(() => {
		const failed = resuming && !!game.state.error;
		untrack(() => {
			if (!failed) return;
			resuming = false;
			game.clearError();
			forgetSession();
		});
	});

	/** @param {{ kind: 'create' } | { kind: 'join', code: string }} req */
	function request(req) {
		codeError = '';
		resuming = false;
		game.clearError();
		pending = req;
		// The handshake carries the nickname as it stands now, which is why the
		// connection waits until the player has actually asked for a room.
		connect();
		flush(connection.status === Status.OPEN);
	}

	/** @param {boolean} isOpen */
	function flush(isOpen) {
		if (!pending || !isOpen) return;
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
		request({ kind: 'join', code });
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
	 * @param {boolean} ready
	 */
	function ready(ready) {
		send(setReady(ready));
	}

	function start() {
		send(startGame());
	}

	/** @param {string} playerId */
	function kick(playerId) {
		if (confirm(t.kickConfirm)) send(kickPlayer(playerId));
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
		if (confirm(t.resignConfirm)) send(resign());
	}
</script>

<section class="online" class:room={inRoom}>
	{#if inRoom}
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

		<div class="pane talk">
			<ChatPanel
				collapsible={playing && !wide}
				column={wide}
				errors={!playing}
				onsend={say}
			/>
		</div>
	{:else}
		<h1>{t.onlineTitle}</h1>
		<p class="intro">{t.onlineIntro}</p>

		<NicknameInput />

		{#if game.state.error}
			<p class="error" role="alert" data-testid="join-error">{game.state.error}</p>
		{/if}

		<button type="button" class="primary" onclick={() => request({ kind: 'create' })}>
			{t.createRoom}
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
				<button type="submit">{t.joinRoom}</button>
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
		gap: 16px;
		min-height: 0;
		padding-top: 12px;
	}

	/* Joining is a form, not a room: it keeps a form's width whatever the
	   screen the two columns were widened for. */
	.online:not(.room) {
		max-width: 480px;
	}

	.pane {
		display: flex;
		flex-direction: column;
		min-width: 0;
		min-height: 0;
	}

	.pane.game {
		gap: 16px;
	}

	/* Stacked: a divider does the work the second column's whitespace does. */
	.pane.talk {
		padding-top: 12px;
		border-top: 1px solid var(--border);
	}

	/* Must match WIDE in the script above. */
	@media (min-width: 900px) {
		.online.room {
			display: grid;
			grid-template-columns: minmax(0, 1fr) minmax(0, 320px);
			align-items: stretch;
			gap: 24px;
		}

		/* Each column scrolls on its own, so a long chain does not push the
		   conversation off the screen and a long conversation does not push
		   the word field off it. */
		.pane {
			overflow-y: auto;
		}

		/* Stacked, the game is as tall as it is and the conversation follows
		   it directly. Given a column, it takes the height of one. */
		.pane.game {
			flex: 1;
		}

		.pane.talk {
			padding-top: 0;
			padding-left: 24px;
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
		padding: 14px;
		border: 0;
		border-radius: var(--radius-sm);
		background: var(--accent);
		color: var(--accent-text);
		font-weight: 600;
	}

	.join {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}

	label {
		font-weight: 600;
		font-size: 0.9rem;
	}

	.row {
		display: flex;
		gap: 8px;
	}

	input {
		flex: 1;
		min-width: 0;
		padding: 12px 14px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		font-size: 1rem;
		letter-spacing: 0.1em;
		text-transform: uppercase;
	}

	input:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}

	.row button {
		padding: 12px 18px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface-alt);
		font-weight: 600;
	}

	.hint {
		margin: 0;
		color: var(--text-muted);
		font-size: 0.8rem;
	}

	.hint.invalid {
		color: var(--danger);
	}

	.error {
		margin: 0;
		padding: 10px 12px;
		border-radius: var(--radius-sm);
		background: var(--danger-soft);
		color: var(--danger);
		font-size: 0.9rem;
	}

	.back {
		align-self: flex-start;
		color: var(--text-muted);
		font-size: 0.9rem;
	}
</style>
