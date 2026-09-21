/**
 * The game model's shape: what a `GameState` looks like, the value it starts
 * at, and the pure decoders that turn a wire message's repeated fields into
 * it. Nothing here is reactive — `$state` is applied once, by the store that
 * owns this shape — which is what lets `initialState()` and the decoders run
 * under plain Vitest with no Svelte runtime involved.
 * @typedef {import('$lib/proto/noitu/v1/game_pb.js').Sense} WireSense
 * @typedef {import('$lib/proto/noitu/v1/game_pb.js').PointPart} WirePointPart
 * @typedef {import('$lib/proto/noitu/v1/game_pb.js').PlayerSlot} WirePlayerSlot
 * @typedef {import('$lib/proto/noitu/v1/game_pb.js').PlayerScore} WirePlayerScore
 */

/**
 * The game model is a projection of what the server sent. The client never
 * decides whether a word is valid, whose turn it is, or who won — it renders
 * the last message it received. That is what makes the bot and the online
 * modes the same screen.
 * @typedef {object} ChainEntry
 * @property {string} word - the canonical spelling
 * @property {string} typed - what the player actually typed, when it differed
 * @property {boolean} byMe
 * @property {string} playerId - the seat that played it, empty for the opening
 * @property {number} points
 * @property {number} syllables
 * @property {boolean} opening - the seed word, played by neither side
 * @property {Sense[]} meanings - what the word means, at most five; empty when
 *   the dictionary has none
 * @property {PointPart[]} parts - how points was arrived at, one entry per
 *   non-zero term, summing to points; empty for the opening word
 */

/**
 * @typedef {object} Sense
 * @property {string} pos - Vietnamese part-of-speech label, empty when unknown
 * @property {string} gloss - the definition, plain text
 */

/**
 * @typedef {object} PointPart
 * @property {number} kind - a PointKind enum value
 * @property {number} value
 */

/**
 * @typedef {object} PlayerSlot
 * @property {string} playerId
 * @property {string} name
 * @property {boolean} isMe
 * @property {boolean} isOwner
 * @property {boolean} ready
 * @property {boolean} connected
 * @property {number} wins - games won since the room opened
 */

/**
 * @typedef {object} PlayerScore
 * @property {string} playerId
 * @property {string} name
 * @property {boolean} isMe
 * @property {number} score
 * @property {boolean} eliminated
 * @property {boolean} connected
 * @property {number} rank - final placing, 1 for the winner; 0 while in play
 */

/** @typedef {{ playerId: string, name: string, reason: number, suggestions: string[] }} Elimination */
/** @typedef {{ playerId: string, name: string, isMe: boolean, reason: number }} LastOut */
/** @typedef {{ n: number, fromMe: boolean, playerId: string, author: string, text: string, atMs: number }} ChatLine */
/** @typedef {{ word: string, message: string, reason: number, suggestion: string }} Rejection */
/** @typedef {{ iWon: boolean, reason: number, myScore: number, chainLength: number }} GameResult */

