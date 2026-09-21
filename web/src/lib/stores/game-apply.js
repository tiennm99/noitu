import { rejectMessage, errorMessage, fill, t } from '$lib/i18n/vi.js';
import { toParts, toSenses, toScore, toSlot } from './game-shape.js';

/**
 * @typedef {import('$lib/proto/noitu/v1/game_pb.js').ServerMessage} ServerMessage
 * @typedef {import('./game-shape.js').GameState} GameState
 */

// Ordinal handed to each chat line as it arrives, for list keys. Never reset:
// a replayed history must not reuse numbers a line still on screen holds.
let chatOrdinal = 0;

/**
 * How many messages the panel holds. The same window the server keeps, so the
 * two can never disagree about what the conversation is.
 */
export const CHAT_WINDOW = 20;

/**
 * Applies one ServerMessage to a GameState in place.
 *
 * A pure function over a plain object — `state` need not be a Svelte proxy —
 * which is what lets this run under plain Vitest and, wrapped by the store,
 * under `$state` in the browser. `reset` and `leave` are handed in rather
 * than imported, so this module never has to know how the store returns to
 * its pre-game shape.
 * @param {GameState} state
 * @param {ServerMessage} msg
 * @param {{ reset: () => void, leave: () => void }} lifecycle
 */
export function applyTo(state, msg, { reset, leave }) {
	const payload = msg.payload;

	switch (payload.case) {
		case 'welcome':
			// The server sanitizes the requested name, so what it returns
			// is the only name safe to display — never the raw input.
			state.nickname = payload.value.acceptedNickname;
			break;

		case 'roomState': {
			const value = payload.value;
			// A room existing is proof the wait is over, whether or not a
			// quickMatchStatus already said so.
			state.queued = false;
			// One snapshot, applied wholesale. Merging fields selectively
			// is how a client ends up believing a mixture of two states
			// the server was never in.
			state.roomCode = value.roomCode;
			state.canStart = value.canStart;
			state.maxPlayers = value.maxPlayers;
			state.minPlayers = value.minPlayers;
			state.graceMs = value.graceMs;
			state.roomPlayers = value.players.map(toSlot);
			// The lobby is where a room sits when no game is on. `over`
			// keeps its result panel, which the lobby appears beneath.
			if (state.phase === 'idle') state.phase = 'lobby';
			break;
		}

		case 'gameStarted': {
			const value = payload.value;
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
					opening: true,
					meanings: toSenses(value.openingMeanings),
					parts: []
				}
			];
			// The opening word is the newest word there is.
			state.expanded = [value.openingWord];
			state.currentSyllable = value.currentSyllable;
			state.myTurn = value.myTurn;
			state.deadlineMs = Number(value.deadlineUnixMs);
			state.turnSeq = value.turnSeq;
			state.turnLimitMs = value.turnLimitMs;
			state.chainLength = 1;
			state.gamePlayers = value.players.map(toScore);
			state.turnPlayerId = value.turnPlayerId;
			break;
		}

		case 'turnUpdate': {
			const value = payload.value;
			const played = value.played;
			// A turn update with no word is an elimination moving the turn
			// on: the syllable and the chain survive the player who could
			// not answer them, so there is nothing to append.
			if (played) {
				// The newest word takes over the open panel from the one
				// before it. Words the player opened by hand stay open.
				const previous = state.chain[state.chain.length - 1]?.word;
				state.chain.push({
					word: played.word,
					typed: played.typed,
					byMe: played.byMe,
					playerId: played.playerId,
					points: played.points,
					syllables: played.syllables,
					opening: false,
					meanings: toSenses(played.meanings),
					parts: toParts(played.parts)
				});
				state.expanded = state.expanded.filter((w) => w !== previous);
				if (!state.expanded.includes(played.word)) state.expanded.push(played.word);
			}
			state.currentSyllable = value.currentSyllable;
			state.myTurn = value.myTurn;
			state.deadlineMs = Number(value.deadlineUnixMs);
			state.turnSeq = value.turnSeq;
			state.chainLength = value.chainLength;
			state.gamePlayers = value.players.map(toScore);
			state.turnPlayerId = value.turnPlayerId;
			// An accepted move answers the previous rejection — and only
			// an accepted move does. A wordless update is somebody being
			// eliminated, which says nothing about the word this player
			// was just refused, and wiping the reason off their screen is
			// one player's exit costing another the only explanation they
			// had.
			if (played) {
				state.rejection = null;
				state.reportConfirmation = null;
			}
			// A false dead-end claim is about the position this update
			// just moved past, however the turn moved.
			state.claimError = null;
			break;
		}

		case 'moveRejected':
			state.rejection = {
				word: payload.value.word,
				message: rejectMessage(payload.value.reason, state.currentSyllable),
				reason: payload.value.reason,
				suggestion: payload.value.suggestion ?? ''
			};
			// A new rejection has nothing reported against it yet.
			state.reportConfirmation = null;
			break;

		case 'wordReported':
			state.reportConfirmation = fill(t.wordReported, { word: payload.value.word });
			break;

		case 'playerEliminated': {
			const value = payload.value;
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
		}

		case 'gameOver': {
			const value = payload.value;
			state.phase = 'over';
			state.myTurn = false;
			state.standings = value.standings.map(toScore);
			const mine = state.standings.find((p) => p.isMe);
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
				n: ++chatOrdinal,
				fromMe: payload.value.fromMe,
				playerId: payload.value.playerId,
				author: payload.value.author,
				text: payload.value.text,
				// int64 on the wire, which the runtime hands over as a
				// bigint. Nothing downstream expects one.
				atMs: Number(payload.value.sentUnixMs)
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
			state.chat = payload.value.messages.map((m) => ({
				n: ++chatOrdinal,
				fromMe: m.fromMe,
				playerId: m.playerId,
				author: m.author,
				text: m.text,
				atMs: Number(m.sentUnixMs)
			}));
			state.chatCount = state.chat.length;
			break;

		case 'error': {
			const value = payload.value;
			// Two of them also end this player's membership of the room, so
			// the model has to stop describing one. Set after, because
			// leaving clears everything including the message.
			if (value.code === 'kicked' || value.code === 'room_idle_closed') leave();
			// A false dead-end claim is answered next to the input, not in
			// the top banner: it is about the move just attempted, not a
			// room-wide condition every screen has to show.
			if (value.code === 'not_a_dead_end') {
				state.claimError = errorMessage(value.code);
				break;
			}
			// A match the server could not open leaves nobody queued, and
			// the only frame that says so is this refusal.
			if (
				value.code === 'server_full' ||
				value.code === 'room_start_failed' ||
				value.code === 'server_restarting'
			) {
				state.queued = false;
			}
			state.error = errorMessage(value.code);
			break;
		}

		case 'quickMatchStatus':
			state.queued = payload.value.queued;
			break;

		case 'pong':
			// Handled by the transport, which owns the clock offset.
			break;

		default:
			// A message this build does not know. Silence would make the
			// next contract addition look like a network problem, so say
			// so once rather than dropping it invisibly.
			console.warn('unhandled server message', payload.case);
			break;
	}
}
