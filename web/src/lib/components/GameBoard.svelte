<script>
	import ChainHistory from '$lib/components/ChainHistory.svelte';
	import ConnectionBadge from '$lib/components/ConnectionBadge.svelte';
	import CountdownRing from '$lib/components/CountdownRing.svelte';
	import ScoreBoard from '$lib/components/ScoreBoard.svelte';
	import WordInput from '$lib/components/WordInput.svelte';
	import { t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/**
	 * The board itself, shared by both modes. It renders the store and nothing
	 * else; what a finished game offers differs between bot and online play, so
	 * that arrives as a snippet rather than as a branch in here.
	 *
	 * @type {{
	 *   opponentLabel: string,
	 *   modeLabel?: string,
	 *   onsubmit: (word: string) => boolean,
	 *   onresign: () => void,
	 *   gameOver: import('svelte').Snippet,
	 *   banner?: import('svelte').Snippet,
	 *   chat?: import('svelte').Snippet
	 * }}
	 */
	let { opponentLabel, modeLabel = '', onsubmit, onresign, gameOver, banner, chat } = $props();
</script>

<section class="board" data-phase={game.state.phase}>
	<div class="top">
		<ConnectionBadge />
		{#if modeLabel}<span class="mode">{modeLabel}</span>{/if}
	</div>

	<ScoreBoard {opponentLabel} />

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
				<p class="who" data-testid="turn-indicator">
					{game.state.myTurn ? t.yourTurn : t.opponentTurn}
				</p>
				<p class="syllable">
					<span class="label">{t.currentSyllable}</span>
					<strong data-testid="current-syllable">{game.state.currentSyllable || '…'}</strong>
				</p>
			</div>
		</div>

		<WordInput {onsubmit} />
	{/if}

	<ChainHistory />

	<!-- Online play passes a chat panel; the bot screen passes none, which is
	     how "a bot game has no chat" stays a fact about the markup rather than
	     a condition somebody has to remember to check. -->
	{#if chat}{@render chat()}{/if}

	{#if game.state.phase === 'playing'}
		<button type="button" class="resign" onclick={onresign}>{t.resign}</button>
	{/if}
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
