// @vitest-environment jsdom

// The socket client owns three things the UI cannot see going wrong: the
// handshake, the reconnect schedule, and the clock offset the countdown is
// drawn from. All three are driven here through injected fakes, so the
// assertions are about the client rather than about a real network.

import { beforeEach, describe, expect, it } from 'vitest';
import { create, toBinary } from '@bufbuild/protobuf';
import {
	ClientMessageSchema,
	ServerMessageSchema
} from '../src/lib/proto/noitu/v1/game_pb.js';
import {
	BACKOFF_MS,
	LIVENESS_TIMEOUT_MS,
	PING_INTERVAL_MS,
	Status,
	createClient,
	socketUrl
} from '../src/lib/ws/client.js';

class FakeSocket {
	/** @param {string} url */
	constructor(url) {
		this.url = url;
		this.readyState = 0;
		this.binaryType = '';
		/** @type {Uint8Array[]} */
		this.sent = [];
		this.closed = false;
		this.onopen = () => {};
		this.onmessage = () => {};
		this.onclose = () => {};
		this.onerror = () => {};
	}

	send(bytes) {
		this.sent.push(bytes);
	}

	close() {
		this.closed = true;
	}

	// --- test drivers ---

	open() {
		this.readyState = 1;
		this.onopen();
	}

	/** @param {any} serverMessage */
	deliver(serverMessage) {
		this.onmessage({ data: toBinary(ServerMessageSchema, serverMessage).buffer });
	}

	deliverRaw(bytes) {
		this.onmessage({ data: bytes.buffer ?? bytes });
	}

	drop() {
		this.readyState = 3;
		this.onclose();
	}
}

/**
 * A controllable clock. Timers are stored rather than run, so a test decides
 * when time passes and can read the delay each one was scheduled with.
 */
function fakeTimers() {
	let nextId = 1;
	const pending = new Map();
	return {
		delays: /** @type {number[]} */ ([]),
		schedule(fn, delay) {
			this.delays.push(delay);
			const id = nextId++;
			pending.set(id, fn);
			return id;
		},
		cancel(id) {
			pending.delete(id);
		},
		/** Runs every timer queued so far, once. */
		flush() {
			const due = [...pending.entries()];
			pending.clear();
			for (const [, fn] of due) fn();
		},
		size: () => pending.size
	};
}

function serverMsg(kind, value) {
	return create(ServerMessageSchema, { payload: { case: kind, value } });
}

/**
 * @param {object} [options]
 */
function setup(options = {}) {
	/** @type {FakeSocket[]} */
	const sockets = [];
	/** @type {any[]} */
	const received = [];
	/** @type {string[]} */
	const statuses = [];
	const timers = fakeTimers();
	let clock = 1_000;

	const client = createClient({
		nickname: () => options.nickname ?? 'Minh',
		onMessage: (m) => received.push(m),
		onStatus: (s) => statuses.push(s),
		url: 'ws://localhost/ws',
		socketFactory: (url) => {
			const s = new FakeSocket(url);
			sockets.push(s);
			return s;
		},
		now: () => clock,
		random: options.random ?? (() => 1),
		schedule: (fn, delay) => timers.schedule(fn, delay),
		cancel: (id) => timers.cancel(id)
	});

	return {
		client,
		sockets,
		received,
		statuses,
		timers,
		// The ping interval uses the same timer double, so the reconnect
		// schedule has to be read out from under it.
		reconnectDelays: () => timers.delays.filter((d) => d !== PING_INTERVAL_MS),
		setClock: (v) => {
			clock = v;
		},
		last: () => sockets[sockets.length - 1]
	};
}

/** @param {FakeSocket} socket */
async function sentMessages(socket) {
	const { fromBinary } = await import('@bufbuild/protobuf');
	return socket.sent.map((b) => fromBinary(ClientMessageSchema, new Uint8Array(b)));
}

beforeEach(() => {
	sessionStorage.clear();
});

describe('socketUrl', () => {
	it('follows the page scheme so a TLS page never opens a plaintext socket', () => {
		expect(socketUrl({ protocol: 'https:', host: 'noitu.example' })).toBe(
			'wss://noitu.example/ws'
		);
		expect(socketUrl({ protocol: 'http:', host: 'localhost:5173' })).toBe(
			'ws://localhost:5173/ws'
		);
	});
});

