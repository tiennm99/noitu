// The store is a projection of server messages, so these tests feed it real
// decoded messages and assert the rendered model — not the reducer's internals.

import { describe, expect, it } from 'vitest';
import { create } from '@bufbuild/protobuf';
import {
	GameEndReason,
	RejectReason,
	ServerMessageSchema
} from '../src/lib/proto/noitu/v1/game_pb.js';
import { CHAT_WINDOW, createGameStore } from '../src/lib/stores/game.svelte.js';

/**
 * @param {string} kind
 * @param {object} value
 */
function msg(kind, value) {
	return create(ServerMessageSchema, { payload: { case: kind, value } });
}

/** The two players every in-game fixture below is scored for. */
function table(mine = 0, theirs = 0) {
	return [
		{ playerId: 'p1', name: 'Minh', isMe: true, score: mine, connected: true },
		{ playerId: 'p2', name: 'Lan', isMe: false, score: theirs, connected: true }
	];
}

function started(overrides = {}) {
	return msg('gameStarted', {
		openingWord: 'học sinh',
		currentSyllable: 'sinh',
		myTurn: true,
		deadlineUnixMs: 1_700_000_020_000n,
		turnSeq: 1,
		turnLimitMs: 20_000,
		players: table(),
		turnPlayerId: 'p1',
		...overrides
	});
}

describe('welcome', () => {
	it('displays the name the server accepted, not the one requested', () => {
		const store = createGameStore();
		store.apply(msg('welcome', { acceptedNickname: 'Minh', sessionId: 's1', resumeToken: 'r1' }));
		expect(store.state.nickname).toBe('Minh');
	});
});

describe('gameStarted', () => {
	it('seeds the chain with the opening word', () => {
		const store = createGameStore();
		store.apply(started());

		expect(store.state.phase).toBe('playing');
		expect(store.state.chain).toHaveLength(1);
		expect(store.state.chain[0]).toMatchObject({ word: 'học sinh', opening: true, byMe: false });
		expect(store.state.currentSyllable).toBe('sinh');
		expect(store.state.myTurn).toBe(true);
		expect(store.state.turnLimitMs).toBe(20_000);
	});

	it('counts the opening word in the chain, matching the server', () => {
		const store = createGameStore();
		store.apply(started());
		expect(store.state.chainLength).toBe(1);
	});

	it('converts the int64 deadline out of bigint so the countdown can do arithmetic', () => {
		const store = createGameStore();
		store.apply(started());
		expect(store.state.deadlineMs).toBe(1_700_000_020_000);
		expect(typeof store.state.deadlineMs).toBe('number');
	});

	it('clears a previous result so a rematch does not start on the old screen', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(msg('gameOver', { iWon: false, reason: GameEndReason.TIMEOUT }));
		expect(store.state.phase).toBe('over');

		store.apply(started());
		expect(store.state.phase).toBe('playing');
		expect(store.state.result).toBeNull();
	});
});

