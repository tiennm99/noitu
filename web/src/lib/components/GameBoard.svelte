<script>
	import ChainHistory from '$lib/components/ChainHistory.svelte';
	import ConnectionBadge from '$lib/components/ConnectionBadge.svelte';
	import CountdownRing from '$lib/components/CountdownRing.svelte';
	import ScoreBoard from '$lib/components/ScoreBoard.svelte';
	import WordInput from '$lib/components/WordInput.svelte';
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { Status, connection, reconnectNow } from '$lib/ws/connection.svelte.js';

	/**
	 * The board itself, shared by both modes. It renders the store and nothing
	 * else; what a finished game offers differs between bot and online play, so
	 * that arrives as a snippet rather than as a branch in here.
	 *
	 * @type {{
	 *   modeLabel?: string,
	 *   onsubmit: (word: string) => boolean,
	 *   onresign: () => void,
	 *   gameOver: import('svelte').Snippet,
	 *   banner?: import('svelte').Snippet
	 * }}
	 */
	let { modeLabel = '', onsubmit, onresign, gameOver, banner } = $props();

	/** How long an armed resign button waits before it goes back to being safe. */
	const ARM_MS = 4000;

	let arming = $state(false);
	/** @type {any} */
	let armTimer;

	// Whose turn it is, said by name. With four people at the table "the
	// opponent is thinking" stops naming anybody.
	const turnLabel = $derived.by(() => {
		if (game.state.myTurn) return t.yourTurn;
		const name = game.nameOf(game.state.turnPlayerId);
		return name ? fill(t.playerTurn, { name }) : t.opponentTurn;
	});

	const offline = $derived(connection.status !== Status.OPEN);

	// Giving up is a move: it is what a player plays instead of a word, so it
	// is offered on their turn and no other. Out of turn the way out of a game
	// is to leave the room, which the lobby's own button does.
	const canResign = $derived(game.state.myTurn && !offline);

	// An armed button that loses the turn goes back to being safe: the second
	// press would arrive at a button that is no longer the one the player was
	// looking at.
	$effect(() => {
		if (canResign) return;
		clearTimeout(armTimer);
		arming = false;
	});

	/**
	 * Two presses, in place of a native confirm().
	 *
	 * confirm() blocks the main thread, which stops the countdown's animation
	 * frame loop while the server's deadline keeps running: hesitating over the
	 * dialog can cost the turn it was protecting. This keeps the board on screen
	 * and the clock moving, and disarms itself so a stray tap does not lie in
	 * wait.
	 */
	function armOrResign() {
		if (arming) {
			clearTimeout(armTimer);
			arming = false;
			onresign();
			return;
		}
		arming = true;
		clearTimeout(armTimer);
		armTimer = setTimeout(() => (arming = false), ARM_MS);
	}

	$effect(() => () => clearTimeout(armTimer));
</script>

