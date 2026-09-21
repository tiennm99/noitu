/**
 * Owns the /online screen's request machine: what the player has asked for
 * that the socket has not yet carried, from opening a room through leaving
 * one. Modelled on bot-session.svelte.js and for the same reason — a request
 * is a thing the player did, not a condition inferred from the board, so it
 * is stored as one rather than re-derived from `game.state`.
 *
 * No DOM and no runes beyond `$state`: every timer (the resume time-box, the
 * stall timer, the quick-match elapsed clock) is a plain `setTimeout` or
 * `setInterval` owned by the page's own `$effect`s, which call back into the
 * plain setters here. That is what makes this testable under Vitest with no
 * Svelte runtime involved, exactly as `ws/client.js` already is.
 */

/**
 * What the player asked for, held until the socket can carry it.
 * @typedef {{ kind: 'create' } | { kind: 'join', code: string } | { kind: 'quickMatch' }} RoomRequest
 */

/**
 * A request made from inside a room that the socket refused to carry.
 * Replacing rather than queuing: a later action is a later expression of the
 * same intent (readying and then leaving before reconnecting means the
 * leave is what should happen), and every one of these is safe to resend —
 * `SetReady`, `StartGame`, `KickPlayer`, `LeaveRoom` and `CancelQuickMatch`
 * are all refused rather than misapplied if they no longer make sense by the
 * time they land.
 * @typedef {{ kind: 'cancelQueue' } | { kind: 'leaveRoom' } | { kind: 'setReady', ready: boolean } | { kind: 'startGame' } | { kind: 'kickPlayer', playerId: string }} RoomAction
 */

export function createRoomSession() {
	const state = $state({
		/** @type {RoomRequest | null} */
		pending: null,
		// True while the only reason this screen has a socket is to reclaim a
		// game it might no longer be able to reclaim.
		resuming: false,
		// An invite link arrived before this player had a name.
		needName: false,
		// A held request that has been waiting on a socket for longer than a
		// player will believe.
		stalled: false,
		// The resume timed out (or was refused) rather than succeeding. Shown
		// instead of leaving the join form's own "connecting" label the only
		// sign anything happened.
		resumeFailed: false,
		// Whole seconds spent in the quick-match queue, for the waiting
		// panel's elapsed time and the bot nudge. Owned here rather than left
		// as a raw interval in the page, alongside everything else this
		// screen is waiting on.
		queuedForS: 0,
		/** @type {RoomAction | null} */
		heldAction: null
	});

	return {
		state,

		/**
		 * Queues a request to open or join a room. Nothing is sent until the
		 * socket is open, which the caller still has to drive with `connect()`
		 * and `flush()` — this only records the intent.
		 * @param {RoomRequest} req
		 */
		request(req) {
			state.pending = req;
			state.resuming = false;
			state.needName = false;
			state.stalled = false;
			state.resumeFailed = false;
		},

		/** Drops a queued request without sending it. */
		clearPending() {
			state.pending = null;
		},

		/**
		 * Holds a join behind a resume already in progress, without touching
		 * the other latches `request()` resets — the resume owns those while
		 * it runs, and is what the held join is waiting on.
		 * @param {RoomRequest} req
		 */
		holdPendingJoin(req) {
			state.pending = req;
		},

		/** @param {boolean} value */
		setNeedName(value) {
			state.needName = value;
		},

		/**
		 * Sends the queued request if there is one and the socket can carry
		 * it. Nothing goes out while a resume is in flight: the seat this tab
		 * is reclaiming may be in the very room the held request names.
		 * @param {boolean} isOpen
		 * @param {{
		 *   create: () => boolean,
		 *   join: (code: string) => boolean,
		 *   quickMatch: () => boolean
		 * }} senders
		 * @returns {boolean} whether a request was sent
		 */
		flush(isOpen, senders) {
			if (!state.pending || !isOpen || state.resuming) return false;
			const req = state.pending;
			const sent =
				req.kind === 'create'
					? senders.create()
					: req.kind === 'quickMatch'
						? senders.quickMatch()
						: senders.join(req.code);
			if (sent) state.pending = null;
			return sent;
		},

		/** Marks a resume attempt as starting, optionally behind a held join. */
		startResume() {
			state.resuming = true;
		},

		/**
		 * The resume worked, so nothing that happens from here is its fault —
		 * and an invite code held behind it has been answered by arriving in a
		 * room.
		 */
		noteRoom() {
			state.resuming = false;
			state.pending = null;
			state.resumeFailed = false;
		},

		/**
		 * A resume that the server refused, or that ran out the client's own
		 * patience for. Neither is something the player did, so the token is
		 * dropped and an invite code held behind it is either spent (the
		 * player already has a name) or turned into asking for one.
		 * @param {boolean} named
		 */
		noteResumeFailed(named) {
			if (!state.resuming) return;
			state.resuming = false;
			state.resumeFailed = true;
			if (state.pending && !named) {
				state.needName = true;
				state.pending = null;
			}
		},

		/** @param {boolean} value */
		setStalled(value) {
			state.stalled = value;
		},

		resetQueued() {
			state.queuedForS = 0;
		},

		tickQueued() {
			state.queuedForS += 1;
		},

		/**
		 * Holds a request the socket refused, replacing whatever this screen
		 * was already waiting to retry: a later action is a later statement of
		 * intent, and every action here is safe to resend regardless of order.
		 * @param {RoomAction} action
		 */
		holdAction(action) {
			state.heldAction = action;
		},

		/** Drops a held action without sending it, for a screen being torn down. */
		clearAction() {
			state.heldAction = null;
		},

		/**
		 * Resends a held action once the socket can carry it.
		 * @param {boolean} isOpen
		 * @param {(action: RoomAction) => boolean} dispatch
		 * @returns {boolean} whether the held action was sent
		 */
		flushAction(isOpen, dispatch) {
			if (!state.heldAction || !isOpen) return false;
			const sent = dispatch(state.heldAction);
			if (sent) state.heldAction = null;
			return sent;
		}
	};
}
