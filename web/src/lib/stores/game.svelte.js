import { rejectMessage, errorMessage } from '$lib/i18n/vi.js';

/**
 * How many messages the panel holds. The same window the server keeps, so the
 * two can never disagree about what the conversation is.
 */
export const CHAT_WINDOW = 20;

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
 * @property {string} playerId - the seat that played it, empty for the opening
 * @property {number} points
 * @property {number} syllables
 * @property {boolean} opening - the seed word, played by neither side
 *
 * @typedef {object} PlayerSlot
 * @property {string} playerId
 * @property {string} name
 * @property {boolean} isMe
 * @property {boolean} isOwner
 * @property {boolean} ready
 * @property {boolean} connected
 * @property {number} wins - games won since the room opened
 *
 * @typedef {object} PlayerScore
 * @property {string} playerId
 * @property {string} name
 * @property {boolean} isMe
 * @property {number} score
 * @property {boolean} eliminated
 * @property {boolean} connected
 * @property {number} rank - final placing, 1 for the winner; 0 while in play
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
		chainLength: 0,

		nickname: '',
		roomCode: '',

		/**
		 * The room, exactly as the server last described it. Every field is
		 * server-owned: the client never decides who is seated, who owns the
		 * room, who is ready, or whether a game may start.
		 *
		 * The recipient's own row is in `roomPlayers` like everybody else's,
		 * marked `isMe`, which is what the derived accessors below read.
		 *
		 * @type {PlayerSlot[]}
		 */
		roomPlayers: [],
		canStart: false,
		/**
		 * How many seats the room has and how many a game needs. Sent by the
		 * server rather than compiled in here, so widening a room is a server
		 * change alone.
		 */
		maxPlayers: 0,
		minPlayers: 0,
		/** How long a seat is held for somebody who dropped. */
		graceMs: 0,

		/**
		 * The table of a running game, in turn order, and who is on turn.
		 *
		 * @type {PlayerScore[]}
		 */
		gamePlayers: [],
		turnPlayerId: '',
		/**
		 * The final table, best first: the player left standing, then the rest
		 * in reverse order of elimination.
		 *
		 * @type {PlayerScore[]}
		 */
		standings: [],

		/**
		 * This player's own knockout, and nobody else's. `suggestions` is what
		 * the position still had when they lost it; empty means it was a dead
		 * end, which is a different thing to say than "here is what you
		 * missed".
		 *
		 * @type {{ playerId: string, name: string, reason: number, suggestions: string[] } | null}
		 */
		elimination: null,
		/**
		 * The last player to go out, whoever they were. It is what a spectator
		 * is shown; the client's own knockout is `elimination` above.
		 *
		 * @type {{ playerId: string, name: string, isMe: boolean, reason: number } | null}
		 */
		lastOut: null,

		/**
		 * The room's conversation, oldest first, capped at CHAT_WINDOW. Chat
		 * belongs to the room rather than to a game, so it survives reset();
		 * leaving the room is what clears it.
		 *
		 * @type {{ fromMe: boolean, playerId: string, author: string, text: string, atMs: number }[]}
		 */
		chat: [],
		/**
		 * How many messages this connection has been told about. The list above
		 * is capped, so its length stops rising and cannot be used to work out
		 * what a folded panel has not shown yet.
		 *
		 * A replayed history sets it to what that history holds rather than to
		 * zero: a resync is not a reason to forget that three of those lines
		 * arrived while the reader was looking away.
		 */
		chatCount: 0,

		/** @type {{ word: string, message: string } | null} */
		rejection: null,
		/**
		 * The finished game, from this player's side. The table it came with
		 * is `standings`; this is the part about them.
		 *
		 * @type {{ iWon: boolean, reason: number, myScore: number, chainLength: number } | null}
		 */
		result: null,
		/** @type {string | null} */
		error: null
	};
}

/**
 * Reads one PlayerScore off the wire.
 *
 * @param {any} p
 * @returns {PlayerScore}
 */