describe('turnUpdate', () => {
	it('appends the played word and adopts the server scores', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(
			msg('turnUpdate', {
				played: {
					word: 'sinh viên',
					byMe: true,
					points: 12,
					syllables: 2,
					typed: 'sinh vien',
					playerId: 'p1'
				},
				currentSyllable: 'viên',
				myTurn: false,
				deadlineUnixMs: 1_700_000_040_000n,
				turnSeq: 2,
				chainLength: 2,
				players: table(12, 0),
				turnPlayerId: 'p2'
			})
		);

		expect(store.state.chain).toHaveLength(2);
		expect(store.state.chain[1]).toMatchObject({
			word: 'sinh viên',
			typed: 'sinh vien',
			byMe: true,
			points: 12,
			syllables: 2
		});
		expect(store.state.currentSyllable).toBe('viên');
		expect(store.state.myTurn).toBe(false);
		expect(store.myScore).toBe(12);
		expect(store.state.turnPlayerId).toBe('p2');
		expect(store.state.chainLength).toBe(2);
		expect(store.state.turnSeq).toBe(2);
	});

	it('moves the turn on without a word when an elimination did it', () => {
		// The syllable and the chain survive the player who could not answer
		// them, so there is nothing to append — but the deadline and the player
		// to act are both new.
		const store = createGameStore();
		store.apply(started());
		store.apply(
			msg('turnUpdate', {
				currentSyllable: 'sinh',
				myTurn: true,
				deadlineUnixMs: 1_700_000_050_000n,
				turnSeq: 3,
				chainLength: 1,
				players: [
					{ playerId: 'p1', name: 'Minh', isMe: true, score: 0, connected: true },
					{ playerId: 'p2', name: 'Lan', score: 0, eliminated: true, connected: true }
				],
				turnPlayerId: 'p1'
			})
		);

		expect(store.state.chain).toHaveLength(1);
		expect(store.state.turnSeq).toBe(3);
		expect(store.state.deadlineMs).toBe(1_700_000_050_000);
		expect(store.state.gamePlayers[1].eliminated).toBe(true);
	});

	it('clears the standing rejection, because an accepted move answers it', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(msg('moveRejected', { reason: RejectReason.NOT_IN_DICTIONARY, word: 'xyz' }));
		expect(store.state.rejection).not.toBeNull();

		store.apply(
			msg('turnUpdate', {
				played: { word: 'sinh viên', byMe: true, points: 1, syllables: 2, typed: '' },
				currentSyllable: 'viên',
				myTurn: false,
				turnSeq: 2,
				chainLength: 2
			})
		);
		expect(store.state.rejection).toBeNull();
	});
});

describe('moveRejected', () => {
	it('renders the specific Vietnamese reason', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(msg('moveRejected', { reason: RejectReason.ALREADY_USED, word: 'học sinh' }));

		expect(store.state.rejection).toEqual({
			word: 'học sinh',
			message: 'Từ này đã được dùng rồi.'
		});
	});

	it('names the required syllable in the wrong-link message', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(msg('moveRejected', { reason: RejectReason.WRONG_LINK, word: 'bàn ghế' }));

		expect(store.state.rejection?.message).toBe('Từ phải bắt đầu bằng tiếng “sinh”.');
	});
});

describe('gameOver', () => {
	it('records the result and takes the turn away', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(
			msg('gameOver', {
				iWon: true,
				reason: GameEndReason.NO_LEGAL_MOVE,
				chainLength: 7,
				standings: [
					{ playerId: 'p1', name: 'Minh', isMe: true, score: 42, connected: true, rank: 1 },
					{ playerId: 'p2', name: 'Lan', score: 8, eliminated: true, connected: true, rank: 2 }
				]
			})
		);

		expect(store.state.phase).toBe('over');
		expect(store.state.myTurn).toBe(false);
		expect(store.state.result).toEqual({
			iWon: true,
			reason: GameEndReason.NO_LEGAL_MOVE,
			myScore: 42,
			chainLength: 7
		});
	});

	it('keeps the final table in the order the server ranked it', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(
			msg('gameOver', {
				iWon: false,
				reason: GameEndReason.TIMEOUT,
				chainLength: 9,
				standings: [
					{ playerId: 'p3', name: 'Hà', score: 60, connected: true, rank: 1 },
					{ playerId: 'p1', name: 'Minh', isMe: true, score: 40, eliminated: true, rank: 2 },
					{ playerId: 'p2', name: 'Lan', score: 55, eliminated: true, rank: 3 }
				]
			})
		);

		expect(store.state.standings.map((p) => p.playerId)).toEqual(['p3', 'p1', 'p2']);
		// Rank is finishing order, not score order: Lan outscored Minh and
		// still placed below him, because he outlasted her.
		expect(store.state.standings[2].score).toBeGreaterThan(store.state.standings[1].score);
		expect(store.state.result?.myScore).toBe(40);
	});
});

