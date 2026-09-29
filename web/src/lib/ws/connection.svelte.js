import { Status, createClient, forgetStoredSession, hasStoredSession } from './client.js';
import { claimDeadEnd, reportWord, resign, submitWord } from './messages.js';
import { game } from '$lib/stores/game.svelte.js';
import { settings } from '$lib/stores/settings.svelte.js';

/** @typedef {import('$lib/proto/noitu/v1/game_pb.js').ClientMessage} ClientMessage */

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

/**
 * Opens the socket if it is not already open. Safe to call from any route.
 * @param {{ freshSession?: boolean }} [options] - `freshSession` forgets any
 *   resume token this tab holds before a new socket is opened, for a screen
 *   that is about to ask for a game of its own. It does nothing when a socket
 *   already exists: that socket's token belongs to the game it is in.
 */
export function connect({ freshSession = false } = {}) {
	if (client) return;
	if (freshSession) forgetStoredSession();
	client = createClient({
		nickname: () => settings.state.nickname,
		onMessage: (msg) => game.apply(msg),
		// Runs before the refusing error itself is applied, so the board is
		// cleared first and the error is still shown afterwards.
		onResumeRefused: () => game.leave(),
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
 * @param {ClientMessage} msg
 * @returns {boolean}
 */
export function send(msg) {
	return client?.send(msg) ?? false;
}

/**
 * Retries the connection immediately instead of waiting out the backoff.
 *
 * For the player looking at a "mất kết nối" banner with a turn timer running:
 * the schedule is tuned for a client nobody is watching, and this is the case
 * where somebody is.
 * @returns {boolean} whether an attempt was actually started
 */
export function reconnectNow() {
	return client?.reconnectNow() ?? false;
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
 * Drops the resume token without touching the connection.
 *
 * Used when a resume was refused: the token is spent, but the socket is fine
 * and the player may well want to start something new on it.
 */
export function forgetSession() {
	forgetStoredSession();
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
	forgetStoredSession();
	client?.close();
	client = null;
	state.status = Status.CLOSED;
}

/**
 * The moves a board offers, sent as the server expects them. Both game
 * screens hand these to GameBoard unchanged: what a turn sends does not
 * depend on whether the other side is a bot or a room.
 *
 * Resigning and claiming are armed by the board with a second press rather
 * than a native confirm(), which would block the countdown's frame loop
 * while the server's deadline kept running.
 */
export const turnActions = {
	/**
	 * @param {string} word
	 * @returns {boolean} whether the word reached the server
	 */
	submit: (word) => send(submitWord(word, game.state.turnSeq)),
	resign: () => send(resign()),
	claimDeadEnd: () => send(claimDeadEnd()),
	/** @param {string} word */
	reportWord: (word) => send(reportWord(word))
};

export const connection = state;
export { Status, hasStoredSession };
