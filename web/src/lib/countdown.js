/**
 * The countdown is cosmetic. The server decides when a turn expires and
 * announces it; the ring only has to avoid contradicting that decision.
 *
 * SETTLE_MS is why: it makes the display reach zero slightly before the real
 * deadline. A ring showing "2 giây" while the server has already timed the
 * player out reads as the game cheating, whereas reaching zero a fraction
 * early reads as a rounding.
 */
export const SETTLE_MS = 300;

/**
 * Milliseconds left on the current turn, never negative.
 *
 * @param {number} deadlineMs - the server's absolute deadline
 * @param {number} serverNowMs - the server clock as this client estimates it
 * @param {number} [settleMs]
 * @returns {number}
 */
export function remainingMs(deadlineMs, serverNowMs, settleMs = SETTLE_MS) {
	if (!deadlineMs) return 0;
	return Math.max(0, deadlineMs - settleMs - serverNowMs);
}

/**
 * How much of the turn is left, as 0..1.
 *
 * A turn limit of zero means no timed turn is in progress, which is full
 * rather than empty: an empty ring would announce a timeout that is not
 * happening.
 *
 * @param {number} remaining
 * @param {number} limitMs
 * @returns {number}
 */
export function fraction(remaining, limitMs) {
	if (limitMs <= 0) return 1;
	return Math.min(1, Math.max(0, remaining / limitMs));
}

/**
 * Whole seconds to display. Rounded up, so the last second is shown as "1"
 * for its whole duration instead of flashing "0" while time remains.
 *
 * @param {number} remaining
 * @returns {number}
 */
export function seconds(remaining) {
	return Math.ceil(remaining / 1000);
}
