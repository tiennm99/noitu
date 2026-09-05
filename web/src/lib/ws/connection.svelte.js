import { Status, createClient } from './client.js';
import { game } from '$lib/stores/game.svelte.js';
import { settings } from '$lib/stores/settings.svelte.js';

/**
 * One socket for the whole app.
 *
 * The client itself is framework-agnostic and injectable, which is what makes
 * it testable; this module is the small reactive shell that binds that one
 * instance to the two stores and to the component tree.
 */
const state = $state({ status: Status.CLOSED });

/** @type {ReturnType<typeof createClient> | null} */
let client = null;

/** Opens the socket if it is not already open. Safe to call from any route. */
export function connect() {
	if (client) return;
	client = createClient({
		nickname: () => settings.state.nickname,
		onMessage: (msg) => game.apply(msg),
		onStatus: (status) => {
			state.status = status;
		}
	});
	client.connect();
}

/**
 * Sends a message, reporting whether it actually went out.
 *
 * Deliberately does not open the socket: a caller that has not connected yet
 * has nothing queued to resume, and auto-connecting here would reopen the
 * connection during teardown.
 *
 * @param {any} msg - a ClientMessage
 * @returns {boolean}
 */
export function send(msg) {
	return client?.send(msg) ?? false;
}

/**
 * The server's clock as this client estimates it. The countdown is drawn
 * against this rather than Date.now(), so a device with a wrong clock still
 * shows the right remaining time.
 */
export function serverNow() {
	return client?.serverNow() ?? Date.now();
}

/**
 * Closes the socket and forgets the session.
 *
 * Called when the player leaves the board. Keeping the socket open across
 * routes would mean the next game is announced to the server under whatever
 * nickname the previous `Hello` carried, and the resume token would offer the
 * abandoned seat back to a connection that no longer wants it.
 */
export function disconnect() {
	client?.forgetSession();
	client?.close();
	client = null;
	state.status = Status.CLOSED;
}

export const connection = state;
export { Status };
