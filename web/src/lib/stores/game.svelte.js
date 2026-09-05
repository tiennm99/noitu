import { rejectMessage, errorMessage } from '$lib/i18n/vi.js';

/**
 * The game model is a projection of what the server sent. The client never
 * decides whether a word is valid, whose turn it is, or who won — it renders
 * the last message it received. That is what makes the bot and the online
 * modes the same screen.
 *
 * @typedef {object} ChainEntry
 * @property {string} word - the canonical spelling
 * @property {string} typed - what the player actually typed, when it differed
 * @property {boolean} byMe
 * @property {number} points
 * @property {number} syllables
 * @property {boolean} opening - the seed word, played by neither side
 */

/** @returns {any} */
function initialState() {
	return {
		/**
		 * Where the screen is. `waiting` is online-only: the room exists and
		 * its code can be shared, but there is nobody to play yet.
		 *
		 * @type {'idle' | 'waiting' | 'playing' | 'over'}
		 */
		phase: 'idle',
		/** @type {ChainEntry[]} */
		chain: [],
		currentSyllable: '',
		myTurn: false,
		deadlineMs: 0,
		turnSeq: 0,
		turnLimitMs: 0,
		myScore: 0,
		opponentScore: 0,
		chainLength: 0,

		nickname: '',
		opponentName: '',
		roomCode: '',

		/** @type {{ word: string, message: string } | null} */
		rejection: null,
		/** @type {{ iWon: boolean, reason: number, myScore: number, chainLength: number } | null} */
		result: null,
		/** @type {{ canReconnect: boolean, graceMs: number } | null} */
		opponentLeft: null,
		/**
		 * The offer that follows a finished online game, or null when none is
		 * open. Both sides of the answer come from the server, so neither
		 * client has to work out which acceptance is whose.
		 *
		 * @type {{ iAccepted: boolean, opponentAccepted: boolean, expiresInMs: number } | null}
		 */
		rematch: null,
		/** @type {string | null} */
		error: null
	};
}

/**
 * Builds a store instance. Tests construct their own rather than sharing the
 * module singleton, so one test's game cannot leak into the next.
 */
export function createGameStore() {
	const state = $state(initialState());

	/** Returns the model to its pre-game shape, keeping the identity fields. */
	function reset() {
		const fresh = initialState();
		for (const key of Object.keys(fresh)) {
			if (key === 'nickname' || key === 'roomCode' || key === 'opponentName') continue;
			state[key] = fresh[key];
		}
	}

	/**
	 * Applies one ServerMessage. Every arm of the oneof is handled here and
	 * nowhere else, so adding a message to the protocol has exactly one place
	 * in the client that has to learn about it.
	 *
	 * @param {any} msg - a decoded ServerMessage
	 */
	function apply(msg) {
		const { case: kind, value } = msg.payload;

		switch (kind) {
			case 'welcome':
				// The server sanitizes the requested name, so what it returns
				// is the only name safe to display — never the raw input.
				state.nickname = value.acceptedNickname;
				break;

			case 'roomCreated':
				state.roomCode = value.roomCode;
				// The room exists but has one seat filled. A resumed session
				// that was waiting lands here too, which is why the phase is
				// set rather than assumed.
				if (state.phase !== 'playing') state.phase = 'waiting';
				break;

			case 'roomJoined':
				state.roomCode = value.roomCode;
				state.opponentName = value.opponentName;
				// The opponent is in the room, which is also how a reconnect is
				// announced. Either way there is nobody to be waiting for.
				state.opponentLeft = null;
				break;

			case 'gameStarted':
				// reset() already clears the rematch offer that led here.
				reset();
				state.phase = 'playing';
				state.chain = [
					{
						word: value.openingWord,
						typed: '',
						byMe: false,
						points: 0,
						syllables: 0,
						opening: true
					}
				];
				state.currentSyllable = value.currentSyllable;
				state.myTurn = value.myTurn;
				state.deadlineMs = Number(value.deadlineUnixMs);
				state.turnSeq = value.turnSeq;
				state.turnLimitMs = value.turnLimitMs;
				state.chainLength = 1;
				break;

			case 'turnUpdate': {
				const played = value.played;
				if (played) {
					state.chain.push({
						word: played.word,
						typed: played.typed,
						byMe: played.byMe,
						points: played.points,
						syllables: played.syllables,
						opening: false
					});
				}
				state.currentSyllable = value.currentSyllable;
				state.myTurn = value.myTurn;
				state.deadlineMs = Number(value.deadlineUnixMs);
				state.turnSeq = value.turnSeq;
				state.myScore = value.myScore;
				state.opponentScore = value.opponentScore;
				state.chainLength = value.chainLength;
				// An accepted move answers the previous rejection.
				state.rejection = null;
				break;
			}

			case 'moveRejected':
				state.rejection = {
					word: value.word,
					message: rejectMessage(value.reason, state.currentSyllable)
				};
				break;

			case 'gameOver':
				state.phase = 'over';
				state.myTurn = false;
				state.result = {
					iWon: value.iWon,
					reason: value.reason,
					myScore: value.myScore,
					chainLength: value.chainLength
				};
				break;

			case 'opponentLeft':
				state.opponentLeft = {
					canReconnect: value.canReconnect,
					graceMs: value.graceMs
				};
				// An opponent who cannot come back also ends any rematch offer.
				// Leaving the prompt up would show a countdown with nobody left
				// to answer it.
				if (!value.canReconnect) state.rematch = null;
				break;

			case 'rematchState':
				state.rematch = {
					iAccepted: value.iAccepted,
					opponentAccepted: value.opponentAccepted,
					expiresInMs: value.expiresInMs
				};
				break;

			case 'error':
				state.error = errorMessage(value.code);
				break;

			case 'pong':
				// Handled by the transport, which owns the clock offset.
				break;
		}
	}

	return {
		state,
		apply,
		reset,
		clearRejection() {
			state.rejection = null;
		},
		clearError() {
			state.error = null;
		},
		clearOpponentLeft() {
			state.opponentLeft = null;
		},
		clearRematch() {
			state.rematch = null;
		}
	};
}

/** The store the routes share. */
export const game = createGameStore();
