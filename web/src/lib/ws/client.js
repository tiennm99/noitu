import { fromBinary, toBinary } from '@bufbuild/protobuf';
import { ClientMessageSchema, ServerMessageSchema } from '$lib/proto/noitu/v1/game_pb.js';
import { hello, ping } from './messages.js';

/** Connection states surfaced to the UI. */
export const Status = {
	CONNECTING: 'connecting',
	OPEN: 'open',
	RECONNECTING: 'reconnecting',
	CLOSED: 'closed'
};

/**
 * Reconnect delays in milliseconds, capped rather than unbounded. The last
 * value repeats forever: a server that has been down for a minute is being
 * restarted or redeployed, and hammering it does not bring it back sooner.
 */
export const BACKOFF_MS = [500, 1000, 2000, 4000, 8000];

/** How often the clock-offset probe is sent while the socket is open. */
export const PING_INTERVAL_MS = 5000;

/**
 * Server errors that reconnecting cannot fix.
 *
 * The server closes the socket after refusing a `Hello` it cannot speak to, and
 * the next attempt would send the same rejected handshake. Without this the
 * client reconnects every half second forever after a deploy that bumps the
 * protocol.
 */
export const TERMINAL_ERROR_CODES = new Set(['protocol_version_mismatch']);

const RESUME_KEY = 'noitu.resumeToken';

/**
 * Resolves the socket URL from the page's own origin.
 *
 * The dev server proxies /ws to the Go binary and production serves both from
 * one origin, so the client never has an environment-specific URL to get
 * wrong. That divergence is the one thing that would work under `npm run dev`
 * and break the moment the binary serves the bundle.
 *
 * @param {{ protocol: string, host: string }} loc
 * @returns {string}
 */
export function socketUrl(loc) {
	const scheme = loc.protocol === 'https:' ? 'wss:' : 'ws:';
	return `${scheme}//${loc.host}/ws`;
}

/**
 * Session storage, not local: a resume token belongs to one tab's game, and
 * two tabs sharing one token would fight over the same seat. Access is guarded
 * because a private-mode browser can throw on the property itself, not only on
 * the call.
 *
 * @returns {Storage | null}
 */
function safeSessionStorage() {
	try {
		return globalThis.sessionStorage ?? null;
	} catch {
		return null;
	}
}

/**
 * Creates the socket client.
 *
 * Everything environment-shaped is injected, so the reconnect schedule and the
 * clock offset can be tested without a real socket or a real clock.
 *
 * @param {object} options
 * @param {() => string} options.nickname - read at each connect, so a name
 *   changed between attempts is the one the server is told about
 * @param {(msg: any) => void} options.onMessage
 * @param {(status: string) => void} [options.onStatus]
 * @param {string} [options.url]
 * @param {(url: string) => WebSocket} [options.socketFactory]
 * @param {() => number} [options.now]
 * @param {() => number} [options.random] - jitter source
 * @param {typeof setTimeout} [options.schedule]
 * @param {(id: any) => void} [options.cancel]
 */
