// The countdown is the one place the client shows a number the server owns, so
// the rule under test is that it never claims more time than the server allows.

import { describe, expect, it } from 'vitest';
import { SETTLE_MS, fraction, remainingMs, seconds } from '../src/lib/countdown.js';

describe('remainingMs', () => {
	it('reaches zero before the server deadline, never after it', () => {
		const deadline = 100_000;
		expect(remainingMs(deadline, deadline - SETTLE_MS)).toBe(0);
		expect(remainingMs(deadline, deadline - SETTLE_MS - 1)).toBe(1);
	});

	it('never goes negative once the deadline has passed', () => {
		expect(remainingMs(100_000, 200_000)).toBe(0);
	});

	it('is zero when no turn is running', () => {
		expect(remainingMs(0, 50_000)).toBe(0);
	});

	it('counts against the server clock, so a wrong device clock is corrected', () => {
		// The client believes it is 80_000; the estimated server clock is
		// 90_000. The ring has to follow the server.
		expect(remainingMs(100_000, 90_000)).toBe(10_000 - SETTLE_MS);
	});
});

describe('fraction', () => {
	it('is full at the start of a turn and empty at the end', () => {
		expect(fraction(20_000, 20_000)).toBe(1);
		expect(fraction(0, 20_000)).toBe(0);
	});

	it('is half way through a half-spent turn', () => {
		expect(fraction(10_000, 20_000)).toBe(0.5);
	});

	it('shows a full ring when no turn is timed, not an expired one', () => {
		expect(fraction(0, 0)).toBe(1);
	});

	it('clamps rather than overflowing the arc', () => {
		expect(fraction(30_000, 20_000)).toBe(1);
		expect(fraction(-5, 20_000)).toBe(0);
	});
});

describe('seconds', () => {
	it('shows the last second for its whole duration', () => {
		expect(seconds(1)).toBe(1);
		expect(seconds(999)).toBe(1);
		expect(seconds(1000)).toBe(1);
		expect(seconds(1001)).toBe(2);
	});

	it('shows zero only when the time is actually gone', () => {
		expect(seconds(0)).toBe(0);
	});
});
