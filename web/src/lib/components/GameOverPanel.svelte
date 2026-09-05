<script>
	import { endReasonMessages, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/**
	 * onrematch is optional because the two modes differ: a bot always plays
	 * again, so the button starts the next game, while online play has to ask
	 * the other person first and offers it from the rematch prompt instead.
	 *
	 * @type {{ isRecord: boolean, onrematch?: () => void, onhome: () => void }}
	 */
	let { isRecord, onrematch, onhome } = $props();

	/** @type {{ iWon: boolean, reason: number, myScore: number, chainLength: number } | null} */
	const result = $derived(game.state.result);
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

		{#if isRecord}
			<p class="record">{t.newRecord}</p>
		{/if}

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

	.record {
		margin: 0;
		padding: 6px 12px;
		border-radius: 999px;
		background: var(--accent-soft);
		color: var(--accent);
		font-weight: 700;
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
