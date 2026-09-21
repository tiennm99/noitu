// The online screen's request machine, extracted so its two failure modes —
// a stale resume token that never gets an answer, and a lobby action the
// socket refused — are things this file can prove directly, without a
// mounted page or a real socket.

import { describe, expect, it } from 'vitest';
import { createRoomSession } from '../src/lib/stores/room-session.svelte.js';

/** Records what reached the socket for each request kind. */
function senders(accepted = true) {
	/** @type {string[]} */
	const sent = [];
	return {
		sent,
		create: () => {
			sent.push('create');
			return accepted;
		},
		join: (/** @type {string} */ code) => {
			sent.push(`join:${code}`);
			return accepted;
		},
		quickMatch: () => {
			sent.push('quickMatch');
			return accepted;
		}
	};
}

describe('joining or creating a room', () => {
	it('holds the request until the socket is open', () => {
		const s = senders();
		const session = createRoomSession();

		session.request({ kind: 'create' });
		expect(session.flush(false, s)).toBe(false);
		expect(s.sent).toEqual([]);

		expect(session.flush(true, s)).toBe(true);
		expect(s.sent).toEqual(['create']);
		expect(session.state.pending).toBeNull();
	});

	it('sends a join with its room code', () => {
		const s = senders();
		const session = createRoomSession();

		session.request({ kind: 'join', code: 'K7M2QP' });
		session.flush(true, s);

		expect(s.sent).toEqual(['join:K7M2QP']);
	});

	it('holds a refused request so the next open connection carries it', () => {
		const refused = senders(false);
		const session = createRoomSession();

		session.request({ kind: 'create' });
		expect(session.flush(true, refused)).toBe(false);
		expect(session.flush(true, refused)).toBe(false);

		expect(refused.sent).toEqual(['create', 'create']);
		expect(session.state.pending).not.toBeNull();
	});

	it('keeps only the latest request when asked twice before sending', () => {
		const s = senders();
		const session = createRoomSession();

		session.request({ kind: 'create' });
		session.request({ kind: 'quickMatch' });
		session.flush(true, s);

		expect(s.sent).toEqual(['quickMatch']);
	});

	it('clears every latch a fresh request opens with', () => {
		const session = createRoomSession();
		session.setNeedName(true);
		session.setStalled(true);

		session.request({ kind: 'create' });

		expect(session.state.needName).toBe(false);
		expect(session.state.stalled).toBe(false);
		expect(session.state.resuming).toBe(false);
	});
});

describe('resuming a stored session', () => {
	it('holds a request behind the resume and refuses to send it while resuming', () => {
		const s = senders();
		const session = createRoomSession();

		session.startResume();
		session.holdPendingJoin({ kind: 'join', code: 'K7M2QP' });

		expect(session.flush(true, s)).toBe(false);
		expect(s.sent).toEqual([]);
	});

	it('spends the held join once the resume lands in a room', () => {
		const s = senders();
		const session = createRoomSession();

		session.startResume();
		session.holdPendingJoin({ kind: 'join', code: 'K7M2QP' });
		session.noteRoom();

		expect(session.state.resuming).toBe(false);
		// Arriving in a room answers the held code too — it is not sent
		// afterwards as a second, redundant join.
		expect(session.state.pending).toBeNull();
		expect(session.flush(true, s)).toBe(false);
		expect(s.sent).toEqual([]);
	});

	it('times out to the join form instead of staying stuck forever', () => {
		// The scenario C1 in the review describes: an older server answers a
		// stale token with silence, never with an error.
		const session = createRoomSession();

		session.startResume();
		session.noteResumeFailed(/* named */ true);

		expect(session.state.resuming).toBe(false);
		expect(session.state.resumeFailed).toBe(true);
	});

	it('does nothing if the resume already resolved before the timer fired', () => {
		const session = createRoomSession();

		session.startResume();
		session.noteRoom();
		session.noteResumeFailed(true);

		// noteRoom() already cleared resuming; the stale timer callback must
		// not reopen the failure state on top of a resume that succeeded.
		expect(session.state.resumeFailed).toBe(false);
	});

	it('asks for a name instead of spending an invite code held behind a failed resume', () => {
		const session = createRoomSession();

		session.startResume();
		session.holdPendingJoin({ kind: 'join', code: 'K7M2QP' });
		session.noteResumeFailed(/* named */ false);

		expect(session.state.needName).toBe(true);
		expect(session.state.pending).toBeNull();
	});

	it('keeps a held join when the player already has a name, so it flushes next', () => {
		const s = senders();
		const session = createRoomSession();

		session.startResume();
		session.holdPendingJoin({ kind: 'join', code: 'K7M2QP' });
		session.noteResumeFailed(/* named */ true);

		expect(session.state.needName).toBe(false);
		expect(session.flush(true, s)).toBe(true);
		expect(s.sent).toEqual(['join:K7M2QP']);
	});
});