describe('playerEliminated', () => {
	it('keeps the words this player could have played', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(
			msg('playerEliminated', {
				playerId: 'p1',
				name: 'Minh',
				isMe: true,
				reason: GameEndReason.TIMEOUT,
				suggestions: ['sinh viên', 'sinh sôi']
			})
		);

		expect(store.state.elimination?.suggestions).toEqual(['sinh viên', 'sinh sôi']);
		expect(store.iAmOut).toBe(true);
		expect(store.state.myTurn).toBe(false);
	});

	it('reads an absent list as a position that had nothing left', () => {
		// A dead end arrives as an empty list, which means "no words to offer"
		// rather than undefined.
		const store = createGameStore();
		store.apply(started());
		store.apply(
			msg('playerEliminated', { playerId: 'p1', isMe: true, reason: GameEndReason.NO_LEGAL_MOVE })
		);

		expect(store.state.elimination?.suggestions).toEqual([]);
	});

	it('records somebody else going out without claiming this player did', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(
			msg('playerEliminated', {
				playerId: 'p2',
				name: 'Lan',
				isMe: false,
				reason: GameEndReason.RESIGNED,
				suggestions: []
			})
		);

		expect(store.state.lastOut?.name).toBe('Lan');
		expect(store.state.elimination).toBeNull();
		expect(store.iAmOut).toBe(false);
	});
});

describe('error', () => {
	it('translates the UI key rather than showing it', () => {
		const store = createGameStore();
		store.apply(msg('error', { code: 'room_not_found', message: '' }));
		expect(store.state.error).toBe('Không tìm thấy phòng với mã này.');
	});

	it('falls back to a generic message for an unknown code', () => {
		const store = createGameStore();
		store.apply(msg('error', { code: 'something_new', message: '' }));
		expect(store.state.error).toBe('Đã có lỗi xảy ra. Hãy thử lại.');
		expect(store.state.error).not.toContain('something_new');
	});
});

/** One seat, with the fields a test does not care about defaulted. */
function seat(fields = {}) {
	return {
		playerId: 'p1',
		name: 'Minh',
		isMe: false,
		isOwner: false,
		ready: false,
		connected: true,
		wins: 0,
		...fields
	};
}

/** One lobby snapshot. `players` is the whole room, the recipient included. */
function lobby(fields = {}) {
	return msg('roomState', {
		roomCode: 'ABCD',
		canStart: false,
		maxPlayers: 4,
		minPlayers: 2,
		graceMs: 30_000,
		players: [seat({ isMe: true })],
		...fields
	});
}

/** A two-player room, seen by the guest. */
function pair(fields = {}) {
	return lobby({
		players: [
			seat({ playerId: 'p1', name: 'Chủ', isOwner: true }),
			seat({ playerId: 'p2', name: 'Lan', isMe: true })
		],
		...fields
	});
}

describe('room messages', () => {
	it('keeps the room code and the sanitized names', () => {
		const store = createGameStore();
		store.apply(pair());

		expect(store.state.roomCode).toBe('ABCD');
		expect(store.state.roomPlayers.map((p) => p.name)).toEqual(['Chủ', 'Lan']);
		expect(store.me?.playerId).toBe('p2');
	});

	it('survives a reset, because identity outlives one game', () => {
		const store = createGameStore();
		store.apply(msg('welcome', { acceptedNickname: 'Minh', sessionId: 's', resumeToken: 't' }));
		store.apply(pair());
		store.reset();

		expect(store.state.nickname).toBe('Minh');
		expect(store.state.roomCode).toBe('ABCD');
		expect(store.state.roomPlayers).toHaveLength(2);
		expect(store.state.chain).toEqual([]);
	});

	it('counts the seats nobody is in yet', () => {
		const store = createGameStore();
		store.apply(pair());
		expect(store.freeSeats).toBe(2);
	});

	it('reads the series score off the room, not off the game', () => {
		const store = createGameStore();
		store.apply(
			pair({
				players: [
					seat({ playerId: 'p1', name: 'Chủ', isOwner: true, wins: 2 }),
					seat({ playerId: 'p2', name: 'Lan', isMe: true, wins: 1 })
				]
			})
		);

		expect(store.winsOf('p1')).toBe(2);
		expect(store.winsOf('p2')).toBe(1);
		// A game starting clears the board and keeps the room, tally included.
		store.apply(started());
		expect(store.winsOf('p1')).toBe(2);
		// Nobody the room does not seat has a tally.
		expect(store.winsOf('bot')).toBe(0);
	});

	it('numbers the seats, which is what a chat line is coloured by', () => {
		const store = createGameStore();
		store.apply(pair());

		expect(store.seatIndexOf('p1')).toBe(1);
		expect(store.seatIndexOf('p2')).toBe(2);
		// A line whose seat the server cleared belongs to nobody.
		expect(store.seatIndexOf('')).toBe(0);
		expect(store.seatIndexOf('p9')).toBe(0);
	});
});

