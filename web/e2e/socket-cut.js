/**
 * A controllable break in a page's WebSocket.
 *
 * Offline emulation is not usable here: Chromium's network conditions do not
 * apply to a loopback socket, so a test that "went offline" against a local
 * server kept playing quite happily. Routing the socket cuts it for real.
 *
 * The server does not reliably see it, though: closing a routed connection is
 * observed by the page but does not always reach the far end, so the grace
 * window may never start. That makes this the right tool for what the client
 * does about a dead socket, and the wrong one for what the server does about a
 * missing player. For that, take the page away instead.
 *
 * @param {import('@playwright/test').Page} page
 */
export async function cuttableSocket(page) {
	/** @type {any} */
	let live = null;
	/** @type {any} */
	let upstream = null;
	let blocked = false;

	await page.routeWebSocket(/\/ws$/, (ws) => {
		if (blocked) {
			// Refuse without connecting, so the client keeps retrying and the
			// outage lasts as long as the test wants it to.
			ws.close({ code: 1006 });
			return;
		}
		live = ws;
		// With no message handlers registered, frames are proxied in both
		// directions, so the game runs against the real server as usual.
		upstream = ws.connectToServer();
	});

	return {
		/** Drops the current connection, optionally refusing reconnects too. */
		async cut({ sustained = false } = {}) {
			blocked = sustained;
			// Both halves. Closing only the page side leaves the server holding
			// a connection it thinks is fine, so it never starts the grace
			// window and the opponent is never told.
			await upstream?.close({ code: 1006 });
			await live?.close({ code: 1006 });
			upstream = null;
			live = null;
		},
		/** Lets the next reconnect through. */
		restore() {
			blocked = false;
		}
	};
}
