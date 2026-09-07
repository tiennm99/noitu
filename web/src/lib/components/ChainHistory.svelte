<script>
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/** @type {HTMLElement | undefined} */
	let list = $state();

	// Newest first: the move that decides what to play next is the last one, so
	// it belongs where the eye lands rather than at the end of a list the
	// player has to scroll through.
	const entries = $derived([...game.state.chain].reverse());

	// Keep the newest word in view as the chain grows. Reading chain.length in
	// the effect is what subscribes it to new moves.
	$effect(() => {
		game.state.chain.length;
		list?.scrollTo({ top: 0, behavior: 'smooth' });
	});
</script>

<section class="chain" aria-label={t.chainTitle}>
	<h2>{t.chainTitle}</h2>
	{#if game.state.chain.length === 0}
		<p class="empty">{t.chainEmpty}</p>
	{:else}
		<ol bind:this={list}>
			{#each entries as entry, index}
				<li class:mine={entry.byMe} class:opening={entry.opening} class:latest={index === 0}>
					<span class="word">{entry.word}</span>
					<span class="meta">
						{#if entry.syllables > 2}
							<span class="badge">{entry.syllables} {t.syllableUnit}</span>
						{/if}
						{#if entry.points > 0}
							<span class="points">+{entry.points}</span>
						{/if}
					</span>
					{#if entry.byMe && entry.typed && entry.typed !== entry.word}
						<!-- The server accepted a different spelling from the one typed.
						     Saying so beats silently rewriting the player's word. -->
						<span class="corrected">
							{fill(t.correctedFrom, { typed: entry.typed, word: entry.word })}
						</span>
					{/if}
				</li>
			{/each}
		</ol>
	{/if}
</section>

<style>
	.chain {
		display: flex;
		flex-direction: column;
		min-height: 0;
	}

	h2 {
		margin: 0 0 8px;
		color: var(--text-muted);
		font-size: 0.85rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}

	.empty {
		margin: 0;
		color: var(--text-muted);
	}

	ol {
		display: flex;
		flex-direction: column;
		gap: 6px;
		margin: 0;
		padding: 0;
		overflow-y: auto;
		list-style: none;
	}

	li {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 8px;
		padding: 8px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
	}

	li.mine {
		border-color: var(--accent);
		background: var(--accent-soft);
	}

	li.opening {
		border-style: dashed;
		background: var(--surface-alt);
	}

	li.latest {
		box-shadow: var(--shadow);
	}

	.word {
		font-size: 1.05rem;
		font-weight: 600;
	}

	.meta {
		display: inline-flex;
		gap: 8px;
		margin-left: auto;
		font-size: 0.8rem;
	}

	.badge {
		padding: 1px 7px;
		border-radius: 999px;
		background: var(--surface-alt);
		color: var(--text-muted);
	}

	.points {
		color: var(--accent);
		font-weight: 600;
		font-variant-numeric: tabular-nums;
	}

	.corrected {
		flex-basis: 100%;
		color: var(--text-muted);
		font-size: 0.78rem;
	}
</style>