function toScore(p) {
	return {
		playerId: p.playerId,
		name: p.name,
		isMe: p.isMe,
		score: p.score,
		eliminated: p.eliminated,
		connected: p.connected,
		rank: p.rank
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
	 * the room. A game ending, or a new one starting, does not change which
	 * room this is or who is in it — the server says so with its own message.
	 */
	const kept = new Set([
		'nickname',
		'roomCode',
		'roomPlayers',
		'canStart',
		'maxPlayers',
		'minPlayers',
		'graceMs',
		'chat',
		'chatCount'
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
				state.canStart = value.canStart;
				state.maxPlayers = value.maxPlayers;
				state.minPlayers = value.minPlayers;
				state.graceMs = value.graceMs;
				state.roomPlayers = value.players.map((/** @type {any} */ p) => ({
					playerId: p.playerId,
					name: p.name,
					isMe: p.isMe,
					isOwner: p.isOwner,
					ready: p.ready,
					connected: p.connected,
					wins: p.wins
				}));
				// The lobby is where a room sits when no game is on. `over`
				// keeps its result panel, which the lobby appears beneath.
				if (state.phase === 'idle') state.phase = 'lobby';
				break;

			case 'gameStarted':
				// reset() clears the readiness that led here, along with the
				// last game's board and its knockouts.
				reset();
				state.phase = 'playing';
				state.chain = [
					{
						word: value.openingWord,
						typed: '',
						byMe: false,
						playerId: '',
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
				state.gamePlayers = value.players.map(toScore);
				state.turnPlayerId = value.turnPlayerId;
				break;

			case 'turnUpdate': {
				const played = value.played;
				// A turn update with no word is an elimination moving the turn
				// on: the syllable and the chain survive the player who could
				// not answer them, so there is nothing to append.
				if (played) {
					state.chain.push({
						word: played.word,
						typed: played.typed,
						byMe: played.byMe,
						playerId: played.playerId,
						points: played.points,
						syllables: played.syllables,
						opening: false
					});
				}
				state.currentSyllable = value.currentSyllable;
				state.myTurn = value.myTurn;
				state.deadlineMs = Number(value.deadlineUnixMs);
				state.turnSeq = value.turnSeq;
				state.chainLength = value.chainLength;
				state.gamePlayers = value.players.map(toScore);
				state.turnPlayerId = value.turnPlayerId;
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

			case 'playerEliminated':
				state.lastOut = {
					playerId: value.playerId,
					name: value.name,
					isMe: value.isMe,
					reason: value.reason
				};
				// Only the player who went out is sent suggestions, and only
				// they have a use for them: they describe the position that
				// beat them, which is nobody else's position.
				if (value.isMe) {
					state.myTurn = false;
					state.elimination = {
						playerId: value.playerId,
						name: value.name,
						reason: value.reason,
						suggestions: value.suggestions ?? []
					};
				}
				break;

			case 'gameOver': {
				state.phase = 'over';
				state.myTurn = false;
				state.standings = value.standings.map(toScore);
				const mine = state.standings.find((/** @type {PlayerScore} */ p) => p.isMe);
				state.result = {
					iWon: value.iWon,
					reason: value.reason,
					myScore: mine?.score ?? 0,
					chainLength: value.chainLength
				};
				break;
			}

			case 'chatMessage':
				state.chat.push({
					fromMe: value.fromMe,
					playerId: value.playerId,
					author: value.author,
					text: value.text,
					// int64 on the wire, which the runtime hands over as a
					// bigint. Nothing downstream expects one.
					atMs: Number(value.sentUnixMs)
				});
				state.chatCount++;
				// Trimmed to the server's window, so a long conversation and a
				// replayed one are the same list.
				if (state.chat.length > CHAT_WINDOW) {
					state.chat = state.chat.slice(-CHAT_WINDOW);
				}
				break;

			case 'chatHistory':
				// A snapshot replaces; it never merges. It is also what a
				// client arriving in a new room is given, so a conversation
				// cannot outlive the room it was had in.
				state.chat = value.messages.map((/** @type {any} */ m) => ({
					fromMe: m.fromMe,
					playerId: m.playerId,
					author: m.author,
					text: m.text,
					atMs: Number(m.sentUnixMs)
				}));
				state.chatCount = state.chat.length;
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

			default:
				// A message this build does not know. Silence would make the
				// next contract addition look like a network problem, so say
				// so once rather than dropping it invisibly.
				console.warn('unhandled server message', kind);
				break;
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
		 *
		 * @returns {PlayerSlot | null}
		 */
		get me() {
			return state.roomPlayers.find((/** @type {PlayerSlot} */ p) => p.isMe) ?? null;
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
			return table.find((/** @type {PlayerScore} */ p) => p.isMe)?.score ?? 0;
		},
		/**
		 * How many games a seat has won since the room opened. Read off the
		 * room rather than the game: the tally spans games, and the table of
		 * the one on screen cannot carry it.
		 *
		 * @param {string} playerId
		 * @returns {number}
		 */
		winsOf(playerId) {
			return (
				state.roomPlayers.find((/** @type {PlayerSlot} */ p) => p.playerId === playerId)?.wins ?? 0
			);
		},
		/**
		 * Which seat a player is in, 1-based, or 0 for nobody. It is what the
		 * chat log colours a line by — the server says which seat spoke, so
		 * the client never has to match display names.
		 *
		 * @param {string} playerId
		 * @returns {number}
		 */
		seatIndexOf(playerId) {
			if (!playerId) return 0;
			const at = state.roomPlayers.findIndex(
				(/** @type {PlayerSlot} */ p) => p.playerId === playerId
			);
			return at < 0 ? 0 : at + 1;
		},
		/**
		 * Everybody whose reconnect window is currently running. The lobby and
		 * the board both wait on the same list.
		 *
		 * @returns {PlayerSlot[]}
		 */
		get awayPlayers() {
			return state.roomPlayers.filter((/** @type {PlayerSlot} */ p) => !p.isMe && !p.connected);
		},
		/**
		 * The name behind a seat id, for the chain and the board. Falls back to
		 * the id's absence rather than inventing a label: an empty string is
		 * something a caller can substitute its own copy for.
		 *
		 * @param {string} playerId
		 * @returns {string}
		 */
		nameOf(playerId) {
			const from = state.gamePlayers.length ? state.gamePlayers : state.standings;
			return (
				from.find((/** @type {PlayerScore} */ p) => p.playerId === playerId)?.name ??
				state.roomPlayers.find((/** @type {PlayerSlot} */ p) => p.playerId === playerId)?.name ??
				''
			);
		},

		clearRejection() {
			state.rejection = null;
		},
		clearError() {
			state.error = null;
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
