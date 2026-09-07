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
		 * Where the screen is. `lobby` is online-only: the room exists and its
		 * code can be shared, and it is where every game is agreed before it
		 * starts and returned to after it ends.
		 *
		 * RoomState deliberately does not move this. A game running is what
		 * the phase is about, and only GameStarted and GameOver know that.
		 *
		 * @type {'idle' | 'lobby' | 'playing' | 'over'}
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

		/**
		 * The lobby, exactly as the server last described it. Every field is
		 * server-owned: the client never decides who owns the room, who is
		 * ready, or whether a game may start.
		 */
		isOwner: false,
		isReady: false,
		canStart: false,
		opponentPresent: false,
		opponentReady: false,
		opponentConnected: false,

		/** @type {{ word: string, message: string } | null} */
		rejection: null,
		/**
		 * The finished game. `suggestions` is what the position still had to
		 * offer and arrives only for the player who lost; empty on a loss
		 * means the position was a dead end, which is a different thing to
		 * say than "here is what you missed".
		 *
		 * @type {{ iWon: boolean, reason: number, myScore: number, chainLength: number, suggestions: string[] } | null}
		 */
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

	/**
	 * Returns the model to its pre-game shape, keeping the identity fields and
	 * the lobby. A game ending, or a new one starting, does not change which
	 * room this is or who is in it — the server says so with its own message.
	 */
	const kept = new Set([
		'nickname',
		'roomCode',
		'opponentName',
		'isOwner',
		'isReady',
		'canStart',
		'opponentPresent',
		'opponentReady',
		'opponentConnected'
	]);

	function reset() {
		const fresh = initialState();
		for (const key of Object.keys(fresh)) {
			if (kept.has(key)) continue;
			state[key] = fresh[key];
		}
	}

	/**
	 * Forgets the room entirely: this connection is not in one any more, which
	 * is what leaving and being kicked have in common.
	 */
	function leave() {
		const fresh = initialState();
		for (const key of Object.keys(fresh)) {
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

			case 'roomState':
				// One snapshot, applied wholesale. Merging fields selectively
				// is how a client ends up believing a mixture of two states
				// the server was never in.
				state.roomCode = value.roomCode;
				state.isOwner = value.iAmOwner;
				state.isReady = value.iAmReady;
				state.canStart = value.canStart;
				state.opponentPresent = value.opponentPresent;
				state.opponentName = value.opponentName;
				state.opponentReady = value.opponentReady;
				state.opponentConnected = value.opponentConnected;
				// A room state arriving mid-game is a presence change, and an
				// opponent who is connected again is not one to wait for.
				if (value.opponentPresent && value.opponentConnected) state.opponentLeft = null;
				// The lobby is where a room sits when no game is on. `over`
				// keeps its result panel, which the lobby appears beneath.
				if (state.phase === 'idle') state.phase = 'lobby';
				break;

			case 'gameStarted':
				// reset() clears the readiness that led here, along with the
				// last game's board.
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
					chainLength: value.chainLength,
					suggestions: value.suggestions ?? []
				};
				break;

			case 'opponentLeft':
				// Only sent while a game is running: in a lobby the same fact
				// arrives as part of the room's own state.
				state.opponentLeft = {
					canReconnect: value.canReconnect,
					graceMs: value.graceMs
				};
				state.opponentConnected = false;
				break;

			case 'error':
				// Two of them also end this player's membership of the room, so
				// the model has to stop describing one. Set after, because
				// leaving clears everything including the message.
				if (value.code === 'kicked' || value.code === 'room_idle_closed') leave();
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
		leave,
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
