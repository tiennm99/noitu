// The store is a projection of server messages, so these tests feed it real
// decoded messages and assert the rendered model — not the reducer's internals.

import { describe, expect, it } from 'vitest';
import { create } from '@bufbuild/protobuf';
import {
	GameEndReason,
	RejectReason,
	ServerMessageSchema
} from '../src/lib/proto/noitu/v1/game_pb.js';
import { createGameStore } from '../src/lib/stores/game.svelte.js';

/**
 * @param {string} kind
 * @param {object} value
 */
function msg(kind, value) {
	return create(ServerMessageSchema, { payload: { case: kind, value } });
}

function started(overrides = {}) {
	return msg('gameStarted', {
		openingWord: 'học sinh',
		currentSyllable: 'sinh',
		myTurn: true,
		deadlineUnixMs: 1_700_000_020_000n,
		turnSeq: 1,
		turnLimitMs: 20_000,
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
				played: { word: 'sinh viên', byMe: true, points: 12, syllables: 2, typed: 'sinh vien' },
				currentSyllable: 'viên',
				myTurn: false,
				deadlineUnixMs: 1_700_000_040_000n,
				turnSeq: 2,
				myScore: 12,
				opponentScore: 0,
				chainLength: 2
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
		expect(store.state.myScore).toBe(12);
		expect(store.state.chainLength).toBe(2);
		expect(store.state.turnSeq).toBe(2);
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
				myScore: 42,
				chainLength: 7
			})
		);

		expect(store.state.phase).toBe('over');
		expect(store.state.myTurn).toBe(false);
		expect(store.state.result).toEqual({
			iWon: true,
			reason: GameEndReason.NO_LEGAL_MOVE,
			myScore: 42,
			chainLength: 7,
			suggestions: []
		});
	});

	it('keeps the words the losing side could have played', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(
			msg('gameOver', {
				iWon: false,
				reason: GameEndReason.TIMEOUT,
				myScore: 12,
				chainLength: 3,
				suggestions: ['sinh viên', 'sinh sôi']
			})
		);

		expect(store.state.result?.suggestions).toEqual(['sinh viên', 'sinh sôi']);
	});

	it('reads an absent list as a position that had nothing left', () => {
		// A dead end and an older server that never sends the field arrive the
		// same way, and both mean "no words to offer" rather than undefined.
		const store = createGameStore();
		store.apply(started());
		store.apply(msg('gameOver', { iWon: false, reason: GameEndReason.NO_LEGAL_MOVE }));

		expect(store.state.result?.suggestions).toEqual([]);
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

describe('opponentLeft', () => {
	it('exposes the grace window so the UI can say how long to wait', () => {
		const store = createGameStore();
		store.apply(msg('opponentLeft', { canReconnect: true, graceMs: 30_000 }));
		expect(store.state.opponentLeft).toEqual({ canReconnect: true, graceMs: 30_000 });
	});
});

/** One lobby snapshot, with the fields a test does not care about defaulted. */
function lobby(fields = {}) {
	return msg('roomState', {
		roomCode: 'ABCD',
		iAmOwner: false,
		canStart: false,
		iAmReady: false,
		opponentPresent: false,
		opponentName: '',
		opponentReady: false,
		opponentConnected: false,
		...fields
	});
}

describe('room messages', () => {
	it('keeps the room code and the sanitized opponent name', () => {
		const store = createGameStore();
		store.apply(lobby({ opponentPresent: true, opponentName: 'Lan', opponentConnected: true }));

		expect(store.state.roomCode).toBe('ABCD');
		expect(store.state.opponentName).toBe('Lan');
	});

	it('survives a reset, because identity outlives one game', () => {
		const store = createGameStore();
		store.apply(msg('welcome', { acceptedNickname: 'Minh', sessionId: 's', resumeToken: 't' }));
		store.apply(lobby({ opponentPresent: true, opponentName: 'Lan', opponentConnected: true }));
		store.reset();

		expect(store.state.nickname).toBe('Minh');
		expect(store.state.roomCode).toBe('ABCD');
		expect(store.state.opponentName).toBe('Lan');
		expect(store.state.opponentPresent).toBe(true);
		expect(store.state.chain).toEqual([]);
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

describe('the lobby', () => {
	it('opens the lobby when the room appears', () => {
		const store = createGameStore();
		store.apply(lobby({ roomCode: 'K7M2QP', iAmOwner: true }));

		expect(store.state.phase).toBe('lobby');
		expect(store.state.roomCode).toBe('K7M2QP');
		expect(store.state.isOwner).toBe(true);
	});

	it('takes every field from the server rather than deriving any', () => {
		const store = createGameStore();
		store.apply(
			lobby({
				iAmOwner: false,
				iAmReady: true,
				canStart: true,
				opponentPresent: true,
				opponentName: 'Lan',
				opponentReady: false,
				opponentConnected: true
			})
		);

		expect(store.state).toMatchObject({
			isOwner: false,
			isReady: true,
			canStart: true,
			opponentPresent: true,
			opponentName: 'Lan',
			opponentReady: false,
			opponentConnected: true
		});
	});

	it('does not drop a live game back into the lobby', () => {
		// A room state arriving mid-game is a presence change, not a phase.
		// Acting on it would replace the board with the lobby mid-turn.
		const store = createGameStore();
		store.apply(started());
		store.apply(lobby({ opponentPresent: true, opponentConnected: true }));

		expect(store.state.phase).toBe('playing');
	});

	it('keeps the result on screen when a finished game returns to the lobby', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(msg('gameOver', { iWon: false, reason: GameEndReason.RESIGNED }));
		store.apply(lobby({ opponentPresent: true, opponentConnected: true }));

		expect(store.state.phase).toBe('over');
		expect(store.state.result).not.toBeNull();
	});

	it('stops waiting for an opponent who is connected again', () => {
		const store = createGameStore();
		store.apply(started());
		store.apply(msg('opponentLeft', { canReconnect: true, graceMs: 30_000 }));
		store.apply(lobby({ opponentPresent: true, opponentConnected: true }));

		expect(store.state.opponentLeft).toBeNull();
		expect(store.state.opponentConnected).toBe(true);
	});

	it('forgets the room when this player is kicked out of it', () => {
		const store = createGameStore();
		store.apply(lobby({ opponentPresent: true, opponentConnected: true }));
		store.apply(msg('error', { code: 'kicked', message: '' }));

		expect(store.state.phase).toBe('idle');
		expect(store.state.roomCode).toBe('');
		expect(store.state.error).toBe('Bạn đã bị mời ra khỏi phòng.');
	});

	it('forgets a room that closed for sitting idle', () => {
		const store = createGameStore();
		store.apply(lobby({ iAmOwner: true }));
		store.apply(msg('error', { code: 'room_idle_closed', message: '' }));

		expect(store.state.phase).toBe('idle');
		expect(store.state.roomCode).toBe('');
		expect(store.state.error).not.toBeNull();
	});
});