describe('handshake', () => {
	it('sends Hello with the protocol version as the first message', async () => {
		const h = setup();
		h.client.connect();
		h.last().open();

		const [first] = await sentMessages(h.last());
		expect(first.payload.case).toBe('hello');
		expect(first.payload.value.protocolVersion).toBe(1);
		expect(first.payload.value.nickname).toBe('Minh');
		expect(first.payload.value.resumeToken).toBe('');
	});

	it('replays the resume token from the previous Welcome on the next connect', async () => {
		const h = setup();
		h.client.connect();
		h.last().open();
		h.last().deliver(
			serverMsg('welcome', {
				sessionId: 's1',
				resumeToken: 'token-1',
				protocolVersion: 1,
				acceptedNickname: 'Minh'
			})
		);

		h.last().drop();
		h.timers.flush();
		h.last().open();

		const [hello] = await sentMessages(h.last());
		expect(hello.payload.value.resumeToken).toBe('token-1');
	});

	it('forgets the token on request, so the next Hello starts a fresh session', async () => {
		const h = setup();
		h.client.connect();
		h.last().open();
		h.last().deliver(
			serverMsg('welcome', {
				sessionId: 's1',
				resumeToken: 'token-1',
				protocolVersion: 1,
				acceptedNickname: 'Minh'
			})
		);
		h.client.forgetSession();

		h.last().drop();
		h.timers.flush();
		h.last().open();

		const [hello] = await sentMessages(h.last());
		expect(hello.payload.value.resumeToken).toBe('');
	});
});

describe('reconnect', () => {
	it('backs off along the documented schedule and then holds at the cap', () => {
		const h = setup({ random: () => 1 });
		h.client.connect();

		// Each attempt fails before it opens, so the delay grows every time.
		for (let i = 0; i < BACKOFF_MS.length + 2; i += 1) {
			h.last().drop();
			h.timers.flush();
		}

		const expected = [...BACKOFF_MS, BACKOFF_MS.at(-1), BACKOFF_MS.at(-1)];
		expect(h.reconnectDelays()).toEqual(expected);
	});

	it('jitters below the base delay, never above it', () => {
		const h = setup({ random: () => 0 });
		h.client.connect();
		h.last().drop();

		expect(h.reconnectDelays()[0]).toBe(BACKOFF_MS[0] / 2);
	});

	it('does not reconnect after a deliberate close', () => {
		const h = setup();
		h.client.connect();
		h.last().open();

		const socketsBefore = h.sockets.length;
		h.client.close();
		h.timers.flush();

		expect(h.sockets.length).toBe(socketsBefore);
		expect(h.client.status()).toBe(Status.CLOSED);
	});

	it('reports reconnecting while it waits, and open once it lands', () => {
		const h = setup();
		h.client.connect();
		h.last().open();
		h.last().drop();
		h.timers.flush();
		h.last().open();

		expect(h.statuses).toEqual([
			Status.CONNECTING,
			Status.OPEN,
			Status.RECONNECTING,
			Status.OPEN
		]);
	});
});

describe('clock offset', () => {
	it('estimates the server clock from the midpoint of the round trip', () => {
		const h = setup();
		h.client.connect();
		h.last().open();

		// Ping left at 1000, the reply is read at 1100, and the server stamped
		// it 5050. The one-way delay is half of 100, so the server clock is
		// 5050 + 50 - 1100 = 4000 ahead.
		h.setClock(1100);
		h.last().deliver(serverMsg('pong', { clientTimeMs: 1000n, serverTimeMs: 5050n }));

		expect(h.client.clockOffset()).toBe(4000);
		expect(h.client.serverNow()).toBe(5100);
	});

	it('starts at zero offset, so the countdown works before the first Pong', () => {
		const h = setup();
		expect(h.client.clockOffset()).toBe(0);
	});

	it('probes the clock on an interval while the socket is open', async () => {
		const h = setup();
		h.client.connect();
		h.last().open();

		expect(h.timers.delays).toContain(PING_INTERVAL_MS);
		h.timers.flush();

		const sent = await sentMessages(h.last());
		expect(sent.map((m) => m.payload.case)).toEqual(['hello', 'ping', 'ping']);
	});

	it('probes immediately on connect, so the opening turn is already corrected', async () => {
		// GameStarted arrives about one round trip after Hello. Waiting out the
		// first interval would draw that whole turn against the device clock.
		const h = setup();
		h.client.connect();
		h.last().open();

		const sent = await sentMessages(h.last());
		expect(sent.map((m) => m.payload.case)).toEqual(['hello', 'ping']);
	});
});

describe('frames', () => {
	it('forwards a decoded server message to the caller', () => {
		const h = setup();
		h.client.connect();
		h.last().open();
		h.last().deliver(serverMsg('roomState', { roomCode: 'ABCD' }));

		expect(h.received).toHaveLength(1);
		expect(h.received[0].payload.case).toBe('roomState');
	});

	it('drops an undecodable frame instead of ending the session', () => {
		const h = setup();
		h.client.connect();
		h.last().open();

		// Field 1 declared as a varint but truncated: valid framing, invalid body.
		expect(() => h.last().deliverRaw(new Uint8Array([0x08]))).not.toThrow();
		expect(h.received).toHaveLength(0);
		expect(h.client.status()).toBe(Status.OPEN);
	});

	it('refuses to send while the socket is not open', () => {
		const h = setup();
		h.client.connect();
		const before = h.last().sent.length;

		expect(h.client.send(create(ClientMessageSchema, {}))).toBe(false);
		expect(h.last().sent.length).toBe(before);
	});
});

