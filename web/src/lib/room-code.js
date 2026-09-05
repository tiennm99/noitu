/**
 * Room codes as the client has to handle them: typed by hand, or pasted from a
 * chat message.
 *
 * The alphabet mirrors the server's. It omits 0/O and 1/I/L because those are
 * the pairs people get wrong when reading a code out. A test checks this
 * against the Go constant rather than trusting the copy.
 */
export const ROOM_CODE_ALPHABET = '23456789ABCDEFGHJKMNPQRSTUVWXYZ';

export const ROOM_CODE_LENGTH = 6;

/** Pulls the code out of a pasted invite link. */
const LINK_CODE = /[?&]code=([^&#\s]+)/i;

/** Separators people put inside or around a code when writing it down. */
const SEPARATORS = /[\s._-]+/g;

/**
 * Turns whatever arrived into what the server would recognise.
 *
 * Two shapes are worth handling, because they are the two ways a code reaches
 * a player: the code itself, in any case and spaced or hyphenated for reading,
 * and a whole invite link pasted from a message.
 *
 * Only separators are removed. Dropping every character outside the alphabet
 * instead would turn any sentence into six plausible letters, and the player
 * would be told the room does not exist rather than that they pasted the wrong
 * thing.
 *
 * @param {string} raw
 * @returns {string}
 */
export function normalizeRoomCode(raw) {
	const text = String(raw ?? '').trim();
	const link = text.match(LINK_CODE);
	const source = link ? link[1] : text;

	return source.replace(SEPARATORS, '').toUpperCase();
}

/**
 * Whether a normalized code is worth sending. Checking here saves a round trip
 * and, more usefully, tells the player which of the two failures they have:
 * a malformed code, or a room that is not there.
 *
 * @param {string} code
 * @returns {boolean}
 */
export function isRoomCode(code) {
	return (
		code.length === ROOM_CODE_LENGTH && [...code].every((ch) => ROOM_CODE_ALPHABET.includes(ch))
	);
}
