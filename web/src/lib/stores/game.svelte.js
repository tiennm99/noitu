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
		/** @type {'idle' | 'playing' | 'over'} */
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
				break;

			case 'roomJoined':
				state.roomCode = value.roomCode;
				state.opponentName = value.opponentName;
				break;

			case 'gameStarted':
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
		}
	};
}

/** The store the routes share. */
export const game = createGameStore();