describe('handshake ordering', () => {
	it('sends Hello before announcing the connection is open', async () => {
		// The server refuses everything until the handshake lands, so a listener
		// that reacts to "open" by sending a message must not be able to get
		// ahead of it.
		/** @type {string[]} */
		const order = [];
		/** @type {any} */
		let socket;
		const client = createClient({
			nickname: () => 'Minh',
			onMessage: () => {},
			onStatus: (s) => order.push(`status:${s}`),
			url: 'ws://localhost/ws',
			socketFactory: () => {
				socket = new FakeSocket('ws://localhost/ws');
				const realSend = socket.send.bind(socket);
				socket.send = (bytes) => {
					order.push('send');
					realSend(bytes);
				};
				return socket;
			},
			schedule: () => 1,
			cancel: () => {}
		});

		client.connect();
		socket.open();

		expect(order.indexOf('send')).toBeLessThan(order.indexOf(`status:${Status.OPEN}`));
	});
});

describe('a handshake the server refuses', () => {
	/** @param {any} h */
	function refuseVersion(h) {
		h.client.connect();
		h.last().open();
		h.last().deliver(serverMsg('error', { code: 'protocol_version_mismatch', message: '' }));
		h.last().drop();
	}

	it('stops reconnecting, instead of retrying a handshake that cannot succeed', () => {
		const h = setup();
		const before = h.sockets.length;

		refuseVersion(h);
		h.timers.flush();

		expect(h.sockets.length).toBe(before + 1);
		expect(h.client.status()).toBe(Status.CLOSED);
	});

	it('still shows the player why, by forwarding the error', () => {
		const h = setup();
		refuseVersion(h);

		expect(h.received.map((m) => m.payload.case)).toContain('error');
	});

	it('keeps retrying an error that a fresh connection could clear', () => {
		const h = setup();
		h.client.connect();
		h.last().open();
		h.last().deliver(serverMsg('error', { code: 'room_not_found', message: '' }));
		h.last().drop();
		h.timers.flush();

		expect(h.client.status()).not.toBe(Status.CLOSED);
	});
});

describe('backoff and the handshake', () => {
	it('resets on Welcome, not merely on the socket opening', () => {
		// A server that accepts the connection and then rejects the handshake
		// would otherwise look like a success, and every retry would start again
		// from the shortest delay.
		const h = setup({ random: () => 1 });
		h.client.connect();

		h.last().drop();
		h.timers.flush();
		h.last().open(); // opens, but no Welcome arrives
		h.last().drop();
		h.timers.flush();

		expect(h.reconnectDelays()).toEqual([500, 1000]);
	});

	it('resets once the server has actually greeted the client', () => {
		const h = setup({ random: () => 1 });
		h.client.connect();

		h.last().drop();
		h.timers.flush();
		h.last().open();
		h.last().deliver(
			serverMsg('welcome', {
				sessionId: 's1',
				resumeToken: 't1',
				protocolVersion: 1,
				acceptedNickname: 'Minh'
			})
		);
		h.last().drop();
		h.timers.flush();

		expect(h.reconnectDelays().at(-1)).toBe(500);
	});
});

describe('a socket that dies without closing', () => {
	/**
	 * Fires the ping timer `ticks` times, advancing the clock by one interval
	 * each time — a healthy tab whose timers are running on schedule.
	 *
	 * @param {any} h
	 * @param {number} ticks
	 * @param {number} start
	 */
	function tick(h, ticks, start) {
		let clock = start;
		for (let i = 0; i < ticks; i += 1) {
			clock += PING_INTERVAL_MS;
			h.setClock(clock);
			h.timers.flush();
		}
		return clock;
	}

	it('is closed by the client once frames stop arriving', () => {
		// A connection that fails without closing leaves onclose unfired, so the
		// page would keep showing a live connection and a running countdown over
		// a socket nothing can reach.
		const h = setup();
		h.client.connect();
		h.last().open();
		const socket = h.last();

		tick(h, LIVENESS_TIMEOUT_MS / PING_INTERVAL_MS + 1, 1000);

		expect(socket.closed).toBe(true);
	});

	it('leaves a socket alone while frames are still arriving', () => {
		const h = setup();
		h.client.connect();
		h.last().open();
		const socket = h.last();

		let clock = 1000;
		for (let i = 0; i < 10; i += 1) {
			clock += PING_INTERVAL_MS;
			h.setClock(clock);
			socket.deliver(serverMsg('pong', { clientTimeMs: 1n, serverTimeMs: 2n }));
			h.timers.flush();
		}

		expect(socket.closed).toBe(false);
	});

	it('does not punish a tab whose timers were throttled', () => {
		// A backgrounded tab fires its timers minutes late through no fault of
		// the connection. Judging silence on a tick that was itself late would
		// close a healthy socket every time the player switched away.
		const h = setup();
		h.client.connect();
		h.last().open();
		const socket = h.last();

		h.setClock(1000 + 10 * 60_000);
		h.timers.flush();

		expect(socket.closed).toBe(false);
	});
});
