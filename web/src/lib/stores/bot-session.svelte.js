/**
 * Owns the lifecycle of a vs-bot game: when one is asked for, when that request
 * goes out, and when its result counts towards a record.
 *
 * It exists because inferring the lifecycle from the game model does not work.
 * "Start a game when the store is idle" reads as an invariant but is really a
 * trigger, and clearing the board for a rematch trips it — so the rematch
 * button would send one request itself and the inference would send a second.
 * The server allocates a room per request, so two requests meant two rooms
 * fighting over one socket. A request is a thing the player made, so it is
 * stored as one.
 */

/**
 * @param {object} options
 * @param {(difficulty: number) => boolean} options.start - sends StartBotGame,
 *   reporting whether the socket took it
 */
export function createBotSession({ start }) {
	const state = $state({
		/** @type {number | null} The difficulty waiting to be requested. */
		pending: null,
		/** @type {object | null} The result already counted towards a record. */
		scored: null
	});

	return {
		state,

		/**
		 * Queues a game. Nothing is sent until the socket is open, which is the
		 * usual case on a fresh page load.
		 *
		 * @param {number} difficulty
		 */
		request(difficulty) {
			state.pending = difficulty;
		},

		/**
		 * Sends the queued request if there is one and the socket can carry it.
		 *
		 * @param {boolean} isOpen
		 * @returns {boolean} whether a request was sent
		 */
		flush(isOpen) {
			if (state.pending === null || !isOpen) return false;
			// Cleared only once the socket has taken it. A refused send never
			// reached the server, so holding the request lets the next open
			// connection carry it instead of leaving the player on a dead board.
			// This cannot duplicate a room: only an explicit ask sets `pending`.
			if (!start(state.pending)) return false;
			state.pending = null;
			return true;
		},

		/** Drops a queued request, for a player who left before it went out. */
		cancel() {
			state.pending = null;
		},

		/**
		 * Counts a finished game towards the record for its difficulty, once.
		 *
		 * Identity of the result object is the guard rather than a boolean, so
		 * re-entering the screen with the same result cannot score it twice and
		 * a genuinely new result is never mistaken for the old one.
		 *
		 * @param {object | null} result - GameOver as the store holds it
		 * @param {number} difficulty
		 * @param {{ recordScore: (difficulty: number, score: number) => boolean }} settings
		 * @returns {boolean} whether this game set a new record
		 */
		score(result, difficulty, settings) {
			if (!result || state.scored === result) return false;
			state.scored = result;
			return settings.recordScore(difficulty, /** @type {any} */ (result).myScore);
		},

		/** Forgets the scored result so a new game can set a record again. */
		reset() {
			state.pending = null;
			state.scored = null;
		}
	};
}
