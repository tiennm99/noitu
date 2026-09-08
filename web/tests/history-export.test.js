// The transcript is the only artefact of a game that outlives the tab, so what
// it contains is worth pinning: the chain in playing order, who played what,
// and a filename that survives every filesystem.

import { describe, expect, it } from 'vitest';
import { chainToText, historyFilename } from '../src/lib/history-export.js';

/**
 * @param {Partial<import('../src/lib/stores/game.svelte.js').ChainEntry>} entry
 */
function link(entry) {
	return {
		word: '',
		typed: '',
		byMe: false,
		playerId: '',
		points: 0,
		syllables: 2,
		opening: false,
		...entry
	};
}

const chain = [
	link({ word: 'học sinh', opening: true, syllables: 0 }),
	link({ word: 'sinh viên', byMe: true, playerId: 'p1', points: 2 }),
	link({ word: 'viên chức', playerId: 'p2', points: 2 })
];

/** Resolves a seat to the name that seat was playing under. */
const nameOf = (/** @type {string} */ id) => (id === 'p2' ? 'Minh' : '');

const at = new Date(2026, 8, 7, 15, 25);

describe('chainToText', () => {
	it('keeps the chain in playing order, opening word first', () => {
		const lines = chainToText({ chain, at }).trim().split('\n');
		const words = lines.filter((line) => /^\d+\./.test(line));

		expect(words).toHaveLength(3);
		expect(words[0]).toContain('học sinh');
		expect(words[1]).toContain('sinh viên');
		expect(words[2]).toContain('viên chức');
	});

	it('marks the opening word as played by neither side', () => {
		const text = chainToText({ chain, at });

		expect(text).toContain('1. học sinh (từ mở đầu)');
		expect(text).not.toContain('học sinh — ');
	});

	it('names who played each word and what it scored', () => {
		const text = chainToText({ chain, at, nameOf });

		expect(text).toContain('2. sinh viên — Bạn +2');
		expect(text).toContain('3. viên chức — Minh +2');
	});

	it('falls back to a generic label for a seat whose name is gone', () => {
		// A player who left takes their name with them, and the transcript is
		// written long after. The line still has to say it was not this
		// player's word.
		const text = chainToText({ chain, at });

		expect(text).toContain('3. viên chức — Đối thủ +2');
	});

	it('heads the file with the result when there is one', () => {
		const text = chainToText({
			chain,
			at,
			result: { iWon: true, myScore: 4, chainLength: 3 }
		});

		expect(text).toContain('2026-09-07 15:25');
		expect(text).toContain('Bạn thắng!');
		expect(text).toContain('Điểm cuối cùng: 4');
		expect(text).toContain('Số từ trong chuỗi: 3');
	});

	it('writes a chain with no result at all', () => {
		const text = chainToText({ chain: [], at });

		expect(text).toContain('2026-09-07 15:25');
		expect(text).not.toContain('Bạn thắng!');
		expect(text).not.toContain('Bạn thua.');
	});
});

describe('historyFilename', () => {
	it('stamps the local date and time without a colon', () => {
		expect(historyFilename(at)).toBe('noi-tu-2026-09-07-1525.txt');
	});

	it('pads every field so names sort chronologically', () => {
		expect(historyFilename(new Date(2026, 0, 2, 3, 4))).toBe('noi-tu-2026-01-02-0304.txt');
	});
});