export function createClient({
	nickname,
	onMessage,
	onStatus = () => {},
	url = socketUrl(globalThis.location ?? { protocol: 'http:', host: 'localhost' }),
	socketFactory = (u) => new WebSocket(u),
	now = () => Date.now(),
	random = Math.random,
	schedule = setTimeout,
	cancel = clearTimeout
}) {
	/** @type {WebSocket | null} */
	let socket = null;
	let attempt = 0;
	// Set by a deliberate close and by a server error that reconnecting cannot
	// fix. Both mean the same thing to onclose: do not come back.
	let stopReconnecting = false;
	/** @type {any} */
	let reconnectTimer = null;
	/** @type {any} */
	let pingTimer = null;
	let clockOffsetMs = 0;
	let status = Status.CLOSED;

	/** @param {string} next */
	function setStatus(next) {
		if (status === next) return;
		status = next;
		onStatus(next);
	}

	function storedToken() {
		try {
			return safeSessionStorage()?.getItem(RESUME_KEY) ?? '';
		} catch {
			return '';
		}
	}

	/** @param {string} token */
	function storeToken(token) {
		try {
			safeSessionStorage()?.setItem(RESUME_KEY, token);
		} catch {
			// A browser refusing storage costs the player a reconnect, not the
			// session. There is nothing to recover here.
		}
	}

	function stopPing() {
		if (pingTimer !== null) {
			cancel(pingTimer);
			pingTimer = null;
		}
	}

	function schedulePing() {
		stopPing();
		pingTimer = schedule(() => {
			send(ping(now()));
			schedulePing();
		}, PING_INTERVAL_MS);
	}

	/**
	 * Backoff with jitter: several tabs dropped by one restart would otherwise
	 * come back in lockstep and arrive as a single burst.
	 */
	function nextDelay() {
		const base = BACKOFF_MS[Math.min(attempt, BACKOFF_MS.length - 1)];
		attempt += 1;
		return Math.round(base * (0.5 + 0.5 * random()));
	}

	function scheduleReconnect() {
		if (stopReconnecting || reconnectTimer !== null) return;
		setStatus(Status.RECONNECTING);
		reconnectTimer = schedule(() => {
			reconnectTimer = null;
			open();
		}, nextDelay());
	}

	function open() {
		stopReconnecting = false;
		setStatus(attempt === 0 ? Status.CONNECTING : Status.RECONNECTING);

		const ws = socketFactory(url);
		ws.binaryType = 'arraybuffer';
		socket = ws;

		ws.onopen = () => {
			// Hello goes out before the status is announced. The server refuses
			// every other message until the handshake lands, and a listener
			// reacting to "open" by sending something would otherwise race it.
			send(hello({ nickname: nickname(), resumeToken: storedToken() }));
			// Probe the clock immediately rather than waiting out the first
			// interval. GameStarted arrives about one round trip after Hello, so
			// a deferred first probe would leave the opening turn counting down
			// against the raw device clock.
			send(ping(now()));
			schedulePing();
			setStatus(Status.OPEN);
		};

		ws.onmessage = (event) => {
			let msg;
			try {
				msg = fromBinary(ServerMessageSchema, new Uint8Array(event.data));
			} catch {
				// An undecodable frame is a contract violation, not a game
				// event. Dropping it stops one bad frame from ending the
				// session; an incompatible peer is caught by the server's own
				// version check instead.
				return;
			}
			intercept(msg);
			onMessage(msg);
		};

		ws.onclose = () => {
			stopPing();
			socket = null;
			if (stopReconnecting) {
				setStatus(Status.CLOSED);
				return;
			}
			scheduleReconnect();
		};

		ws.onerror = () => {
			// onclose always follows, and that is where the reconnect is
			// scheduled. Handling both would schedule it twice.
		};
	}

	/**
	 * Two messages are the transport's own business before the UI sees them:
	 * Welcome carries the token a reconnect needs, and Pong is the clock probe.
	 * Both are still forwarded, because the UI shows the accepted nickname.
	 *
	 * @param {any} msg
	 */
	function intercept(msg) {
		const payload = msg.payload;
		if (payload.case === 'welcome') {
			// The backoff resets here, not when the socket opens. A server that
			// accepts the connection and then rejects the handshake would
			// otherwise look like a success to the schedule, and every retry
			// would start again from the shortest delay.
			attempt = 0;
			if (payload.value.resumeToken) storeToken(payload.value.resumeToken);
			return;
		}
		if (payload.case === 'error' && TERMINAL_ERROR_CODES.has(payload.value.code)) {
			stopReconnecting = true;
			return;
		}
		if (payload.case === 'pong') {
			const sent = Number(payload.value.clientTimeMs);
			const serverTime = Number(payload.value.serverTimeMs);
			const received = now();
			// Half the round trip is the best estimate of the one-way delay, so
			// the server's timestamp is compared against the midpoint of the
			// exchange rather than against either end of it.
			clockOffsetMs = serverTime + (received - sent) / 2 - received;
		}
	}

	/** @param {any} msg */
	function send(msg) {
		if (!socket || socket.readyState !== 1) return false;
		socket.send(toBinary(ClientMessageSchema, msg));
		return true;
	}

	return {
		connect: open,
		send,
		/** The server's clock as this client best estimates it. */
		serverNow: () => now() + clockOffsetMs,
		clockOffset: () => clockOffsetMs,
		status: () => status,
		/** Deliberate teardown: no reconnect follows. */
		close() {
			stopReconnecting = true;
			stopPing();
			if (reconnectTimer !== null) {
				cancel(reconnectTimer);
				reconnectTimer = null;
			}
			socket?.close();
			socket = null;
			setStatus(Status.CLOSED);
		},
		/** Forgets the resume token so the next Hello starts a fresh session. */
		forgetSession() {
			try {
				safeSessionStorage()?.removeItem(RESUME_KEY);
			} catch {
				// Same as storeToken: nothing to recover.
			}
		}
	};
}
