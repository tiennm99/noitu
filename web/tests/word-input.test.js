// @vitest-environment jsdom

// The word field's three direct writes — the turn seed, the suggestion fill,
// and the submit-clear — are the exceptions to an otherwise fully
// uncontrolled field, so each is worth pinning down: the seed fires once per
// turn and never during the player's own composition, the suggestion lands
// on click, and the seed itself is gated on the connection as well as the
// turn.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { unmount, flushSync } from 'svelte';
import WordInput from '../src/lib/components/WordInput.svelte';
import { RejectReason } from '../src/lib/proto/noitu/v1/game_pb.js';
import { game } from '../src/lib/stores/game.svelte.js';
import { Status, connection } from '../src/lib/ws/connection.svelte.js';
import { receive, render, startGame as startTurn } from './component-support.js';

/** @param {string} suggestion */
function rejectWith(suggestion) {
	receive('moveRejected', {
		reason: RejectReason.NOT_IN_DICTIONARY,
		word: 'binh yen',
		turnSeq: 1,
		suggestion
	});
}

function renderWordInput() {
	const { target, component } = render(WordInput, {
		onsubmit: () => true,
		onreportword: () => {}
	});
	/** @type {HTMLInputElement} */
	const input = target.querySelector('input');
	return { target, component, input };
}

beforeEach(() => {
	game.leave();
	connection.status = Status.OPEN;
});

afterEach(() => {
	document.body.innerHTML = '';
	connection.status = Status.CLOSED;
});

describe('seeding the turn', () => {
	it('fills the field with the current syllable once the turn starts', () => {
		startTurn({ currentSyllable: 'an' });
		const { input, component } = renderWordInput();

		expect(input.value).toBe('an ');
		unmount(component);
	});

	it('does not seed, or focus, while the connection is down', () => {
		// Seeding during a reconnect wrote into a field the player could not
		// submit from, and the first composition event then undid it.
		connection.status = Status.CLOSED;
		startTurn({ currentSyllable: 'an' });
		const { input, component } = renderWordInput();

		expect(input.value).toBe('');
		expect(document.activeElement).not.toBe(input);
		unmount(component);
	});

	it('does not reseed the same turn once the player has started typing', () => {
		startTurn({ turnSeq: 5, currentSyllable: 'an' });
		const { input, component } = renderWordInput();
		expect(input.value).toBe('an ');

		input.value = 'an ninh';
		input.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();

		// A connection blip and recovery re-evaluates the seeding effect
		// (enabled depends on connection.status) without the turn changing.
		connection.status = Status.CLOSED;
		flushSync();
		connection.status = Status.OPEN;
		flushSync();

		expect(input.value).toBe('an ninh');
		unmount(component);
	});

	it('seeds again for a new turn', () => {
		startTurn({ turnSeq: 1, currentSyllable: 'an' });
		const { input, component } = renderWordInput();
		input.value = 'an ninh';
		input.dispatchEvent(new Event('input', { bubbles: true }));

		startTurn({ turnSeq: 2, currentSyllable: 'ninh' });
		flushSync();

		expect(input.value).toBe('ninh ');
		unmount(component);
	});
});

/** Passes the turn to the other player, as the server's next update would. */
function passTurn() {
	receive('turnUpdate', {
		currentSyllable: 'ninh',
		myTurn: false,
		deadlineUnixMs: 1_700_000_040_000n,
		turnSeq: 2,
		chainLength: 1,
		players: [
			{ playerId: 'p1', name: 'Minh', isMe: true, connected: true },
			{ playerId: 'p2', name: 'Lan', isMe: false, connected: true }
		],
		turnPlayerId: 'p2'
	});
	flushSync();
}

/** @param {HTMLInputElement} input */
function typeInto(input, text) {
	input.value = text;
	input.dispatchEvent(new Event('input', { bubbles: true }));
	flushSync();
}

