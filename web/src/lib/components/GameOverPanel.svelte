<script>
	import { chainToText, downloadText, historyFilename } from '$lib/history-export.js';
	import { endReasonMessages, fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/**
	 * onrematch is optional because the two modes differ: a bot always plays
	 * again, so the button starts the next game, while online play has to ask
	 * the other person first and offers it from the rematch prompt instead.
	 *
	 * @type {{ isRecord: boolean, onrematch?: () => void, onhome: () => void }}
	 */
	let { isRecord, onrematch, onhome } = $props();

	/** @type {{ iWon: boolean, reason: number, myScore: number, chainLength: number, suggestions: string[] } | null} */
	const result = $derived(game.state.result);

	/** Hands the finished chain to the player as a text file to keep. */
	function exportHistory() {
		const at = new Date();
		const text = chainToText({
			chain: game.state.chain,
			result,
			opponentLabel: game.state.opponentName || t.opponent,
			at
		});
		downloadText(historyFilename(at), text);
	}
</script>

{#if result}
	<div class="panel" role="group" aria-label={result.iWon ? t.won : t.lost}>
		<h2 class:won={result.iWon}>{result.iWon ? t.won : t.lost}</h2>

		{#if endReasonMessages[result.reason]}
			<p class="reason">{endReasonMessages[result.reason]}</p>
		{/if}

		<dl class="stats">
			<div>
				<dt>{t.finalScore}</dt>
				<dd>{result.myScore}</dd>
			</div>
			<div>
				<dt>{t.chainLength}</dt>
				<dd>{result.chainLength}</dd>
			</div>
		</dl>

		{#if !result.iWon}
			<!-- Only the loser is shown this, and only they were sent it. Losing
			     without ever learning what the position wanted is the part that
			     stings; an empty list says the position had nothing, which is
			     worth hearing too. -->
			{#if result.suggestions.length > 0}
				<div class="suggestions">
					<h3>{t.suggestionsTitle}</h3>
					<ul>
						{#each result.suggestions as word}
							<li>{word}</li>
						{/each}
					</ul>
				</div>
			{:else}
				<p class="dead-end">
					{fill(t.noSuggestions, { syllable: game.state.currentSyllable })}
				</p>
			{/if}
		{/if}

		{#if isRecord}
			<p class="record">{t.newRecord}</p>
		{/if}

		<button type="button" class="export" onclick={exportHistory}>{t.exportHistory}</button>

		<div class="actions">
			{#if onrematch}
				<button type="button" class="primary" onclick={onrematch}>{t.rematch}</button>
			{/if}
			<button type="button" onclick={onhome}>{t.home}</button>
		</div>
	</div>
{/if}

<style>
	.panel {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 20px;
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--surface);
		box-shadow: var(--shadow);
		text-align: center;
	}

	h2 {
		margin: 0;
		color: var(--danger);
		font-size: 1.4rem;
	}

	h2.won {
		color: var(--accent);
	}

	.reason {
		margin: 0;
		color: var(--text-muted);
	}

	.stats {
		display: flex;
		justify-content: center;
		gap: 28px;
		margin: 0;
	}

	dt {
		color: var(--text-muted);
		font-size: 0.8rem;
	}

	dd {
		margin: 0;
		font-size: 1.4rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}

	.suggestions h3 {
		margin: 0 0 6px;
		color: var(--text-muted);
		font-size: 0.8rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}

	.suggestions ul {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: 6px;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.suggestions li {
		padding: 6px 12px;
		border: 1px solid var(--border);
		border-radius: 999px;
		background: var(--surface-alt);
		font-weight: 600;
	}

	.dead-end {
		margin: 0;
		color: var(--text-muted);
	}

	.record {
		margin: 0;
		padding: 6px 12px;
		border-radius: 999px;
		background: var(--accent-soft);
		color: var(--accent);
		font-weight: 700;
	}

	.export {
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--text-muted);
		font-size: 0.9rem;
		font-weight: 600;
	}

	.actions {
		display: flex;
		gap: 8px;
	}

	.actions button {
		flex: 1;
		padding: 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		font-weight: 600;
	}

	.actions .primary {
		border-color: transparent;
		background: var(--accent);
		color: var(--accent-text);
	}
</style>
