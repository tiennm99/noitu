// Room codes cross a gap the type system cannot see: the server generates them
// from a Go constant and the client validates them against a JavaScript copy.
// The first test here reads the Go source so the copy cannot quietly drift.

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
	ROOM_CODE_ALPHABET,
	ROOM_CODE_LENGTH,
	isRoomCode,
	normalizeRoomCode
} from '../src/lib/room-code.js';

const hubSource = fileURLToPath(new URL('../../server/internal/wsapi/hub.go', import.meta.url));

describe('the alphabet', () => {
	const go = readFileSync(hubSource, 'utf8');

	it('matches the one the server generates codes from', () => {
		const match = go.match(/roomCodeAlphabet\s*=\s*"([^"]+)"/);
		expect(match, 'roomCodeAlphabet not found in hub.go').not.toBeNull();
		expect(ROOM_CODE_ALPHABET).toBe(match?.[1]);
	});

	it('matches the length the server generates', () => {
		const match = go.match(/roomCodeLen\s*=\s*(\d+)/);
		expect(match, 'roomCodeLen not found in hub.go').not.toBeNull();
		expect(ROOM_CODE_LENGTH).toBe(Number(match?.[1]));
	});

	it('omits the characters people confuse when reading a code aloud', () => {
		for (const ch of ['0', 'O', '1', 'I', 'L']) {
			expect(ROOM_CODE_ALPHABET.includes(ch), `${ch} should not be in the alphabet`).toBe(false);
		}
	});
});

describe('normalizeRoomCode', () => {
	it('upper-cases what was typed in lower case', () => {
		expect(normalizeRoomCode('k7m2qp')).toBe('K7M2QP');
	});

	it('accepts a code with punctuation or spacing around it', () => {
		expect(normalizeRoomCode(' K7M2-QP ')).toBe('K7M2QP');
	});

	it('takes the code out of a pasted invite link', () => {
		expect(normalizeRoomCode('https://noitu.example/online?code=K7M2QP')).toBe('K7M2QP');
	});

	it('reads the code from a link that carries other parameters too', () => {
		expect(normalizeRoomCode('https://noitu.example/online?ref=chat&code=K7M2QP#top')).toBe(
			'K7M2QP'
		);
	});

	it('does not try to find a code buried in a sentence', () => {
		// Guessing would occasionally produce a plausible code for the wrong
		// room, which is worse than asking the player to paste the code itself.
		expect(isRoomCode(normalizeRoomCode('vào phòng của tôi nhé'))).toBe(false);
	});

	it('keeps characters outside the alphabet so the code can be refused', () => {
		// A code misread as O for 0 should be reported as malformed, not
		// quietly rewritten into a different six-character code.
		expect(normalizeRoomCode('K0MOQP')).toBe('K0MOQP');
		expect(isRoomCode(normalizeRoomCode('K0MOQP'))).toBe(false);
	});

	it('does not truncate a long paste into a different code', () => {
		expect(isRoomCode(normalizeRoomCode('K7M2QPZZZZ'))).toBe(false);
	});

	it('handles nothing at all', () => {
		expect(normalizeRoomCode('')).toBe('');
		expect(normalizeRoomCode(/** @type {any} */ (undefined))).toBe('');
	});
});

describe('isRoomCode', () => {
	it('accepts a well-formed code', () => {
		expect(isRoomCode('K7M2QP')).toBe(true);
	});

	it('rejects the wrong length', () => {
		expect(isRoomCode('K7M2Q')).toBe(false);
		expect(isRoomCode('K7M2QPZ')).toBe(false);
	});

	it('rejects characters outside the alphabet', () => {
		expect(isRoomCode('K7M2Q0')).toBe(false);
		expect(isRoomCode('k7m2qp')).toBe(false);
	});
});
