<script>
	import { chainToText, downloadText, historyFilename } from '$lib/history-export.js';
	import { endReasonMessages, fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/**
	 * onrematch is optional because the two modes differ: a bot always plays
	 * again, so the button starts the next game, while online play agrees the
	 * next one in the lobby this panel appears above.
	 *
	 * @type {{ isRecord: boolean, onrematch?: () => void, onhome: () => void }}
	 */
	let { isRecord, onrematch, onhome } = $props();

	/** @type {HTMLElement | undefined} */
	let panel = $state();

	const result = $derived(game.state.result);
	const standings = $derived(game.state.standings);
	// What the position still had when this player lost it. It arrives with
	// their knockout rather than with the result, because by the time a game
	// with four people in it ends, the position that beat them is nobody
	// else's position.
	const elimination = $derived(game.state.elimination);

	// The word field unmounts when the game ends, which drops focus to the top
	// of the document: a keyboard player tabs through the header and the badge
	// to reach "Chơi lại", and a screen reader is told nothing at all, because
	// role="group" is not announced on insertion.
	//
	// The panel takes focus, not the rematch button. A player who just pressed
	// Enter to submit a word may still be holding it, and a focused button
	// under that key would start the next game before they had read this one.
	$effect(() => {
		if (game.state.result) panel?.focus();
	});

	/** Hands the finished chain to the player as a text file to keep. */
	function exportHistory() {
		const at = new Date();
		const text = chainToText({
			chain: game.state.chain,
			result,
			nameOf: (id) => game.nameOf(id),
			at
		});
		downloadText(historyFilename(at), text);
	}
</script>

{#if result}
	<div
		class="panel"
		bind:this={panel}
		role="group"
		tabindex="-1"
		aria-label={result.iWon ? t.won : t.lost}
	>
		<h2 class:won={result.iWon} aria-live="polite">{result.iWon ? t.won : t.lost}</h2>

		{#if endReasonMessages[result.reason]}
			<p class="reason">{endReasonMessages[result.reason]}</p>
		{/if}

		{#if standings.length > 1}
			<!-- Ranked by who outlasted whom, which is what the game is decided
			     on. The score sits beside the place rather than setting it.
			     Shown for two players as well now that it is the only table on
			     the screen: the board's own stops at the final whistle. -->
			<ol class="standings" aria-label={t.standingsTitle} data-testid="standings">
				{#each standings as player (player.playerId)}
					<li class:me={player.isMe} class:winner={player.rank === 1}>
						<span class="rank">{player.rank}</span>
						<span class="name">
							{player.isMe ? game.state.nickname || t.you : player.name || t.someone}
						</span>
						<span class="points">{player.score} {t.pointsUnit}</span>
						{#if player.rank === 1}<span class="trophy" aria-label={t.winnerBadge}>🏆</span>{/if}
					</li>
				{/each}
			</ol>
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

		{#if elimination}
			<!-- Only the player who was stuck is sent this, and only they were
			     stuck. Losing without ever learning what the position wanted is
			     the part that stings; an empty list says the position had
			     nothing, which is worth hearing too. -->
			{#if elimination.suggestions.length > 0}
				<div class="suggestions">
					<h3>{t.suggestionsTitle}:</h3>
					<ul>
						{#each elimination.suggestions as word}
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

		<!-- One row, wrapping when the words no longer fit it. Stacked, the
		     three of them were 150px of buttons, which on a 1080p screen was
		     what pushed the lobby's own — the ones that start the next game —
		     off the bottom. -->
		<div class="actions">
			{#if onrematch}
				<button type="button" class="primary" onclick={onrematch}>{t.rematch}</button>
			{/if}
			<button type="button" onclick={onhome}>{t.home}</button>
			<button type="button" class="export" onclick={exportHistory}>{t.exportHistory}</button>
		</div>
	</div>
{/if}

<style>
	.panel {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-4);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--surface);
		box-shadow: var(--shadow);
		text-align: center;
	}

	h2 {
		margin: 0;
		color: var(--danger);
		font-size: var(--text-7);
	}

	h2.won {
		color: var(--accent);
	}

	.reason {
		margin: 0;
		color: var(--text-muted);
	}

	.standings {
		display: flex;
		flex-direction: column;
		gap: 4px;
		margin: 0;
		padding: 0;
		list-style: none;
		text-align: left;
	}

	.standings li {
		display: flex;
		align-items: baseline;
		gap: 10px;
		padding: var(--space-1) var(--space-3);
		font-size: var(--text-5);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface-alt);
	}

	.standings li.me {
		border-color: var(--accent);
	}

	.standings li.winner {
		background: var(--accent-soft);
	}

	.rank {
		color: var(--text-muted);
		font-variant-numeric: tabular-nums;
	}

	.standings .name {
		overflow: hidden;
		font-weight: 600;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.points {
		margin-left: auto;
		color: var(--text-muted);
		font-size: var(--text-4);
		font-variant-numeric: tabular-nums;
	}

	/* One line rather than two stacked tiles. The score is in the table above
	   this, so the tiles were 40px of repeating it — and 40px is the
	   difference between the lobby's buttons being on a 1080p screen and
	   under it. */
	.stats {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: var(--space-2) var(--space-5);
		margin: 0;
	}

	.stats div {
		display: flex;
		align-items: baseline;
		gap: var(--space-2);
	}

	dt {
		color: var(--text-muted);
		font-size: var(--text-4);
	}

	dd {
		margin: 0;
		font-size: var(--text-5);
		font-weight: 700;
		font-variant-numeric: tabular-nums;
	}

	/* Label and words on one line, wrapping when they run out of it: a heading
	   of its own cost a whole row of a screen the lobby's buttons are also on. */
	.suggestions {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		justify-content: center;
		gap: 6px var(--space-2);
	}

	.suggestions h3 {
		margin: 0;
		color: var(--text-muted);
		font-size: var(--text-3);
		font-weight: 600;
		/* Uppercase Vietnamese stacks a tone mark above a capital. */
		line-height: 1.6;
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
		padding: var(--space-1) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-pill);
		background: var(--surface-alt);
		font-weight: 600;
	}

	.dead-end {
		margin: 0;
		color: var(--text-muted);
	}

	.record {
		margin: 0;
		padding: 6px var(--space-3);
		border-radius: var(--radius-pill);
		background: var(--accent-soft);
		color: var(--accent);
		font-weight: 700;
	}

	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}

	/* Wrapping rather than shrinking: three of these across a phone would
	   break "Tải chuỗi từ" over two lines, so the row gives way instead. */
	.actions button {
		flex: 1 1 auto;
		min-width: 120px;
		min-height: 44px;
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface);
		font-weight: 600;
	}

	.actions .primary {
		border-color: transparent;
		background: var(--accent);
		color: var(--accent-text);
	}

	/* Keeping the chain is worth offering and not worth pressing first. */
	.actions .export {
		background: transparent;
		color: var(--text-muted);
		font-size: var(--text-5);
	}
</style>
