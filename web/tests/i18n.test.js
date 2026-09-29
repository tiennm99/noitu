// The Vietnamese copy is the only place prose lives, so these tests guard the
// two ways it can silently fall out of step with the wire contract: a new enum
// value with no message, and a placeholder no caller fills.

import { readdirSync, readFileSync } from 'node:fs';
import { join, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, expect, it } from 'vitest';
import {
	Difficulty,
	GameEndReason,
	PointKind,
	RejectReason,
	RejectReasonSchema,
	GameEndReasonSchema,
	DifficultySchema,
	PointKindSchema
} from '../src/lib/proto/noitu/v1/game_pb.js';
import {
	difficultyHints,
	difficultyLabels,
	endReasonMessages,
	errorFallback,
	errorMessage,
	fill,
	pointKindLabels,
	rejectMessage,
	rejectMessages,
	t
} from '../src/lib/i18n/vi.js';

/**
 * Enumerates a generated enum's numeric values from its schema rather than
 * from the message table under test. Reading the table to check the table
 * would pass no matter what is missing.
 * @param {{ values: { number: number }[] }} schema
 */
function valuesOf(schema) {
	return schema.values.map((v) => v.number);
}

describe('rejection messages', () => {
	it('covers every RejectReason in the schema', () => {
		for (const value of valuesOf(RejectReasonSchema)) {
			expect(rejectMessages[value], `RejectReason ${value} has no Vietnamese message`).toBeTypeOf(
				'string'
			);
			expect(rejectMessages[value].length).toBeGreaterThan(0);
		}
	});

	it('fills the syllable placeholder for a wrong link', () => {
		expect(rejectMessage(RejectReason.WRONG_LINK, 'sinh')).toBe(
			'Từ phải bắt đầu bằng tiếng “sinh”.'
		);
	});

	it('leaves no unfilled placeholder in any rendered message', () => {
		for (const value of valuesOf(RejectReasonSchema)) {
			expect(rejectMessage(value, 'sinh')).not.toMatch(/\{\w+\}/);
		}
	});

	it('falls back to the unspecified message for a value this build predates', () => {
		expect(rejectMessage(999, 'sinh')).toBe(rejectMessages[RejectReason.UNSPECIFIED]);
	});
});

describe('end reason messages', () => {
	it('covers every GameEndReason in the schema', () => {
		for (const value of valuesOf(GameEndReasonSchema)) {
			expect(endReasonMessages[value], `GameEndReason ${value} has no entry`).toBeTypeOf('string');
		}
	});

	it('gives every real reason a non-empty message', () => {
		// Only UNSPECIFIED is deliberately blank. Asserting the type alone would
		// let a reason be silently emptied and still report green while the
		// game-over screen explained nothing.
		const real = valuesOf(GameEndReasonSchema).filter((v) => v !== GameEndReason.UNSPECIFIED);
		for (const value of real) {
			expect(endReasonMessages[value].length, `GameEndReason ${value} is blank`).toBeGreaterThan(0);
		}
	});

	it('leaves the unspecified reason empty, so the screen shows only the result', () => {
		expect(endReasonMessages[GameEndReason.UNSPECIFIED]).toBe('');
	});
});

describe('point kind labels', () => {
	it('names every scoring term except the unspecified one', () => {
		const real = valuesOf(PointKindSchema).filter((v) => v !== PointKind.UNSPECIFIED);
		expect(real.length).toBeGreaterThan(0);
		for (const value of real) {
			expect(pointKindLabels[value], `PointKind ${value} has no label`).toBeTypeOf('string');
			expect(pointKindLabels[value].length).toBeGreaterThan(0);
		}
	});

	it('carries no label for the unspecified kind, which never reaches a player', () => {
		expect(pointKindLabels[PointKind.UNSPECIFIED]).toBeUndefined();
	});
});

describe('difficulty labels', () => {
	it('names every playable difficulty', () => {
		const playable = valuesOf(DifficultySchema).filter((v) => v !== Difficulty.UNSPECIFIED);
		for (const value of playable) {
			expect(difficultyLabels[value], `Difficulty ${value} has no label`).toBeTypeOf('string');
		}
	});

	it('gives every playable difficulty a hint for the record line to fall back on', () => {
		const playable = valuesOf(DifficultySchema).filter((v) => v !== Difficulty.UNSPECIFIED);
		for (const value of playable) {
			expect(difficultyHints[value], `Difficulty ${value} has no hint`).toBeTypeOf('string');
			expect(difficultyHints[value].length).toBeGreaterThan(0);
		}
	});
});

describe('server error codes', () => {
	it('translates a known code', () => {
		expect(errorMessage('room_not_found')).toBe('Không tìm thấy phòng với mã này.');
	});

	it('never leaks an unknown key into the interface', () => {
		expect(errorMessage('a_code_added_after_this_build')).toBe(errorFallback);
	});
});

describe('fill', () => {
	it('leaves a placeholder alone when no value was supplied', () => {
		expect(fill('xin chào {name}')).toBe('xin chào {name}');
	});

	it('replaces every occurrence', () => {
		expect(fill('{a} và {a}', { a: 'x' })).toBe('x và x');
	});
});

describe('fill call sites', () => {
	const src = fileURLToPath(new URL('../src', import.meta.url));

	/** Every source file a `fill(t.key, { ... })` call can live in. */
	const files = readdirSync(src, { recursive: true, encoding: 'utf8' })
		.filter((f) => /\.(svelte|js)$/.test(f) && !f.split(sep).includes('proto'))
		.map((f) => join(src, f));

	/**
	 * Splits an object literal's body on its top-level commas, so a value that
	 * is itself a call with arguments stays whole.
	 * @param {string} body
	 */
	function topLevel(body) {
		/** @type {string[]} */
		const parts = [];
		let depth = 0;
		let from = 0;
		for (let i = 0; i < body.length; i++) {
			if ('([{'.includes(body[i])) depth++;
			else if (')]}'.includes(body[i])) depth--;
			else if (body[i] === ',' && depth === 0) {
				parts.push(body.slice(from, i));
				from = i + 1;
			}
		}
		parts.push(body.slice(from));
		return parts.map((p) => p.trim()).filter(Boolean);
	}

	// Matches the direct form, `fill(t.key, { ... })`. A call whose template is
	// chosen by an expression is not visible to a regex and is not covered.
	const call = /\bfill\(\s*t\.(\w+)\s*,\s*\{((?:[^{}]|\{[^{}]*\})*)\}/g;

	it('supplies exactly the placeholders each template asks for', () => {
		let seen = 0;
		for (const file of files) {
			const text = readFileSync(file, 'utf8');
			for (const [, key, body] of text.matchAll(call)) {
				seen++;
				const template = /** @type {Record<string, string>} */ (t)[key];
				expect(template, `${file}: t.${key} does not exist`).toBeTypeOf('string');

				const asked = [...template.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();
				const given = topLevel(body)
					.map((entry) => entry.match(/^(\w+)\s*(?::|$)/)?.[1] ?? '?')
					.sort();
				expect(given, `${file}: fill(t.${key}) placeholders`).toEqual(asked);
			}
		}
		// A scan that matched nothing would pass for the wrong reason.
		expect(seen).toBeGreaterThan(10);
	});
});
