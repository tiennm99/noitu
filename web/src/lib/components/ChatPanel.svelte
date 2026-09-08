<script>
	import { fill, t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';

	/**
	 * The room's conversation. Everything it shows comes from the server: the
	 * client never appends its own copy of a message, so there is one ordering
	 * rather than a guessed one.
	 *
	 * Three facts about where it is, because each is a different question and
	 * the caller is the only one that can answer them:
	 *
	 * - `collapsible` folds the panel behind an unread count. That is for the
	 *   board on a narrow screen, where the game and the chat share one column
	 *   and an open log would crowd the board off it.
	 * - `column` says the panel has a column of its own, so the log grows into
	 *   the height it is given instead of stopping at a phone's worth.
	 * - `errors` shows the room's refusals here. The lobby renders none of its
	 *   own, so this is where they land; the board has its own alert, and two
	 *   boxes for one error is worse than none.
	 *
	 * @type {{
	 *   collapsible?: boolean,
	 *   column?: boolean,
	 *   errors?: boolean,
	 *   onsend: (text: string) => void
	 * }}
	 */
	let { collapsible = false, column = false, errors = false, onsend } = $props();

	/** @type {HTMLElement | undefined} */
	let list = $state();
	/** @type {HTMLInputElement | undefined} */
	let field = $state();

	let draft = $state('');
	let composing = $state(false);
	// Folded is the toggle's own state; open is what the panel is, which a
	// panel that cannot fold always is.
	let folded = $state(true);
	const open = $derived(!collapsible || !folded);

	// Where this reader had got to, counted against the store's running total
	// rather than the list length: the list is capped, so past twenty messages
	// a length-based count stops rising and the badge dies. Seeded at the
	// declaration so a panel appearing mid-conversation — the board's, after a
	// lobby full of chat — does not open claiming all of it is new.
	let seenAt = $state(game.state.chatCount);

	const messages = $derived(game.state.chat);
	const unread = $derived(open ? 0 : Math.max(0, game.state.chatCount - seenAt));

	// What the server would be left with after sanitizing: whitespace gone and
	// the invisible characters that survive a trim stripped out. Matching the
	// server's own emptiness test is what keeps a blank message from being
	// sent and silently dropped with nothing on screen to explain it.
	//
	/** @param {string} text */
	function hasContent(text) {
		return text.replace(/[\p{Cf}\p{Cc}]/gu, '').trim().length > 0;
	}

	const sendable = $derived(hasContent(draft));

	// Everything is read while the panel is open. A count that went backwards
	// is a resynchronised panel — a reconnect, or an opponent leaving — and
	// whatever it now holds is all there is to have missed.
	$effect(() => {
		const count = game.state.chatCount;
		if (open || count < seenAt) seenAt = count;
	});

	// Keep the newest line in view, but only for a reader who is already at the
	// bottom: yanking somebody out of scrollback to show them a new message is
	// worse than making them scroll for it.
	$effect(() => {
		messages.length;
		if (!open || !list) return;
		const room = list.scrollHeight - list.scrollTop - list.clientHeight;
		if (room < 80) list.scrollTo({ top: list.scrollHeight, behavior: 'smooth' });
	});

	/**
	 * The colour a seat writes in, from the palette in app.css. A line whose
	 * seat the server cleared has no colour of its own: it belongs to nobody,
	 * and painting it as somebody would be a lie about who said it.
	 *
	 * @param {string} playerId
	 * @returns {string}
	 */
	function colourOf(playerId) {
		const seat = game.seatIndexOf(playerId);
		return seat ? `var(--player-${seat})` : 'var(--text-muted)';
	}

	/** @param {number} atMs */
	function clock(atMs) {
		const at = new Date(atMs);
		return `${String(at.getHours()).padStart(2, '0')}:${String(at.getMinutes()).padStart(2, '0')}`;
	}

	/** @param {SubmitEvent} event */
	function submit(event) {
		event.preventDefault();
		// Enter can commit an accent rather than the form. Sending here would
		// post a half-typed word and swallow the keystroke that was choosing
		// the diacritic.
		if (composing || !sendable || !field) return;

		// Re-tested against the element rather than the mirror: a form value the
		// browser restored without firing `input` would otherwise be sent
		// unchecked, or send an empty message the server drops in silence.
		const text = field.value;
		if (!hasContent(text)) return;

		onsend(text);
		draft = '';
		field.value = '';
	}

	function toggle() {
		folded = !folded;
	}
</script>

<section class="chat" aria-label={t.chatTitle}>
	{#if collapsible}
		<button type="button" class="header" aria-expanded={open} onclick={toggle}>
			<span>{t.chatTitle}</span>
			{#if unread > 0}
				<span class="badge" data-testid="chat-unread">{fill(t.chatUnread, { n: unread })}</span>
			{/if}
		</button>
	{:else}
		<h2>{t.chatTitle}</h2>
	{/if}

	{#if open}
		{#if messages.length === 0}
			<p class="empty">{t.chatEmpty}</p>
		{:else}
			<!-- A log, not a stack of bubbles: every line reads "name: text" in
			     its author's colour, so four people talking stay tellable apart
			     without a shape per speaker. -->
			<ol bind:this={list} class:column data-testid="chat-log">
				{#each messages as entry}
					<li style:color={colourOf(entry.playerId)}>
						<!-- An author the server cleared belongs to nobody: the seat
						     they spoke from may be somebody else's now. -->
						<span class="author">{entry.author || t.chatAuthorLeft}:</span>
						<!-- Interpolated, never {@html}: this is another player's text. -->
						<span class="text">{entry.text}</span>
						<span class="at">{clock(entry.atMs)}</span>
					</li>
				{/each}
			</ol>
		{/if}

		{#if errors && game.state.error}
			<!-- too_fast on a burst, must_unready_first, player_is_ready: the
			     lobby's refusals, which nothing else on that screen shows. -->
			<p class="error" role="alert" data-testid="chat-error">
				{game.state.error}
				<button type="button" onclick={() => game.clearError()} aria-label={t.dismiss}>×</button>
			</p>
		{/if}

		<form class="row" onsubmit={submit}>
			<!--
				Uncontrolled on purpose, exactly as the word field is: Vietnamese
				diacritics are composed over several keystrokes by a Telex or VNI
				input method, and writing a value back into the element cancels
				that composition and mangles the accent. `draft` mirrors the
				field one way only — it is never bound back.
			-->
			<input
				bind:this={field}
				type="text"
				name="chat"
				autocomplete="off"
				autocorrect="off"
				spellcheck="false"
				enterkeyhint="send"
				placeholder={t.chatPlaceholder}
				aria-label={t.chatTitle}
				data-testid="chat-input"
				oninput={(event) => (draft = event.currentTarget.value)}
				oncompositionstart={() => (composing = true)}
				oncompositionend={() => (composing = false)}
			/>
			<button type="submit" disabled={!sendable} data-testid="chat-send">{t.submit}</button>
		</form>
	{/if}
</section>

<style>
	.chat {
		display: flex;
		flex-direction: column;
		gap: 8px;
		min-height: 0;
	}

	h2,
	.header {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 0;
		padding: 0;
		border: 0;
		background: none;
		color: var(--text-muted);
		font-size: 0.85rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}

	.header {
		width: 100%;
		justify-content: space-between;
	}

	.badge {
		padding: 1px 8px;
		border-radius: 999px;
		background: var(--accent);
		color: var(--accent-text);
		font-size: 0.7rem;
		text-transform: none;
		letter-spacing: 0;
	}

	.empty {
		margin: 0;
		color: var(--text-muted);
	}

	ol {
		display: flex;
		flex-direction: column;
		gap: 4px;
		/* Stacked under the game, so bounded: the field it scrolls above has to
		   stay on screen. */
		max-height: 180px;
		margin: 0;
		padding: 0;
		overflow-y: auto;
		list-style: none;
	}

	/* Given a column of its own, the log takes the height of it. */
	ol.column {
		flex: 1;
		min-height: 0;
		max-height: none;
	}

	li {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 6px;
		/* Colour is set per line, from the author's seat. Everything else about
		   a line is the same for everybody. */
		font-size: 0.95rem;
	}

	.author {
		font-weight: 700;
	}

	.text {
		flex: 1;
		min-width: 0;
		/* A single unbroken run of characters must wrap rather than widen the
		   page: the sender chooses this text. */
		overflow-wrap: anywhere;
	}

	.at {
		color: var(--text-muted);
		font-size: 0.7rem;
		font-variant-numeric: tabular-nums;
	}

	.error {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		margin: 0;
		padding: 8px 10px;
		border-radius: var(--radius-sm);
		background: var(--danger-soft);
		color: var(--danger);
		font-size: 0.85rem;
	}

	.error button {
		border: 0;
		background: none;
		color: inherit;
		font-size: 1.1rem;
		line-height: 1;
	}

	.row {
		display: flex;
		gap: 8px;
	}

	input {
		flex: 1;
		min-width: 0;
		padding: 10px 12px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		/* 16px or larger stops iOS Safari zooming the page on focus. */
		font-size: 1rem;
	}

	input:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}

	.row button {
		padding: 10px 16px;
		border: 0;
		border-radius: var(--radius-sm);
		background: var(--accent);
		color: var(--accent-text);
		font-weight: 600;
	}

	.row button:disabled {
		background: var(--surface-alt);
		color: var(--text-muted);
	}
</style>
