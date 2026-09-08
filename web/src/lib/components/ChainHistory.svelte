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
		<ol class="rows" bind:this={list}>
			{#each entries as entry, index}
				<!-- The panel id comes from the row's place in the chain, not the
				     word, so two rows can never share one. -->
				{@const open = game.isExpanded(entry.word)}
				{@const panelId = `meaning-${entries.length - 1 - index}`}
				<li class:mine={entry.byMe} class:opening={entry.opening} class:latest={index === 0}>
					<!-- Every word is a button, with or without a definition, so the
					     chain behaves the same for all of them. Never focused from
					     here: the word input keeps focus while a player types. -->
					<button
						class="word"
						type="button"
						aria-expanded={open}
						aria-controls={open ? panelId : undefined}
						aria-label={fill(open ? t.meaningHide : t.meaningShow, { word: entry.word })}
						onclick={() => game.toggleMeaning(entry.word)}>{entry.word}</button
					>
					<!-- Who played it, not merely whether it was mine: a chain
					     four people built is unreadable without the names. -->
					{#if !entry.opening && !entry.byMe && game.nameOf(entry.playerId)}
						<span class="by">{game.nameOf(entry.playerId)}</span>
					{/if}
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
					{#if open}
						<!-- Plain text from the server, rendered as text: the builder
						     stripped the wiki markup and nothing here re-interprets it. -->
						{#if entry.meanings.length}
							<ol class="meanings" id={panelId}>
								{#each entry.meanings as sense}
									<li>{sense.pos ? `(${sense.pos}) ` : ''}{sense.gloss}</li>
								{/each}
							</ol>
						{:else}
							<p class="meanings none" id={panelId}>{t.meaningNone}</p>
						{/if}
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

	/* Row rules are scoped to the outer list: the meanings list nested in a
	   row is an <ol> of <li> too and must not inherit the card styling. */
	.rows {
		display: flex;
		flex-direction: column;
		gap: 6px;
		margin: 0;
		padding: 0;
		overflow-y: auto;
		list-style: none;
	}

	.rows > li {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 8px;
		padding: 8px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
	}

	.rows > li.mine {
		border-color: var(--accent);
		background: var(--accent-soft);
	}

	.rows > li.opening {
		border-style: dashed;
		background: var(--surface-alt);
	}

	.rows > li.latest {
		box-shadow: var(--shadow);
	}

	.word {
		padding: 0;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		font-size: 1.05rem;
		font-weight: 600;
		text-align: left;
		cursor: pointer;
	}

	.word:hover {
		text-decoration: underline;
	}

	.word:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
		border-radius: 2px;
	}

	.meanings {
		flex-basis: 100%;
		margin: 2px 0 0;
		padding-left: 1.4em;
		color: var(--text-muted);
		font-size: 0.85rem;
		line-height: 1.4;
	}

	ol.meanings {
		list-style: decimal;
	}

	.meanings.none {
		padding-left: 0;
		font-style: italic;
	}

	.meta {
		display: inline-flex;
		gap: 8px;
		margin-left: auto;
		font-size: 0.8rem;
	}

	.by {
		color: var(--text-muted);
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
