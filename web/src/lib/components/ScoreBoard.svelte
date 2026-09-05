<script>
	import { t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/** @type {{ opponentLabel?: string }} */
	let { opponentLabel = t.opponent } = $props();

	const me = $derived(game.state.nickname || t.you);
</script>

<div class="board">
	<div class="side" class:active={game.state.myTurn}>
		<span class="who">{me}</span>
		<span class="score">{game.state.myScore}</span>
	</div>
	<span class="sep" aria-hidden="true">–</span>
	<div class="side" class:active={game.state.phase === 'playing' && !game.state.myTurn}>
		<span class="who">{opponentLabel}</span>
		<span class="score">{game.state.opponentScore}</span>
	</div>
</div>

<style>
	.board {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 12px;
	}

	.side {
		display: flex;
		flex-direction: column;
		align-items: center;
		min-width: 0;
		padding: 6px 12px;
		border: 1px solid transparent;
		border-radius: var(--radius-sm);
		flex: 1;
	}

	/* The active side is whose turn it is, so the board doubles as the turn
	   indicator rather than needing a second one. */
	.side.active {
		border-color: var(--accent);
		background: var(--accent-soft);
	}

	.who {
		max-width: 100%;
		overflow: hidden;
		color: var(--text-muted);
		font-size: 0.8rem;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.score {
		font-size: 1.4rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}

	.sep {
		color: var(--text-muted);
	}
</style>
