import { applyTo, CHAT_WINDOW } from './game-apply.js';
import { initialState } from './game-shape.js';

export { CHAT_WINDOW };

/**
 * @typedef {import('./game-shape.js').GameState} GameState
 * @typedef {import('./game-shape.js').ChainEntry} ChainEntry
 * @typedef {import('./game-shape.js').PointPart} PointPart
 * @typedef {import('./game-shape.js').PlayerSlot} PlayerSlot
 * @typedef {import('./game-shape.js').PlayerScore} PlayerScore
 * @typedef {import('$lib/proto/noitu/v1/game_pb.js').ServerMessage} ServerMessage
 */

/**
 * Fields a game ending, or a new one starting, does not change: they
 * describe the room, not the game.
 * @type {readonly (keyof GameState)[]}
 */
const KEPT = [
	'nickname',
	'roomCode',
	'roomPlayers',
	'canStart',
	'maxPlayers',
	'minPlayers',
	'graceMs',
	'chat',
	'chatCount'
];

/**
 * Copies the listed fields from `from` into `into`. A small generic instead
 * of a `Set` plus an index write: both sides are known to be `GameState[K]`
 * for whichever `K` is being copied, so this typechecks without a cast.
 * @template {keyof GameState} K
 * @param {GameState} into
 * @param {GameState} from
 * @param {readonly K[]} keys
 */
function carry(into, from, keys) {
	for (const k of keys) into[k] = from[k];
}

/**
 * Builds a store instance. Tests construct their own rather than sharing the
 * module singleton, so one test's game cannot leak into the next.
 */
export function createGameStore() {
	const state = $state(initialState());

	/**
	 * Returns the model to its pre-game shape, keeping the identity fields and
	 * the room. A game ending, or a new one starting, does not change which
	 * room this is or who is in it — the server says so with its own message.
	 */
	function reset() {
		const fresh = initialState();
		carry(fresh, state, KEPT);
		Object.assign(state, fresh);
	}

	/**
	 * Forgets the room entirely: this connection is not in one any more, which
	 * is what leaving and being kicked have in common.
	 */
	function leave() {
		Object.assign(state, initialState());
	}

	/**
	 * Applies one ServerMessage. Every arm of the oneof is handled in
	 * `game-apply.js` and nowhere else, so adding a message to the protocol
	 * has exactly one place in the client that has to learn about it.
	 *
	 * Wrapped here rather than in `applyTo` itself: a throw partway through a
	 * case would otherwise leave the `$state` proxy half-mutated on screen —
	 * `roomState` sets `queued`/`roomCode`/`canStart` before mapping
	 * `players`, for instance — so the whole application is treated as one
	 * step, logged and discarded rather than left half done.
	 * @param {ServerMessage} msg
	 */
	function apply(msg) {
		try {
			applyTo(state, msg, { reset, leave });
		} catch (err) {
			console.error('failed to apply server message', msg.payload.case, err);
		}
	}

	return {
		state,
		apply,
		reset,
		leave,

		/**
		 * The recipient's own row in the room. Their role and their readiness
		 * live there and nowhere else: a second copy alongside the list is a
		 * second thing to keep in step with the server.
		 * @returns {PlayerSlot | null}
		 */
		get me() {
			return state.roomPlayers.find((p) => p.isMe) ?? null;
		},
		get isOwner() {
			return this.me?.isOwner ?? false;
		},
		get isReady() {
			return this.me?.ready ?? false;
		},
		/** How many seats are still free, for a lobby that draws the empty ones. */
		get freeSeats() {
			return Math.max(0, state.maxPlayers - state.roomPlayers.length);
		},
		/** Whether this player has been knocked out of the game still running. */
		get iAmOut() {
			return state.phase === 'playing' && state.elimination !== null;
		},
		/** This player's score in the game on screen, finished or not. */
		get myScore() {
			const table = state.phase === 'over' ? state.standings : state.gamePlayers;
			return table.find((p) => p.isMe)?.score ?? 0;
		},
		/**
		 * How many games a seat has won since the room opened. Read off the
		 * room rather than the game: the tally spans games, and the table of
		 * the one on screen cannot carry it.
		 * @param {string} playerId
		 * @returns {number}
		 */
		winsOf(playerId) {
			return state.roomPlayers.find((p) => p.playerId === playerId)?.wins ?? 0;
		},
		/**
		 * Which seat a player is in, 1-based, or 0 for nobody. It is what the
		 * chat log colours a line by — the server says which seat spoke, so
		 * the client never has to match display names.
		 * @param {string} playerId
		 * @returns {number}
		 */
		seatIndexOf(playerId) {
			if (!playerId) return 0;
			const at = state.roomPlayers.findIndex((p) => p.playerId === playerId);
			return at < 0 ? 0 : at + 1;
		},
		/**
		 * Everybody whose reconnect window is currently running. The lobby and
		 * the board both wait on the same list.
		 * @returns {PlayerSlot[]}
		 */
		get awayPlayers() {
			return state.roomPlayers.filter((p) => !p.isMe && !p.connected);
		},
		/**
		 * The name behind a seat id, for the chain and the board. Falls back to
		 * the id's absence rather than inventing a label: an empty string is
		 * something a caller can substitute its own copy for.
		 * @param {string} playerId
		 * @returns {string}
		 */
		nameOf(playerId) {
			const from = state.gamePlayers.length ? state.gamePlayers : state.standings;
			return (
				from.find((p) => p.playerId === playerId)?.name ??
				state.roomPlayers.find((p) => p.playerId === playerId)?.name ??
				''
			);
		},

		clearError() {
			state.error = null;
		},
		clearClaimError() {
			state.claimError = null;
		},
		clearReportConfirmation() {
			state.reportConfirmation = null;
		},
		/**
		 * Whether a word's meaning panel is open.
		 * @param {string} word
		 */
		isExpanded(word) {
			return state.expanded.includes(word);
		},
		/**
		 * Opens a closed word's meaning or closes an open one. Every word in the
		 * chain toggles, with or without a definition, so the chain behaves
		 * the same for all of them.
		 * @param {string} word
		 */
		toggleMeaning(word) {
			if (state.expanded.includes(word)) {
				state.expanded = state.expanded.filter((w) => w !== word);
			} else {
				state.expanded.push(word);
			}
		},
		/**
		 * Forgets the conversation without forgetting the room. The screen
		 * calls this when it is entered and left: chat survives reset() so a
		 * game starting cannot wipe it, which means something else has to
		 * clear it when the player moves between rooms.
		 */
		clearChat() {
			state.chat = [];
			state.chatCount = 0;
		}
	};
}

/** The store the routes share. */
export const game = createGameStore();
