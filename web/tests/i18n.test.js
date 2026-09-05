// The Vietnamese copy is the only place prose lives, so these tests guard the
// two ways it can silently fall out of step with the wire contract: a new enum
// value with no message, and a placeholder no caller fills.

import { describe, expect, it } from 'vitest';
import {
	Difficulty,
	GameEndReason,
	RejectReason,
	RejectReasonSchema,
	GameEndReasonSchema,
	DifficultySchema
} from '../src/lib/proto/noitu/v1/game_pb.js';
import {
	difficultyLabels,
	endReasonMessages,
	errorFallback,
	errorMessage,
	fill,
	rejectMessage,
	rejectMessages
} from '../src/lib/i18n/vi.js';

/**
 * Enumerates a generated enum's numeric values from its schema rather than
 * from the message table under test. Reading the table to check the table
 * would pass no matter what is missing.
 *
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

describe('difficulty labels', () => {
	it('names every playable difficulty', () => {
		const playable = valuesOf(DifficultySchema).filter((v) => v !== Difficulty.UNSPECIFIED);
		for (const value of playable) {
			expect(difficultyLabels[value], `Difficulty ${value} has no label`).toBeTypeOf('string');
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