describe('quick match', () => {
	it('goes from queued to seated the same way a plain request does', () => {
		const s = senders();
		const session = createRoomSession();

		session.request({ kind: 'quickMatch' });
		expect(session.flush(true, s)).toBe(true);
		expect(s.sent).toEqual(['quickMatch']);

		session.resetQueued();
		session.tickQueued();
		session.tickQueued();
		expect(session.state.queuedForS).toBe(2);

		// RoomState arriving is what a match found looks like from here.
		session.noteRoom();
		expect(session.state.pending).toBeNull();
	});

	it('counts seconds only from the moment counting starts', () => {
		const session = createRoomSession();
		session.tickQueued();
		expect(session.state.queuedForS).toBe(1);
		session.resetQueued();
		expect(session.state.queuedForS).toBe(0);
	});
});

describe('held room actions', () => {
	/** @param {boolean} accepted */
	function dispatcher(accepted = true) {
		/** @type {import('../src/lib/stores/room-session.svelte.js').RoomAction[]} */
		const sent = [];
		return {
			sent,
			/** @param {import('../src/lib/stores/room-session.svelte.js').RoomAction} action */
			dispatch: (action) => {
				sent.push(action);
				return accepted;
			}
		};
	}

	it('does nothing when nothing is held', () => {
		const d = dispatcher();
		const session = createRoomSession();

		expect(session.flushAction(true, d.dispatch)).toBe(false);
		expect(d.sent).toEqual([]);
	});

	it('resends a held action once the socket reopens', () => {
		const d = dispatcher();
		const session = createRoomSession();

		session.holdAction({ kind: 'setReady', ready: true });
		expect(session.flushAction(false, d.dispatch)).toBe(false);
		expect(d.sent).toEqual([]);

		expect(session.flushAction(true, d.dispatch)).toBe(true);
		expect(d.sent).toEqual([{ kind: 'setReady', ready: true }]);
		expect(session.state.heldAction).toBeNull();
	});

	it('keeps retrying a held action the server keeps refusing', () => {
		const d = dispatcher(false);
		const session = createRoomSession();

		session.holdAction({ kind: 'startGame' });
		expect(session.flushAction(true, d.dispatch)).toBe(false);
		expect(session.flushAction(true, d.dispatch)).toBe(false);

		expect(d.sent).toEqual([{ kind: 'startGame' }, { kind: 'startGame' }]);
	});

	it('replaces an unset held action with the player\'s later intent', () => {
		// Readying, then leaving before either reaches the server: only the
		// leave should still be waiting to go out.
		const d = dispatcher();
		const session = createRoomSession();

		session.holdAction({ kind: 'setReady', ready: true });
		session.holdAction({ kind: 'leaveRoom' });
		session.flushAction(true, d.dispatch);

		expect(d.sent).toEqual([{ kind: 'leaveRoom' }]);
	});

	it('drops a held action when the screen is torn down before it can be sent', () => {
		const d = dispatcher();
		const session = createRoomSession();

		session.holdAction({ kind: 'cancelQueue' });
		session.clearAction();

		expect(session.flushAction(true, d.dispatch)).toBe(false);
		expect(d.sent).toEqual([]);
	});
});