describe('pong', () => {
	it('changes nothing in the game model', () => {
		const store = createGameStore();
		store.apply(started());
		const before = JSON.stringify(store.state);
		store.apply(msg('pong', { clientTimeMs: 1n, serverTimeMs: 2n }));
		expect(JSON.stringify(store.state)).toBe(before);
	});
});

describe('chat', () => {
	/** @param {object} fields */
	function line(fields) {
		return msg('chatMessage', {
			fromMe: false,
			playerId: 'p2',
			author: 'Lan',
			text: 'chào',
			sentUnixMs: 1756998000123n,
			...fields
		});
	}

	it('appends in arrival order and converts the time out of bigint', () => {
		const store = createGameStore();
		store.apply(line({ text: 'một' }));
		store.apply(line({ text: 'hai', fromMe: true }));

		expect(store.state.chat.map((/** @type {any} */ m) => m.text)).toEqual(['một', 'hai']);
		expect(typeof store.state.chat[0].atMs).toBe('number');
		expect(store.state.chat[0].atMs).toBe(1756998000123);
	});

	it('keeps the seat a line came from, which is what colours it', () => {
		const store = createGameStore();
		store.apply(line({ text: 'của tôi', fromMe: true, playerId: 'p1' }));
		// An author the server cleared: the seat goes with the name.
		store.apply(line({ text: 'của ai', playerId: '', author: '' }));

		expect(store.state.chat.map((/** @type {any} */ m) => m.playerId)).toEqual(['p1', '']);
	});

	it('stops at the window the server keeps, so the two cannot disagree', () => {
		const store = createGameStore();
		for (let i = 0; i < CHAT_WINDOW + 5; i++) {
			store.apply(line({ text: `tin ${i}` }));
		}

		expect(store.state.chat).toHaveLength(CHAT_WINDOW);
		expect(store.state.chat[0].text).toBe('tin 5');
	});

	it('replaces the conversation wholesale on a history, never merging', () => {
		const store = createGameStore();
		store.apply(line({ text: 'cũ' }));
		const history = msg('chatHistory', {
			messages: [
				{ fromMe: true, author: 'Minh', text: 'a', sentUnixMs: 1n },
				{ fromMe: false, author: 'Lan', text: 'b', sentUnixMs: 2n }
			]
		});

		store.apply(history);
		store.apply(history);

		expect(store.state.chat.map((/** @type {any} */ m) => m.text)).toEqual(['a', 'b']);
	});

	it('survives a game starting: the conversation belongs to the room', () => {
		const store = createGameStore();
		store.apply(line({ text: 'trước ván' }));
		store.apply(started());

		expect(store.state.chat).toHaveLength(1);
	});

	it('counts arrivals past the window, which the list length cannot', () => {
		// The badge is built on this: once the list is capped its length stops
		// rising, so anything derived from it stops counting.
		const store = createGameStore();
		for (let i = 0; i < CHAT_WINDOW + 5; i++) {
			store.apply(line({ text: `tin ${i}` }));
		}

		expect(store.state.chat).toHaveLength(CHAT_WINDOW);
		expect(store.state.chatCount).toBe(CHAT_WINDOW + 5);
	});

	it('counts a resynchronising history as what it holds, not as nothing', () => {
		// A reconnect or an opponent leaving replaces the panel. Zeroing the
		// counter there would leave a folded panel's "seen" mark stranded
		// above it, and the badge would go quiet for as many messages as had
		// been read before — which is a bug two reviewers found by reading.
		const store = createGameStore();
		store.apply(line({}));
		store.apply(line({}));
		expect(store.state.chatCount).toBe(2);

		store.apply(
			msg('chatHistory', {
				messages: [
					{ fromMe: true, author: 'Minh', text: 'a', sentUnixMs: 1n },
					{ fromMe: false, author: 'Lan', text: 'b', sentUnixMs: 2n }
				]
			})
		);
		expect(store.state.chatCount).toBe(2);

		store.apply(msg('chatHistory', { messages: [] }));
		expect(store.state.chatCount).toBe(0);
	});

	it('is dropped when this player is no longer in the room', () => {
		const store = createGameStore();
		store.apply(line({}));

		store.clearChat();
		expect(store.state.chat).toEqual([]);

		store.apply(line({}));
		store.leave();
		expect(store.state.chat).toEqual([]);
		expect(store.state.chatCount).toBe(0);
	});
});

