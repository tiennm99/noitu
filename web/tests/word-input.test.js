// @vitest-environment jsdom

// The word field's three direct writes — the turn seed, the suggestion fill,
// and the submit-clear — are the exceptions to an otherwise fully
// uncontrolled field, so each is worth pinning down: the seed fires once per
// turn and never during the player's own composition, the suggestion lands
// on click, and the seed itself is gated on the connection (C6) as well as
// the turn.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import { create } from '@bufbuild/protobuf';
import WordInput from '../src/lib/components/WordInput.svelte';
import { RejectReason, ServerMessageSchema } from '../src/lib/proto/noitu/v1/game_pb.js';
import { game } from '../src/lib/stores/game.svelte.js';
import { Status, connection } from '../src/lib/ws/connection.svelte.js';

/** @param {{ turnSeq?: number, currentSyllable?: string }} [fields] */
function startTurn({ turnSeq = 1, currentSyllable = 'an' } = {}) {
	game.apply(
		create(ServerMessageSchema, {
			payload: {
				case: 'gameStarted',
				value: {
					openingWord: 'bình yên',
					currentSyllable,
					myTurn: true,
					deadlineUnixMs: 1_700_000_020_000n,
					turnSeq,
					turnLimitMs: 20_000,
					players: [{ playerId: 'p1', name: 'Minh', isMe: true, connected: true }],
					turnPlayerId: 'p1'
				}
			}
		})
	);
}

/** @param {string} suggestion */
function rejectWith(suggestion) {
	game.apply(
		create(ServerMessageSchema, {
			payload: {
				case: 'moveRejected',
				value: {
					reason: RejectReason.NOT_IN_DICTIONARY,
					word: 'binh yen',
					turnSeq: 1,
					suggestion
				}
			}
		})
	);
}

function renderWordInput() {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(WordInput, {
		target,
		props: { onsubmit: () => true, onreportword: () => {} }
	});
	flushSync();
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
		// C6: seeding during a reconnect wrote into a field the player could
		// not submit from, and the first composition event then undid it.
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

describe('composing on the player\'s own turn', () => {
	it('leaves an in-progress composition alone', () => {
		startTurn({ currentSyllable: 'an' });
		const { input, component } = renderWordInput();

		input.dispatchEvent(new Event('compositionstart'));
		input.value = 'an niệ';
		input.dispatchEvent(new Event('input', { bubbles: true }));
		flushSync();

		// undoInput only reverts out of turn; on the player's own turn it is
		// a no-op, so a composed character in flight is never taken back.
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
