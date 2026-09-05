// The lifecycle these tests cover is where the screen used to infer "start a
// game" from the board being empty. Clearing the board for a rematch tripped
// that inference, so a rematch sent two StartBotGame messages and the server
// built two rooms that then fought over one socket. A request is now a stored
// intent, and the first test here is the one that would have caught it.

import { describe, expect, it } from 'vitest';
import { createBotSession } from '../src/lib/stores/bot-session.svelte.js';

/** Records what reached the socket. */
function recorder(accepted = true) {
	/** @type {number[]} */
	const sent = [];
	return {
		sent,
		start: (difficulty) => {
			sent.push(difficulty);
			return accepted;
		}
	};
}

describe('requesting a game', () => {
	it('holds the request until the socket is open', () => {
		const r = recorder();
		const session = createBotSession(r);

		session.request(2);
		expect(session.flush(false)).toBe(false);
		expect(r.sent).toEqual([]);

		expect(session.flush(true)).toBe(true);
		expect(r.sent).toEqual([2]);
	});

	it('sends one request per ask, however often it is flushed', () => {
		const r = recorder();
		const session = createBotSession(r);

		session.request(3);
		session.flush(true);
		session.flush(true);
		session.flush(true);

		expect(r.sent).toEqual([3]);
	});

	it('sends nothing when nothing was asked for', () => {
		const r = recorder();
		const session = createBotSession(r);

		expect(session.flush(true)).toBe(false);
		expect(r.sent).toEqual([]);
	});

	it('keeps only the latest difficulty when asked twice before sending', () => {
		const r = recorder();
		const session = createBotSession(r);

		session.request(1);
		session.request(3);
		session.flush(true);

		expect(r.sent).toEqual([3]);
	});

	it('drops a queued request when the player leaves first', () => {
		const r = recorder();
		const session = createBotSession(r);

		session.request(2);
		session.cancel();
		session.flush(true);

		expect(r.sent).toEqual([]);
	});

	it('holds a refused request so the next open connection carries it', () => {
		// A refused send never reached the server, so there is no room to
		// duplicate — only a player left staring at a board that never starts.
		const refused = recorder(false);
		const session = createBotSession(refused);

		session.request(2);
		expect(session.flush(true)).toBe(false);
		expect(session.flush(true)).toBe(false);
		expect(refused.sent).toEqual([2, 2]);
	});
});

describe('recording a result', () => {
	/** A settings double that remembers the best score per difficulty. */
	function settingsDouble() {
		/** @type {Record<string, number>} */
		const best = {};
		return {
			best,
			recordScore(difficulty, score) {
				if (score <= (best[difficulty] ?? 0)) return false;
				best[difficulty] = score;
				return true;
			}
		};
	}

	it('reports a first result as a record', () => {
		const session = createBotSession(recorder());
		const settings = settingsDouble();

		expect(session.score({ myScore: 40 }, 1, settings)).toBe(true);
		expect(settings.best[1]).toBe(40);
	});

	it('counts one result once, however often the screen re-renders', () => {
		const session = createBotSession(recorder());
		const settings = settingsDouble();
		const result = { myScore: 40 };

		expect(session.score(result, 1, settings)).toBe(true);
		expect(session.score(result, 1, settings)).toBe(false);
		expect(session.score(result, 1, settings)).toBe(false);
	});

	it('still scores a later game that happens to have the same numbers', () => {
		// Identity, not value: two games can end on the same score, and the
		// second is a fresh result the player deserves credit for.
		const session = createBotSession(recorder());
		const settings = settingsDouble();

		session.score({ myScore: 40 }, 1, settings);
		expect(session.score({ myScore: 90 }, 1, settings)).toBe(true);
	});

	it('ignores a game that has not finished', () => {
		const session = createBotSession(recorder());
		const settings = settingsDouble();

		expect(session.score(null, 1, settings)).toBe(false);
		expect(settings.best).toEqual({});
	});

	it('lets a new game set a record again after a reset', () => {
		const session = createBotSession(recorder());
		const settings = settingsDouble();
		const result = { myScore: 40 };

		session.score(result, 1, settings);
		session.reset();

		// The same object now counts as new, because the reset forgot it.
		expect(session.score(result, 1, settings)).toBe(false); // no improvement
		expect(session.score({ myScore: 41 }, 1, settings)).toBe(true);
	});
});

describe('reset', () => {
	it('clears a queued request as well as the scored result', () => {
		const r = recorder();
		const session = createBotSession(r);

		session.request(2);
		session.reset();
		session.flush(true);

		expect(r.sent).toEqual([]);
	});
});
