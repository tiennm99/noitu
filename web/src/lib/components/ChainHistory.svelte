<script>
	import { fill, t } from '$lib/i18n/vi.js';
	import { scrollBehavior } from '$lib/motion.js';
	import { game } from '$lib/stores/game.svelte.js';

	/** @type {HTMLElement | undefined} */
	let list = $state();

	// Newest first: the move that decides what to play next is the last one, so
	// it belongs where the eye lands rather than at the end of a list the
	// player has to scroll through.
	const entries = $derived([...game.state.chain].reverse());

	/** How far from the newest row still counts as watching the newest row. */
	const FOLLOW_PX = 48;

	// Keep the newest word in view as the chain grows. Reading chain.length in
	// the effect is what subscribes it to new moves.
	//
	// Only for a reader who is already at the top, though: the newest word is
	// row one, so anybody scrolled past it is reading an older definition, and
	// yanking them back every time somebody moves is worse than making them
	// scroll.
	$effect(() => {
		game.state.chain.length;
		if (!list || list.scrollTop > FOLLOW_PX) return;
		list.scrollTo({ top: 0, behavior: scrollBehavior() });
	});

	/**
	 * Whether there is chain below the fold. The list is given whatever height
	 * the board has left, which is never a whole number of rows, so the last
	 * one is part in and part out of view — and a card sliced by the bottom
	 * edge reads as broken rather than as scrollable. It is faded out instead,
	 * but only while there is really something under it: a fade at the true
	 * end of the chain would be saying the same thing about nothing.
	 */
	let more = $state(false);

	function measure() {
		if (!list) return;
		more = list.scrollHeight - list.scrollTop - list.clientHeight > 4;
	}

	// Re-measured on the three things that change the answer: a new word, the
	// reader scrolling, and the box being resized — which is what an opening
	// keyboard, a rotation and a meaning panel unfolding all are.
	$effect(() => {
		game.state.chain.length;
		game.state.expanded.length;
		if (!list) return;
		measure();
		const observer = new ResizeObserver(measure);
		observer.observe(list);
		return () => observer.disconnect();
	});
</script>

<section class="chain" aria-label={t.chainTitle}>
	<h2>{t.chainTitle}</h2>
	{#if game.state.chain.length === 0}
		<p class="empty">{t.chainEmpty}</p>
	{:else}
		<!-- role, because list-style: none takes the list semantics away in
		     Safari with VoiceOver, and "3 trong 24" is most of what the chain
		     tells somebody listening to it. -->
		<ol class="rows" class:more role="list" bind:this={list} onscroll={measure}>
			{#each entries as entry, index}
				<!-- The panel id comes from the row's place in the chain, not the
				     word, so two rows can never share one. -->
				{@const open = game.isExpanded(entry.word)}
				{@const panelId = `meaning-${entries.length - 1 - index}`}
				<li class:mine={entry.byMe} class:opening={entry.opening} class:latest={index === 0}>
					<!-- The whole row is the button, with or without a definition, so the
					     chain behaves the same for all of them and a player can hit it
					     anywhere on the card instead of aiming at the word. It holds text
					     only: the panel it opens cannot live inside a button. Never
					     focused from here: the word input keeps focus while a player types. -->
					<button
						class="row"
						type="button"
						aria-expanded={open}
						aria-controls={open ? panelId : undefined}
						aria-label={fill(open ? t.meaningHide : t.meaningShow, { word: entry.word })}
						onclick={() => game.toggleMeaning(entry.word)}
					>
						<span class="word">{entry.word}</span>
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
					</button>
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
	/* Takes whatever height the board has left so the list scrolls inside
	   itself; the syllable, the input and the buttons above it stay put
	   however long the chain gets. */
	.chain {
		display: flex;
		flex: 1;
		flex-direction: column;
		min-height: 0;
	}

	h2 {
		margin: 0 0 var(--space-2);
		color: var(--text-muted);
		font-size: var(--text-4);
		font-weight: 600;
		/* Uppercase Vietnamese stacks a tone mark above a capital, which the
		   inherited 1.5 only just clears. */
		line-height: 1.6;
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
		/* overflow-y sets a flex item's automatic minimum size to 0, and the
		   shell is pinned at exactly 100dvh — so on a short viewport, which is
		   what an open keyboard or a finished game's panel leaves, the list
		   would shrink to nothing with no page scroll to recover it. A floor
		   makes the page grow instead.
		
		   Three rows of it: at a row and a half the chain was a sliver you
		   could not read the history in, and the whole point of it is reading
		   back what has been played. Anything above this is space the board
		   had going spare. */
		min-height: 9rem;
		overflow-y: auto;
		list-style: none;
		/* Rows settle whole rather than half in and half out of view: the
		   list is a transcript to read back, and a card sliced by the bottom
		   edge reads as broken rather than as scrollable. Proximity, not
		   mandatory — a row with its meaning open can be taller than the
		   window that holds it. */
		scroll-snap-type: y proximity;
		/* The list keeps its own scrolling to itself rather than carrying on
		   into the page behind it once it reaches the end. */
		overscroll-behavior: contain;
	}

	.rows > li {
		scroll-snap-align: start;
	}

	/* Set while there is chain under the bottom edge: the row that is half in
	   view fades out instead of being cut off square, which is the only thing
	   on a phone that says this list scrolls. */
	.rows.more {
		mask-image: linear-gradient(to bottom, #000 calc(100% - 28px), transparent);
	}

	.rows > li {
		display: flex;
		flex-direction: column;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
	}

	/* The padding belongs to the button rather than the card so the whole
	   closed card, its edges included, is inside the click target. */
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 8px;
		min-height: 44px;
		padding: 8px 12px;
		border: 0;
		border-radius: inherit;
		background: none;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}

	.row:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}

	.row:hover .word {
		text-decoration: underline;
	}

	.rows > li.mine {
		border-color: var(--accent);
		background: var(--accent-soft);
	}

	.rows > li.opening {
		border-style: dashed;
		background: var(--surface-alt);
	}

	/* A structural cue rather than only a shadow: --shadow is black at 30-40%
	   over a dark surface, which is nothing at all, and in a two-player game
	   the alternating .mine tint was left doing the marking instead. */
	.rows > li.latest {
		border-inline-start: 3px solid var(--accent);
		box-shadow: var(--shadow);
	}

	.word {
		font-size: var(--text-6);
		font-weight: 600;
	}

	.meanings {
		margin: 0 0 8px;
		padding-left: calc(12px + 1.4em);
		padding-right: 12px;
		color: var(--text-muted);
		font-size: var(--text-4);
		line-height: 1.4;
	}

	ol.meanings {
		list-style: decimal;
	}

	.meanings.none {
		padding-left: 12px;
		font-style: italic;
	}

	.meta {
		display: inline-flex;
		gap: 8px;
		margin-left: auto;
		font-size: var(--text-3);
	}

	.by {
		color: var(--text-muted);
		font-size: var(--text-3);
	}

	.badge {
		padding: 1px var(--space-2);
		border-radius: var(--radius-pill);
		background: var(--surface-alt);
		color: var(--text-muted);
	}

	.points {
		color: var(--accent);
		font-weight: 600;
		font-variant-numeric: tabular-nums;
	}

	.corrected {
		padding: 0 12px 8px;
		color: var(--text-muted);
		font-size: var(--text-3);
	}
</style>
