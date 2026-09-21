<script>
	import { fill, t } from '$lib/i18n/vi.js';
	import { RejectReason } from '$lib/proto/noitu/v1/game_pb.js';
	import { game } from '$lib/stores/game.svelte.js';
	import { Status, connection } from '$lib/ws/connection.svelte.js';

	/**
	 * @type {{
	 *   onsubmit: (word: string) => boolean,
	 *   onreportword: (word: string) => void
	 * }}
	 */
	let { onsubmit, onreportword } = $props();

	/** @type {HTMLInputElement | undefined} */
	let field = $state();
	let composing = $state(false);

	// The connection is part of the condition, not just the turn. A word typed
	// during a reconnect would be dropped by a closed socket and the player
	// would watch their turn expire with no idea why.
	const enabled = $derived(
		game.state.phase === 'playing' && game.state.myTurn && connection.status === Status.OPEN
	);

	// Why the field cannot be sent from, in its own placeholder: an inviting
	// "Nhập từ của bạn" over a dead input is how a player ends up typing into
	// one. Kept short — the board's turn line above says who is thinking, and
	// a sentence here is cut off by the send button beside it.
	const waitingFor = $derived.by(() => {
		if (enabled) return t.wordInputPlaceholder;
		if (connection.status !== Status.OPEN) return t.wordInputOffline;
		return t.wordInputWaiting;
	});

	// What the field was last seeded for. Plain lets, not state: they guard the
	// effect below and must not re-trigger it. The rejection is part of the key
	// because a refused word empties the field without ending the turn, and the
	// player deserves the seed back before they retype.
	let seededTurn = -1;
	/** @type {unknown} */
	let seededRejection = null;

	/**
	 * Whether the player is typing somewhere else — the chat, in practice,
	 * which now sits beside the board rather than folded away under it.
	 *
	 * Taking focus off a field somebody is mid-sentence in would drop the rest
	 * of that sentence into the word field, so a turn arriving is allowed to
	 * ask for focus only when nothing else holds it.
	 */
	function typingElsewhere() {
		const active = document.activeElement;
		if (!active || active === field) return false;
		return (
			active instanceof HTMLElement &&
			(active.tagName === 'INPUT' || active.tagName === 'TEXTAREA' || active.isContentEditable)
		);
	}

	/**
	 * What the field held when the turn passed, so anything typed into it out
	 * of turn can be put back. A plain let: nothing renders it, and making it
	 * state would re-run the effects below on every keystroke it absorbs.
	 */
	let lockedValue = '';

	// Captured on the way out of the turn, not on every keystroke: reading the
	// field here is what makes `enabled` the only thing that moves it.
	$effect(() => {
		if (enabled || !field) return;
		lockedValue = field.value;
	});

	/**
	 * Out of turn the field takes no text. It stays focusable and focused —
	 * that is what keeps the on-screen keyboard alive between turns, and is
	 * why this is not `disabled` or `readonly` — but a keystroke, a paste and
	 * a drop all do nothing, so nobody spends their opponent's turn typing a
	 * word that was never going to be sent.
	 * @param {Event} event
	 */
	function guardInput(event) {
		if (!enabled) event.preventDefault();
	}

	/**
	 * The fallback for text the guard cannot refuse: `beforeinput` is not
	 * cancelable for a composition, which is how every Vietnamese input method
	 * types. The composed character lands and is taken straight back out.
	 */
	function undoInput() {
		if (enabled || !field || field.value === lockedValue) return;
		field.value = lockedValue;
	}

	// Focus when the turn arrives, so a player on a phone can type without
	// reaching for the field, and seed it with the syllable the word has to
	// start with — that part of the answer is already decided, and typing it
	// again is the one keystroke sequence every single turn shares.
	//
	// Gated on `enabled` rather than just the turn and the phase: seeding
	// during a reconnect wrote into a field the player could not submit from,
	// and the first composition event then undid the seed anyway (undoInput
	// yanks back anything typed while offline), making it non-deterministic
	// exactly when the player is anxious about a running clock.
	$effect(() => {
		if (!enabled) return;
		const turn = game.state.turnSeq;
		const syllable = game.state.currentSyllable;
		const rejection = game.state.rejection;

		// The seeding below happens either way: it writes into the field
		// without disturbing wherever the player actually is.
		if (!typingElsewhere()) field?.focus();
		if (!field || (turn === seededTurn && rejection === seededRejection)) return;
		seededTurn = turn;
		seededRejection = rejection;
		if (!syllable) return;

		// The field keeps whatever it held, so a draft the player never sent may
		// now be aimed at a syllable the game has moved past. One that still
		// starts with what is being asked for is the player's word and is left
		// alone; one that does not is worse than no draft at all.
		const draft = field.value.trim();
		if (draft && draft.toLowerCase().startsWith(syllable.toLowerCase())) return;

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
		// Still inside the gesture that submitted, which is the only context an
		// on-screen keyboard will reopen for. Tapping "Gửi" moves focus to the
		// button, and a focus() from the effect above arrives on a WebSocket
		// message instead — iOS ignores that one and the player pays a dead tap
		// every turn to get the keyboard back.
		field.focus();
	}

	/**
	 * Fills the field with the server's suggestion and focuses it. A direct
	 * write, unlike everywhere else in this field: it happens only from this
	 * click, a gesture the player just made rather than one mid-keystroke, so
	 * no IME composition can be in progress for it to cancel.
	 */
	function useSuggestion() {
		const suggestion = game.state.rejection?.suggestion;
		if (!suggestion || !field) return;
		field.value = suggestion;
		field.focus();
		field.setSelectionRange(field.value.length, field.value.length);
	}

	function report() {
		const word = game.state.rejection?.word;
		if (word) onreportword(word);
	}
</script>

<form class="input-row" onsubmit={handleSubmit}>
	<!--
		Deliberately uncontrolled. Vietnamese diacritics are typed over several
		keystrokes by a Telex or VNI input method, and writing the value back on
		every keystroke cancels that composition and mangles the accent. The
		server normalizes the text anyway, so the client has no reason to touch
		it.

		Never `disabled`, and never `readonly` either: setting either on the
		focused field costs the on-screen keyboard — `disabled` blurs it
		outright — and nothing can reopen that without a tap, so a phone player
		pays a dead tap every turn. Out of turn the field refuses text instead,
		in guardInput, and the submit button and handleSubmit refuse the word.
		The accessible name stays put while the placeholder changes, so the
		field is still the same field to anybody listening.
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
		aria-disabled={!enabled}
		aria-invalid={!!game.state.rejection}
		aria-describedby={game.state.rejection ? 'word-rejection' : undefined}
		placeholder={waitingFor}
		aria-label={t.wordInputPlaceholder}
		oncompositionstart={() => (composing = true)}
		oncompositionend={() => (composing = false)}
		onbeforeinput={guardInput}
		oninput={undoInput}
	/>
	<!-- Named, because the chat's send button says the same word: both are a
	     "Gửi", and only a test can tell them apart by where they are. -->
	<button type="submit" disabled={!enabled} data-testid="word-submit">{t.submit}</button>
</form>

{#if game.state.rejection}
	<!-- The word is shown, not only the verdict. A Vietnamese rejection is
	     usually one tone mark out, and the field was cleared when the word went
	     out — so without this the player rebuilds it from memory, under the
	     clock, with nothing to compare against. -->
	<p class="rejection" id="word-rejection" role="alert">
		<strong>{game.state.rejection.word}</strong> — {game.state.rejection.message}
	</p>
	{#if game.state.rejection.suggestion || game.state.rejection.reason === RejectReason.NOT_IN_DICTIONARY}
		<!-- Their own line, at a real tap size: under the clock these are the
		     two most valuable taps on the board, and the pills they used to be
		     were under WCAG's 24px minimum. -->
		<div class="fixes">
			{#if game.state.rejection.suggestion}
				<!-- Corrects typing, not vocabulary: the server only ever offers
				     this for a word that differs from a real one by diacritics
				     alone — primary weight because it is almost certainly the
				     word the player meant. -->
				<button type="button" class="fix suggestion" onclick={useSuggestion}>
					{fill(t.suggestionPrompt, { word: game.state.rejection.suggestion })}
				</button>
			{/if}
			{#if game.state.rejection.reason === RejectReason.NOT_IN_DICTIONARY}
				<button type="button" class="fix report" onclick={report}>{t.reportWord}</button>
			{/if}
		</div>
	{/if}
{/if}

{#if game.state.reportConfirmation}
	<p class="report-confirmation" role="status">
		{game.state.reportConfirmation}
	</p>
{/if}

<style>
	.input-row {
		display: flex;
		gap: var(--space-2);
	}

	input {
		flex: 1;
		min-width: 0;
		padding: var(--space-4);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface);
		/* 16px or larger stops iOS Safari zooming the page on focus, which on a
		   phone hides half the board behind the keyboard. */
		font-size: var(--text-3);
		/* Whatever does not fit ends in an ellipsis rather than against the
		   edge of the box. */
		text-overflow: ellipsis;
	}

	/* Looks exactly as the disabled field used to. It is only the behaviour
	   that differs: focus, and therefore the keyboard, survives the turn — so
	   the caret has to go by hand, or a field that takes no text sits there
	   blinking as though it were waiting for some. */
	input[aria-disabled='true'] {
		caret-color: transparent;
		cursor: default;
		background: var(--surface-alt);
		color: var(--text-muted);
	}

	button {
		padding: var(--space-4) var(--space-5);
		border: 0;
		border-radius: var(--radius-sm);
		background: var(--accent);
		color: var(--accent-text);
		font-weight: 600;
		transition: background-color 150ms ease-out;
	}

	button:hover:not(:disabled) {
		background: var(--accent-hover);
	}

	button:active:not(:disabled) {
		background: var(--accent-pressed);
	}

	button:disabled {
		background: var(--surface-alt);
		color: var(--text-muted);
	}

	.rejection {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 4px var(--space-2);
		margin: var(--space-2) 0 0;
		padding: var(--space-3) var(--space-3);
		border-radius: var(--radius-sm);
		background: var(--danger-soft);
		color: var(--danger);
		font-size: var(--text-2);
	}

	.rejection strong {
		font-weight: 600;
	}

	.fixes {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin-top: var(--space-2);
	}

	.fix {
		min-height: 36px;
		padding: var(--space-1) var(--space-3);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-pill);
		background: var(--surface);
		color: var(--text);
		font-size: var(--text-2);
		font-weight: 600;
		text-align: left;
		transition: background-color 150ms ease-out;
	}

	.fix:hover {
		background: var(--surface-alt);
	}

	/* Primary weight: of the two, this is the one tap that almost certainly
	   finishes the turn. */
	.fix.suggestion {
		border-color: transparent;
		background: var(--accent);
		color: var(--accent-text);
	}

	.fix.suggestion:hover {
		background: var(--accent-hover);
	}

	.fix.suggestion:active {
		background: var(--accent-pressed);
	}

	.report-confirmation {
		margin: var(--space-2) 0 0;
		padding: var(--space-3) var(--space-3);
		border-radius: var(--radius-sm);
		background: var(--accent-soft);
		color: var(--text);
		font-size: var(--text-2);
	}
</style>