describe('submitting', () => {
	/** @param {boolean} accepted */
	function submitWith(accepted) {
		startTurn({ currentSyllable: 'an' });
		/** @type {string[]} */
		const sent = [];
		const { target, component } = render(WordInput, {
			onsubmit: (/** @type {string} */ word) => {
				sent.push(word);
				return accepted;
			},
			onreportword: () => {}
		});
		/** @type {HTMLInputElement} */
		const input = target.querySelector('input');
		typeInto(input, 'an ninh');
		target.querySelector('form')?.requestSubmit();
		flushSync();
		return { input, sent, component };
	}

	it('clears the field once the word went out', () => {
		const { input, sent, component } = submitWith(true);

		expect(sent).toEqual(['an ninh']);
		expect(input.value).toBe('');
		unmount(component);
	});

	it('keeps the word when the socket refused it, so it can be sent again', () => {
		const { input, sent, component } = submitWith(false);

		expect(sent).toEqual(['an ninh']);
		expect(input.value).toBe('an ninh');
		unmount(component);
	});

	it('refuses to submit while an accent is still being composed', () => {
		startTurn({ currentSyllable: 'an' });
		/** @type {string[]} */
		const sent = [];
		const { target, component } = render(WordInput, {
			onsubmit: (/** @type {string} */ word) => !!sent.push(word),
			onreportword: () => {}
		});
		/** @type {HTMLInputElement} */
		const input = target.querySelector('input');
		typeInto(input, 'an niệ');

		input.dispatchEvent(new Event('compositionstart'));
		target.querySelector('form')?.requestSubmit();
		flushSync();
		expect(sent).toEqual([]);
		expect(input.value).toBe('an niệ');

		input.dispatchEvent(new Event('compositionend'));
		target.querySelector('form')?.requestSubmit();
		flushSync();
		expect(sent).toEqual(['an niệ']);
		unmount(component);
	});
});

describe('out of turn', () => {
	it('cancels typed text before it lands, and allows it on the player\'s own turn', () => {
		startTurn({ currentSyllable: 'an' });
		const { input, component } = renderWordInput();

		const own = new Event('beforeinput', { cancelable: true, bubbles: true });
		input.dispatchEvent(own);
		expect(own.defaultPrevented).toBe(false);

		passTurn();
		const other = new Event('beforeinput', { cancelable: true, bubbles: true });
		input.dispatchEvent(other);
		expect(other.defaultPrevented).toBe(true);
		unmount(component);
	});

	it('puts back what a composition wrote past the guard', () => {
		// beforeinput cannot be cancelled for a composition, so the text
		// lands and has to be taken straight back out.
		startTurn({ currentSyllable: 'an' });
		const { input, component } = renderWordInput();
		expect(input.value).toBe('an ');

		passTurn();
		input.dispatchEvent(new Event('compositionstart'));
		typeInto(input, 'an niệ');

		expect(input.value).toBe('an ');
		unmount(component);
	});

	it('leaves the player\'s own text alone while it is their turn', () => {
		startTurn({ currentSyllable: 'an' });
		const { input, component } = renderWordInput();

		input.dispatchEvent(new Event('compositionstart'));
		typeInto(input, 'an niệ');

		expect(input.value).toBe('an niệ');
		input.dispatchEvent(new Event('compositionend'));
		unmount(component);
	});
});

describe('a rejected word\'s suggestion', () => {
	it('fills the field with the suggestion on click', () => {
		startTurn({ currentSyllable: 'an' });
		const { target, input, component } = renderWordInput();
		input.value = 'binh yen';
		rejectWith('bình yên');
		flushSync();

		/** @type {HTMLButtonElement | null} */
		const suggestion = target.querySelector('.suggestion');
		expect(suggestion).not.toBeNull();
		suggestion?.click();
		flushSync();

		expect(input.value).toBe('bình yên');
		unmount(component);
	});

	it('offers no suggestion button when the server sent none', () => {
		startTurn({ currentSyllable: 'an' });
		const { target, component } = renderWordInput();
		rejectWith('');
		flushSync();

		expect(target.querySelector('.suggestion')).toBeNull();
		unmount(component);
	});
});
