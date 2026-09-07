<script>
	import { t } from '$lib/i18n/vi.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { Status, connection } from '$lib/ws/connection.svelte.js';

	/** @type {{ onsubmit: (word: string) => boolean }} */
	let { onsubmit } = $props();

	/** @type {HTMLInputElement | undefined} */
	let field = $state();
	let composing = $state(false);

	// The connection is part of the condition, not just the turn. A word typed
	// during a reconnect would be dropped by a closed socket and the player
	// would watch their turn expire with no idea why.
	const enabled = $derived(
		game.state.phase === 'playing' && game.state.myTurn && connection.status === Status.OPEN
	);

	// What the field was last seeded for. Plain lets, not state: they guard the
	// effect below and must not re-trigger it. The rejection is part of the key
	// because a refused word empties the field without ending the turn, and the
	// player deserves the seed back before they retype.
	let seededTurn = -1;
	/** @type {unknown} */
	let seededRejection = null;

	// Focus when the turn arrives, so a player on a phone can type without
	// reaching for the field, and seed it with the syllable the word has to
	// start with — that part of the answer is already decided, and typing it
	// again is the one keystroke sequence every single turn shares.
	//
	// Reading myTurn is what subscribes the effect.
	$effect(() => {
		if (!(game.state.myTurn && game.state.phase === 'playing')) return;
		const turn = game.state.turnSeq;
		const syllable = game.state.currentSyllable;
		const rejection = game.state.rejection;

		field?.focus();
		if (!field || (turn === seededTurn && rejection === seededRejection)) return;
		seededTurn = turn;
		seededRejection = rejection;
		// Never over an existing draft: a rejected word is still the player's
		// text, and replacing it would delete a word they were about to fix.
		if (field.value || !syllable) return;

		field.value = `${syllable} `;
		// Caret after the seed, so typing continues the word instead of
		// landing in front of it.
		field.setSelectionRange(field.value.length, field.value.length);
	});

	/** @param {SubmitEvent} event */
	function handleSubmit(event) {
		event.preventDefault();
		// Enter can commit an IME candidate rather than the form. Submitting
		// mid-composition would send a half-typed word and swallow the
		// keystroke that was choosing the accent.
		if (composing || !enabled || !field) return;

		const word = field.value.trim();
		if (!word) return;

		// Only clear a word that actually went out. Clearing regardless would
		// erase what the player typed and leave nothing to resend.
		if (!onsubmit(word)) return;
		// Safe here and only here: composition has ended by the time a submit
		// is delivered, so clearing cannot destroy an accent being formed.
		field.value = '';
	}
</script>

<form class="input-row" onsubmit={handleSubmit}>
	<!--
		Deliberately uncontrolled. Vietnamese diacritics are typed over several
		keystrokes by a Telex or VNI input method, and writing the value back on
		every keystroke cancels that composition and mangles the accent. The
		server normalizes the text anyway, so the client has no reason to touch
		it.
	-->
	<input
		bind:this={field}
		type="text"
		name="word"
		autocomplete="off"
		autocapitalize="off"
		autocorrect="off"
		spellcheck="false"
		enterkeyhint="send"
		disabled={!enabled}
		placeholder={t.wordInputPlaceholder}
		aria-label={t.wordInputPlaceholder}
		oncompositionstart={() => (composing = true)}
		oncompositionend={() => (composing = false)}
	/>
	<button type="submit" disabled={!enabled}>{t.submit}</button>
</form>

{#if game.state.rejection}
	<p class="rejection" role="alert">{game.state.rejection.message}</p>
{/if}

<style>
	.input-row {
		display: flex;
		gap: 8px;
	}

	input {
		flex: 1;
		min-width: 0;
		padding: 14px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		/* 16px or larger stops iOS Safari zooming the page on focus, which on a
		   phone hides half the board behind the keyboard. */
		font-size: 1rem;
	}

	input:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}

	input:disabled {
		background: var(--surface-alt);
		color: var(--text-muted);
	}

	button {
		padding: 14px 20px;
		border: 0;
		border-radius: var(--radius-sm);
		background: var(--accent);
		color: var(--accent-text);
		font-weight: 600;
	}

	button:disabled {
		background: var(--surface-alt);
		color: var(--text-muted);
	}

	.rejection {
		margin: 8px 0 0;
		padding: 10px 12px;
		border-radius: var(--radius-sm);
		background: var(--danger-soft);
		color: var(--danger);
		font-size: 0.9rem;
	}
</style>
