<script>
	import ChainHistory from '$lib/components/ChainHistory.svelte';
	import ConnectionBadge from '$lib/components/ConnectionBadge.svelte';
	import CountdownRing from '$lib/components/CountdownRing.svelte';
	import ScoreBoard from '$lib/components/ScoreBoard.svelte';
	import WordInput from '$lib/components/WordInput.svelte';
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

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

	// Whose turn it is, said by name. With four people at the table "the
	// opponent is thinking" stops naming anybody.
	const turnLabel = $derived.by(() => {
		if (game.state.myTurn) return t.yourTurn;
		const name = game.nameOf(game.state.turnPlayerId);
		return name ? fill(t.playerTurn, { name }) : t.opponentTurn;
	});
</script>

<section class="board" data-phase={game.state.phase}>
	<div class="top">
		<ConnectionBadge />
		{#if modeLabel}<span class="mode">{modeLabel}</span>{/if}
	</div>

	<ScoreBoard />

	{#if banner}{@render banner()}{/if}

	{#if game.state.error}
		<p class="error" role="alert">
			{game.state.error}
			<button type="button" onclick={() => game.clearError()} aria-label={t.dismiss}>×</button>
		</p>
	{/if}

	{#if game.state.phase === 'over'}
		{@render gameOver()}
	{:else}
		<div class="turn">
			<CountdownRing />
			<div class="prompt">
				<p class="who" data-testid="turn-indicator">{turnLabel}</p>
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
		<button type="button" class="resign" onclick={onresign}>{t.resign}</button>
	{/if}

	<ChainHistory />
</section>

<style>
	.board {
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

	/* A 44px target on a glyph that is a fraction of that: the padding is
	   negative-margined back out so the banner keeps its height. */
	.error button {
		flex: none;
		width: 44px;
		height: 44px;
		margin: -12px -6px;
		border: 0;
		border-radius: var(--radius-sm);
		background: none;
		color: inherit;
		font-size: 1.1rem;
		line-height: 1;
	}

	.spectating {
		margin: 0;
		padding: 12px;
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
		min-height: 44px;
		padding: 8px 16px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--danger);
		font-size: 0.85rem;
		transition: background-color 150ms ease-out;
	}

	.resign:hover {
		background: var(--danger-soft);
	}
</style>
