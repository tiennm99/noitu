// @vitest-environment jsdom

// The module-level connection is what a screen actually calls. These tests
// drive it through a stubbed WebSocket, because what they pin is which resume
// token a screen's socket carries and what happens to the board when the
// server refuses one.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { create, fromBinary, toBinary } from '@bufbuild/protobuf';
import {
	ClientMessageSchema,
	ServerMessageSchema
} from '../src/lib/proto/noitu/v1/game_pb.js';
import { game } from '../src/lib/stores/game.svelte.js';
import { connect, disconnect } from '../src/lib/ws/connection.svelte.js';
import { receive } from './component-support.js';

const RESUME_KEY = 'noitu.resumeToken';

/** @type {FakeWebSocket[]} */
let sockets = [];

class FakeWebSocket {
	/** @param {string} url */
	constructor(url) {
		this.url = url;
		this.readyState = 0;
		this.binaryType = '';
		/** @type {Uint8Array[]} */
		this.sent = [];
		this.onopen = () => {};
		this.onmessage = (/** @type {{ data: ArrayBuffer }} */ _event) => {};
		this.onclose = () => {};
		this.onerror = () => {};
		sockets.push(this);
	}

	send(/** @type {Uint8Array} */ bytes) {
		this.sent.push(bytes);
	}

	close() {}

	open() {
		this.readyState = 1;
		this.onopen();
	}

	/**
	 * @param {string} kind
	 * @param {object} value
	 */
	deliver(kind, value) {
		const msg = create(ServerMessageSchema, { payload: { case: kind, value } });
		this.onmessage({ data: toBinary(ServerMessageSchema, msg).buffer });
	}

	helloToken() {
		const [first] = this.sent.map((b) => fromBinary(ClientMessageSchema, new Uint8Array(b)));
		return first.payload.case === 'hello' ? first.payload.value.resumeToken : null;
	}
}

const greeting = { sessionId: 's1', resumeToken: 'fresh-token', protocolVersion: 1, acceptedNickname: 'Minh' };

beforeEach(() => {
	sockets = [];
	sessionStorage.clear();
	vi.stubGlobal('WebSocket', FakeWebSocket);
	game.leave();
});

afterEach(() => {
	disconnect();
	vi.unstubAllGlobals();
});

describe('opening a socket for a game of its own', () => {
	it('does not replay a token left by an earlier game or tab', () => {
		sessionStorage.setItem(RESUME_KEY, 'left-behind');

		connect({ freshSession: true });
		sockets[0].open();

		expect(sockets[0].helloToken()).toBe('');
	});

	it('replays the stored token when nothing asked for a fresh session', () => {
		sessionStorage.setItem(RESUME_KEY, 'left-behind');

		connect();
		sockets[0].open();

		expect(sockets[0].helloToken()).toBe('left-behind');
	});

	it('keeps the token this game was given, so a dropped socket still resumes', () => {
		sessionStorage.setItem(RESUME_KEY, 'left-behind');
		connect({ freshSession: true });
		sockets[0].open();
		sockets[0].deliver('welcome', greeting);

		// A rematch asks again on the socket that already exists. That must
		// not throw away the token the game in progress is resumable by.
		connect({ freshSession: true });

		expect(sessionStorage.getItem(RESUME_KEY)).toBe('fresh-token');
		expect(sockets).toHaveLength(1);
	});
});

describe('a resume the server refuses', () => {
	it('clears the room the board still shows, and still says why', () => {
		receive('roomState', {
			roomCode: 'ABCD',
			canStart: false,
			maxPlayers: 4,
			minPlayers: 2,
			graceMs: 30_000,
			players: [{ playerId: 'p1', name: 'Minh', isMe: true, connected: true }]
		});
		expect(game.state.phase).toBe('lobby');
		sessionStorage.setItem(RESUME_KEY, 'old-token');

		connect();
		sockets[0].open();
		sockets[0].deliver('welcome', greeting);
		sockets[0].deliver('error', { code: 'game_already_over', message: '' });

		expect(game.state.phase).toBe('idle');
		expect(game.state.roomCode).toBe('');
		expect(game.state.error).toBe('Ván đấu đã kết thúc.');
	});
});
