<script>
	import { t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/**
	 * The table of the game on screen: one row per player, in turn order.
	 *
	 * It doubles as the turn indicator — the row that is lit is the player to
	 * act — which is why there is no second one anywhere on the board.
	 */
	const s = $derived(game.state);
	// Standings once the game is over, so the board settles into the result
	// rather than freezing on the last position.
	const players = $derived(s.phase === 'over' && s.standings.length ? s.standings : s.gamePlayers);
	// The series score, which belongs to the room rather than to this game. A
	// bot game is played in a room with no seating to speak of, so there is
	// nothing to tally and the row says nothing about one.
	const series = $derived(s.roomPlayers.length > 0);
</script>

<ul class="board" data-testid="scoreboard">
	{#each players as player (player.playerId)}
		<li
			class="side"
			class:active={s.phase === 'playing' && player.playerId === s.turnPlayerId}
			class:out={player.eliminated}
			class:me={player.isMe}
		>
			<span class="who">
				{player.isMe ? s.nickname || t.you : player.name || t.someone}
				{#if !player.connected && !player.eliminated}
					<span class="away" title={t.offline}>⚠</span>
				{/if}
			</span>
			<span class="score">{player.score}</span>
			{#if series}
				<span class="series" data-testid={`series-${player.playerId}`}>
					{t.winsLabel} {game.winsOf(player.playerId)}
				</span>
			{/if}
			{#if player.rank === 1}
				<span class="badge win">{t.winnerBadge}</span>
			{:else if player.eliminated}
				<span class="badge">{t.eliminated}</span>
			{/if}
		</li>
	{/each}
</ul>

<style>
	.board {
		display: flex;
		flex-wrap: wrap;
		align-items: stretch;
		justify-content: center;
		gap: 8px;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.side {
		display: flex;
		flex-direction: column;
		align-items: center;
		flex: 1 1 0;
		min-width: 72px;
		padding: 6px 10px;
		border: 1px solid transparent;
		border-radius: var(--radius-sm);
	}

	/* The active side is whose turn it is, so the board doubles as the turn
	   indicator rather than needing a second one. */
	.side.active {
		border-color: var(--accent);
		background: var(--accent-soft);
	}

	.side.out {
		opacity: 0.55;
	}

	.who {
		display: flex;
		align-items: baseline;
		gap: 4px;
		max-width: 100%;
		overflow: hidden;
		color: var(--text-muted);
		font-size: 0.8rem;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.side.me .who {
		color: var(--text);
		font-weight: 600;
	}

	.away {
		color: var(--danger);
	}

	.score {
		font-size: 1.4rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}

	.side.out .score {
		text-decoration: line-through;
	}

	.series {
		color: var(--text-muted);
		font-size: 0.7rem;
		font-variant-numeric: tabular-nums;
		white-space: nowrap;
	}

	.badge {
		padding: 0 7px;
		border-radius: 999px;
		background: var(--surface-alt);
		color: var(--text-muted);
		font-size: 0.7rem;
	}

	.badge.win {
		background: var(--accent-soft);
		color: var(--accent);
		font-weight: 700;
	}
</style>