<section class="board" data-phase={game.state.phase}>
	<div class="top">
		<ConnectionBadge />
		{#if modeLabel}<span class="mode">{modeLabel}</span>{/if}
	</div>

	<!-- Not once the game is over: the result panel below carries the same
	     names and scores as a ranked table, and two of them is one screenful
	     of duplication between the result and the lobby's own buttons. -->
	{#if game.state.phase !== 'over'}
		<ScoreBoard />
	{/if}

	{#if banner}{@render banner()}{/if}

	<!--
		Not a live region: the connection badge above already announces the same
		change, and saying it twice is worse than saying it once. This is the
		visible half — and the only place the player can do anything about it,
		since the backoff otherwise runs up to eight seconds while their turn
		does not wait.
	-->
	{#if offline && game.state.phase === 'playing'}
		<p class="offline">
			{t.reconnecting}
			<button type="button" onclick={() => reconnectNow()}>{t.retry}</button>
		</p>
	{/if}

	{#if game.state.error}
		<p class="error" role="alert">
			{game.state.error}
			<button
				type="button"
				class="icon-button"
				onclick={() => game.clearError()}
				aria-label={t.dismiss}>×</button
			>
		</p>
	{/if}

	{#if game.state.phase === 'over'}
		{@render gameOver()}
	{:else}
		<div class="turn">
			<CountdownRing />
			<div class="prompt">
				<!--
					The turn changing is the one state change the whole game hangs
					on, and it reached nobody who was not looking at this line: a
					screen-reader player, or anybody reading the chat beside the
					board, learned it from the clock forfeiting them.
				-->
				<p
					class="who"
					class:mine={game.state.myTurn}
					role="status"
					aria-live="polite"
					aria-atomic="true"
					data-testid="turn-indicator"
				>
					{turnLabel}
				</p>
				<p class="syllable">
					<span class="label">{t.currentSyllable}</span>
					<strong data-testid="current-syllable">{game.state.currentSyllable || '…'}</strong>
				</p>
			</div>
		</div>

		<!-- A player who has been knocked out watches the rest of it: the chain,
		     the clock and the chat all keep working, and only the one thing
		     they can no longer do goes away. -->
		{#if game.iAmOut}
			<p class="spectating">{t.spectating}</p>
		{:else}
			<WordInput {onsubmit} />
		{/if}
	{/if}

	<!-- Above the chain, not below it: the chain is the one part of the board
	     that grows, and a button under it walks off the bottom of the screen
	     exactly as the game gets long enough to want to give up on. -->
	{#if game.state.phase === 'playing' && !game.iAmOut}
		<button
			type="button"
			class="resign"
			class:arming
			disabled={!canResign}
			onclick={armOrResign}
		>
			{arming ? t.resignSure : t.resign}
		</button>
	{/if}

	<ChainHistory />
</section>

<style>
	.board {
		display: flex;
		flex-direction: column;
		flex: 1;
		gap: var(--space-3);
		min-height: 0;
		padding-bottom: var(--space-2);
	}

	.top {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}

	.mode {
		color: var(--text-muted);
		font-size: var(--text-4);
	}

	.turn {
		display: flex;
		align-items: center;
		gap: var(--space-4);
	}

	.prompt {
		min-width: 0;
	}

	.who {
		margin: 0 0 var(--space-1);
		color: var(--text-muted);
		font-size: var(--text-4);
	}

	/* The player's own turn, said loudly enough to catch the eye that is in the
	   chat column beside the board. */
	.who.mine {
		color: var(--text);
		font-size: var(--text-6);
		font-weight: 700;
	}

	.syllable {
		display: flex;
		flex-direction: column;
		margin: 0;
	}

	.syllable .label {
		margin-bottom: 2px;
		color: var(--text-muted);
		font-size: var(--text-2);
	}

	/* The one glyph read every single turn, so it gets the headroom: a stacked
	   Vietnamese tone mark on ệ or ộ rides into the label above it at 1.2. */
	.syllable strong {
		font-size: 1.6rem;
		line-height: 1.35;
	}

	.error,
	.offline {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		margin: 0;
		padding: 10px var(--space-3);
		border-radius: var(--radius-sm);
		font-size: var(--text-5);
	}

	.error {
		background: var(--danger-soft);
		color: var(--danger);
	}

	.offline {
		background: var(--surface-alt);
		color: var(--warn);
	}

	.offline button {
		flex: none;
		min-height: 44px;
		padding: var(--space-2) var(--space-4);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		margin: -8px 0;
		background: var(--surface);
		color: var(--text);
		font-size: var(--text-4);
		font-weight: 600;
	}

	.spectating {
		margin: 0;
		padding: var(--space-3);
		border: 1px dashed var(--border);
		border-radius: var(--radius-sm);
		color: var(--text-muted);
		text-align: center;
	}

	/* Right of the board and away from the input: giving up is the one thing
	   here nobody should hit by accident while typing. Danger coloured because
	   it ends the game, subordinate because it is not the way to play it. */
	.resign {
		align-self: flex-end;
		/* Below the 44px the rest of the controls keep, deliberately: this is
		   the one button here nobody is trying to hit, it takes two presses to
		   do anything, and at full size it read as an offer rather than as the
		   way out. Still its own outlined block in danger colour, so it is
		   plainly findable rather than hidden. */
		min-height: 32px;
		padding: var(--space-1) var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--danger);
		font-size: var(--text-3);
		transition: background-color 150ms ease-out;
	}

	.resign:hover:enabled {
		background: var(--danger-soft);
	}

	/* Off turn: still there, so the way out of the game does not appear and
	   disappear under the player's thumb every handover, but plainly not the
	   thing to press yet. */
	.resign:disabled {
		border-color: var(--border);
		color: var(--text-muted);
	}

	/* Armed, and saying so: the second press is the one that ends the game. */
	.resign.arming {
		border-color: var(--danger);
		background: var(--danger-soft);
		font-weight: 600;
	}
</style>
