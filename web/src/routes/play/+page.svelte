<script>
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import ChainHistory from '$lib/components/ChainHistory.svelte';
	import ConnectionBadge from '$lib/components/ConnectionBadge.svelte';
	import CountdownRing from '$lib/components/CountdownRing.svelte';
	import GameOverPanel from '$lib/components/GameOverPanel.svelte';
	import ScoreBoard from '$lib/components/ScoreBoard.svelte';
	import WordInput from '$lib/components/WordInput.svelte';
	import { difficultyLabels, t } from '$lib/i18n/vi.js';
	import { Difficulty } from '$lib/proto/noitu/v1/game_pb.js';
	import { createBotSession } from '$lib/stores/bot-session.svelte.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { settings } from '$lib/stores/settings.svelte.js';
	import { resign, startBotGame, submitWord } from '$lib/ws/messages.js';
	import { Status, connect, connection, disconnect, send } from '$lib/ws/connection.svelte.js';

	/**
	 * The difficulty travels in the URL so a reload resumes the same ladder rung
	 * and the record comparison knows which one to compare against.
	 */
	const difficulty = $derived.by(() => {
		const raw = Number(page.url.searchParams.get('difficulty'));
		return difficultyLabels[raw] ? raw : Difficulty.MEDIUM;
	});

	const session = createBotSession({ start: (d) => send(startBotGame(d)) });

	let isRecord = $state(false);

	// Owns the socket and the game for as long as this screen is on. Entering
	// asks for a game; leaving gives the current one up rather than abandoning a
	// room that would keep its turn timer running and time the player out
	// somewhere they can no longer see.
	//
	// Reading `difficulty` makes a change of rung a teardown and a fresh game,
	// which is what changing the URL means.
	$effect(() => {
		const rung = difficulty;
		untrack(() => startGame(rung));

		return () => {
			if (game.state.phase === 'playing') send(resign());
			session.cancel();
			disconnect();
			game.reset();
		};
	});

	// The request is sent as soon as the socket can carry it. On a fresh load
	// that is after the handshake; on a rematch it is immediate.
	$effect(() => {
		const open = connection.status === Status.OPEN;
		untrack(() => session.flush(open));
	});

	// A finished game counts once. The write is untracked so the effect does not
	// depend on the record it just stored.
	$effect(() => {
		const result = game.state.result;
		untrack(() => {
			if (result) isRecord = session.score(result, difficulty, settings);
		});
	});

	/** @param {number} rung */
	function startGame(rung) {
		game.reset();
		session.reset();
		isRecord = false;
		session.request(rung);
		connect();
		session.flush(connection.status === Status.OPEN);
	}

	function rematch() {
		startGame(difficulty);
	}

	function goHome() {
		goto('/');
	}

	/**
	 * @param {string} word
	 * @returns {boolean} whether the word reached the server
	 */
	function play(word) {
		return send(submitWord(word, game.state.turnSeq));
	}

	function giveUp() {
		if (confirm(t.resignConfirm)) send(resign());
	}
</script>

<section class="play">
	<div class="top">
		<ConnectionBadge />
		<span class="mode">{difficultyLabels[difficulty]}</span>
	</div>

	<ScoreBoard opponentLabel={t.opponent} />

	{#if game.state.error}
		<p class="error" role="alert">
			{game.state.error}
			<button type="button" onclick={() => game.clearError()} aria-label={t.dismiss}>×</button>
		</p>
	{/if}

	{#if game.state.phase === 'over'}
		<GameOverPanel {isRecord} onrematch={rematch} onhome={goHome} />
	{:else}
		<div class="turn">
			<CountdownRing />
			<div class="prompt">
				<p class="who">{game.state.myTurn ? t.yourTurn : t.opponentTurn}</p>
				<p class="syllable">
					<span class="label">{t.currentSyllable}</span>
					<strong>{game.state.currentSyllable || '…'}</strong>
				</p>
			</div>
		</div>

		<WordInput onsubmit={play} />
	{/if}

	<ChainHistory />

	{#if game.state.phase === 'playing'}
		<button type="button" class="resign" onclick={giveUp}>{t.resign}</button>
	{/if}
</section>

<style>
	.play {
		display: flex;
		flex-direction: column;
		flex: 1;
		gap: 14px;
		min-height: 0;
		padding-bottom: 8px;
	}

	.top {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}

	.mode {
		color: var(--text-muted);
		font-size: 0.85rem;
	}

	.turn {
		display: flex;
		align-items: center;
		gap: 16px;
	}

	.prompt {
		min-width: 0;
	}

	.who {
		margin: 0 0 2px;
		color: var(--text-muted);
		font-size: 0.85rem;
	}

	.syllable {
		display: flex;
		flex-direction: column;
		margin: 0;
	}

	.syllable .label {
		color: var(--text-muted);
		font-size: 0.75rem;
	}

	.syllable strong {
		font-size: 1.6rem;
		line-height: 1.2;
	}

	.error {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		margin: 0;
		padding: 10px 12px;
		border-radius: var(--radius-sm);
		background: var(--danger-soft);
		color: var(--danger);
		font-size: 0.9rem;
	}

	.error button {
		border: 0;
		background: none;
		font-size: 1.1rem;
		line-height: 1;
	}

	.resign {
		align-self: center;
		padding: 8px 16px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--text-muted);
		font-size: 0.85rem;
	}
</style>
