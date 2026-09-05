<script>
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import GameBoard from '$lib/components/GameBoard.svelte';
	import GameOverPanel from '$lib/components/GameOverPanel.svelte';
	import NicknameInput from '$lib/components/NicknameInput.svelte';
	import OpponentStatus from '$lib/components/OpponentStatus.svelte';
	import RematchPrompt from '$lib/components/RematchPrompt.svelte';
	import WaitingRoom from '$lib/components/WaitingRoom.svelte';
	import { t } from '$lib/i18n/vi.js';
	import { isRoomCode, normalizeRoomCode, ROOM_CODE_LENGTH } from '$lib/room-code.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { createRoom, joinRoom, requestRematch, resign, submitWord } from '$lib/ws/messages.js';
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

	let codeInput = $state(normalizeRoomCode(page.url.searchParams.get('code') ?? ''));
	let codeError = $state('');
	// True while the only reason this screen has a socket is to reclaim a game
	// it might no longer be able to reclaim.
	let resuming = $state(false);

	const inviteCode = $derived(normalizeRoomCode(page.url.searchParams.get('code') ?? ''));
	const playing = $derived(game.state.phase === 'playing' || game.state.phase === 'over');

	// The resume worked, so nothing that happens from here is its fault.
	$effect(() => {
		if (playing || game.state.phase === 'waiting') untrack(() => (resuming = false));
	});

	// Owns the socket while this screen is on, exactly as the bot screen does.
	// An invite link is a request to join, so it is honoured on arrival rather
	// than making the player press a button they did not ask for.
	$effect(() => {
		const code = inviteCode;
		untrack(() => {
			game.reset();
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

	function leave() {
		goto('/');
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

<section class="online">
	{#if playing}
		<GameBoard
			opponentLabel={game.state.opponentName || t.opponent}
			modeLabel={game.state.roomCode}
			onsubmit={play}
			onresign={giveUp}
		>
			{#snippet banner()}
				<OpponentStatus />
			{/snippet}
			{#snippet gameOver()}
				<!-- No rematch button on the panel: two people have to agree, and
				     an offer is not always open. The prompt below appears only
				     while one is, so it is the only thing that can ask. -->
				<GameOverPanel isRecord={false} onhome={leave} />
				<RematchPrompt onaccept={() => send(requestRematch())} />
			{/snippet}
		</GameBoard>
	{:else if game.state.phase === 'waiting'}
		<WaitingRoom code={game.state.roomCode} onleave={leave} />
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
