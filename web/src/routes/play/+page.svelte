<script>
	import { untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import GameBoard from '$lib/components/GameBoard.svelte';
	import GameOverPanel from '$lib/components/GameOverPanel.svelte';
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

<GameBoard
	opponentLabel={t.opponent}
	modeLabel={difficultyLabels[difficulty]}
	onsubmit={play}
	onresign={giveUp}
>
	{#snippet gameOver()}
		<!-- A bot always plays again, so there is nothing to negotiate: the
		     button starts the next game rather than offering one. -->
		<GameOverPanel {isRecord} onrematch={rematch} onhome={goHome} />
	{/snippet}
</GameBoard>