/**
 * Everything the screens read. One object, one source of truth: the store's
 * own comment on `reset()` states the failure mode a slice design invites —
 * merging fields selectively is how a client ends up believing a mixture of
 * two states the server was never in — which is why this stays one typedef
 * even though it is split across files.
 * @typedef {object} GameState
 * @property {'idle' | 'lobby' | 'playing' | 'over'} phase - Where the screen
 *   is. `lobby` is online-only: the room exists and its code can be shared,
 *   and it is where every game is agreed before it starts and returned to
 *   after it ends. RoomState deliberately does not move this — only
 *   GameStarted and GameOver do.
 * @property {ChainEntry[]} chain
 * @property {string[]} expanded - Words in the chain whose meaning is open.
 *   Client-only state, like the theme: the newest word opens on arrival and
 *   closes the one before it, and a click toggles any word.
 * @property {string} currentSyllable
 * @property {boolean} myTurn
 * @property {number} deadlineMs
 * @property {number} turnSeq
 * @property {number} turnLimitMs
 * @property {number} chainLength
 * @property {string} nickname
 * @property {string} roomCode
 * @property {boolean} queued - Waiting in the quick-match queue for the next
 *   stranger who also asked. Ends on its own once a RoomState seats this
 *   connection somewhere.
 * @property {PlayerSlot[]} roomPlayers - The room, exactly as the server last
 *   described it. Every field is server-owned. The recipient's own row is
 *   marked `isMe`, which is what the derived accessors below read.
 * @property {boolean} canStart
 * @property {number} maxPlayers
 * @property {number} minPlayers
 * @property {number} graceMs - How long a dropped seat is held for a reconnect.
 * @property {PlayerScore[]} gamePlayers - The table of a running game, in
 *   turn order, and who is on turn.
 * @property {string} turnPlayerId
 * @property {PlayerScore[]} standings - The final table, best first: the
 *   player left standing, then the rest in reverse order of elimination.
 * @property {Elimination | null} elimination - This player's own knockout,
 *   and nobody else's. Empty `suggestions` means it was a dead end, which is
 *   a different thing to say than "here is what you missed".
 * @property {LastOut | null} lastOut - The last player to go out, whoever
 *   they were: what a spectator is shown. The client's own knockout is
 *   `elimination` above.
 * @property {ChatLine[]} chat - The room's conversation, oldest first, capped
 *   at CHAT_WINDOW. Survives `reset()`; leaving the room is what clears it.
 * @property {number} chatCount - How many messages this connection has been
 *   told about. `chat` is capped, so its length cannot say what a folded
 *   panel has not shown yet.
 * @property {Rejection | null} rejection - `reason` is kept alongside the
 *   rendered message so the UI can decide whether reporting the word applies
 *   without re-deriving it from the text. `suggestion` is the one real word
 *   the input differs from by diacritics alone, empty when none applies.
 * @property {string | null} claimError - A dead-end claim the server refused
 *   because a move still existed.
 * @property {string | null} reportConfirmation - The confirmation text for
 *   the last word this session reported, once the server has acknowledged it.
 * @property {GameResult | null} result - The finished game, from this
 *   player's side. The table it came with is `standings`.
 * @property {string | null} error
 */

/** @returns {GameState} */
export function initialState() {
	return {
		phase: 'idle',
		chain: [],
		expanded: [],
		currentSyllable: '',
		myTurn: false,
		deadlineMs: 0,
		turnSeq: 0,
		turnLimitMs: 0,
		chainLength: 0,

		nickname: '',
		roomCode: '',

		queued: false,

		roomPlayers: [],
		canStart: false,
		maxPlayers: 0,
		minPlayers: 0,
		graceMs: 0,

		gamePlayers: [],
		turnPlayerId: '',
		standings: [],

		elimination: null,
		lastOut: null,

		chat: [],
		chatCount: 0,

		rejection: null,
		claimError: null,
		reportConfirmation: null,
		result: null,
		error: null
	};
}

/**
 * Reads a word's senses off the wire.
 * @param {WireSense[] | undefined} senses
 * @returns {Sense[]}
 */
export function toSenses(senses) {
	return (senses ?? []).map((s) => ({ pos: s.pos, gloss: s.gloss }));
}

/**
 * Reads a move's score breakdown off the wire.
 * @param {WirePointPart[] | undefined} parts
 * @returns {PointPart[]}
 */
export function toParts(parts) {
	return (parts ?? []).map((p) => ({ kind: p.kind, value: p.value }));
}

/**
 * Reads one PlayerScore off the wire.
 * @param {WirePlayerScore} p
 * @returns {PlayerScore}
 */
export function toScore(p) {
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
 * Reads one seat off the wire.
 * @param {WirePlayerSlot} p
 * @returns {PlayerSlot}
 */
export function toSlot(p) {
	return {
		playerId: p.playerId,
		name: p.name,
		isMe: p.isMe,
		isOwner: p.isOwner,
		ready: p.ready,
		connected: p.connected,
		wins: p.wins
	};
}