describe('the lobby', () => {
	it('opens the lobby when the room appears', () => {
		const store = createGameStore();
		store.apply(lobby({ roomCode: 'K7M2QP', players: [seat({ isMe: true, isOwner: true })] }));

		expect(store.state.phase).toBe('lobby');
		expect(store.state.roomCode).toBe('K7M2QP');
		expect(store.isOwner).toBe(true);
	});

	it("reads this player’s role and readiness off their own row", () => {
		// There is one encoding of each, so a client cannot end up believing a
		// role the list it is rendering disagrees with.
		const store = createGameStore();
		store.apply(
			pair({
				canStart: true,
				players: [
					seat({ playerId: 'p1', name: 'Chủ', isOwner: true }),
					seat({ playerId: 'p2', name: 'Lan', isMe: true, ready: true })
				]
			})
		);

		expect(store.isOwner).toBe(false);
		expect(store.isReady).toBe(true);
		expect(store.state.canStart).toBe(true);
		expect(store.state.maxPlayers).toBe(4);
		expect(store.state.minPlayers).toBe(2);
	});

	it('lists everybody whose reconnect window is running, and nobody else', () => {
		const store = createGameStore();
		store.apply(
			lobby({
				players: [
					seat({ playerId: 'p1', name: 'Chủ', isOwner: true }),
					seat({ playerId: 'p2', name: 'Lan', isMe: true }),
					seat({ playerId: 'p3', name: 'Hà', connected: false })
				]
			})
		);

		expect(store.awayPlayers.map((p) => p.name)).toEqual(['Hà']);
		expect(store.state.graceMs).toBe(30_000);
	});

	it('does not drop a live game back into the lobby', () => {
		// A room state arriving mid-game is a presence change, not a phase.
		// Acting on it would replace the board with the lobby mid-turn.
		const store = createGameStore();
		store.apply(started());
		store.apply(pair());

		expect(store.state.phase).toBe('playing');
	});

	it('keeps the result on screen when a finished game returns to the lobby', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(msg('gameOver', { iWon: false, reason: GameEndReason.RESIGNED }));
		store.apply(pair());

		expect(store.state.phase).toBe('over');
		expect(store.state.result).not.toBeNull();
	});

	it('stops waiting for a player who is connected again', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(
			pair({
				players: [
					seat({ playerId: 'p1', name: 'Chủ', isOwner: true, connected: false }),
					seat({ playerId: 'p2', name: 'Lan', isMe: true })
				]
			})
		);
		expect(store.awayPlayers).toHaveLength(1);

		store.apply(pair());
		expect(store.awayPlayers).toHaveLength(0);
	});

	it('forgets the room when this player is kicked out of it', () => {
		const store = createGameStore();
		store.apply(pair());
		store.apply(msg('error', { code: 'kicked', message: '' }));

		expect(store.state.phase).toBe('idle');
		expect(store.state.roomCode).toBe('');
		expect(store.state.error).toBe('Bạn đã bị mời ra khỏi phòng.');
	});

	it('forgets a room that closed for sitting idle', () => {
		const store = createGameStore();
		store.apply(lobby());
		store.apply(msg('error', { code: 'room_idle_closed', message: '' }));

		expect(store.state.phase).toBe('idle');
		expect(store.state.roomCode).toBe('');
		expect(store.state.error).not.toBeNull();
	});
});
