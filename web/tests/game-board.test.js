// @vitest-environment jsdom

// The chat pill in the board's top row only exists for the online screen —
// a bot game has no chat, so GameBoard simply never receives the handler
// that draws it. This pins that down as a prop contract rather than
// something only the online route happens to exercise.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { unmount, flushSync } from 'svelte';
import GameBoard from '../src/lib/components/GameBoard.svelte';
import { game } from '../src/lib/stores/game.svelte.js';
import { Status, connection } from '../src/lib/ws/connection.svelte.js';
import { receive, render, startGame } from './component-support.js';

/** @param {Partial<ConstructorParameters<typeof GameBoard>[0]['props']>} [extra] */
function renderGameBoard(extra = {}) {
	return render(GameBoard, {
		onsubmit: () => true,
		onresign: () => {},
		onclaimdeadend: () => {},
		onreportword: () => {},
		...extra
	});
}

beforeEach(() => {
	game.leave();
	connection.status = Status.OPEN;
	// jsdom implements neither; ChainHistory (rendered inside the board) uses
	// both to keep the newest word in view, which is not what this file
	// tests.
	Element.prototype.scrollTo = () => {};
	globalThis.ResizeObserver ??= class {
		observe() {}
		disconnect() {}
	};
	startGame();
});

afterEach(() => {
	document.body.innerHTML = '';
	connection.status = Status.CLOSED;
});

describe('the chat pill', () => {
	it('is absent when the caller passes no onchatopen, as a bot game does', () => {
		const { target, component } = renderGameBoard();

		expect(target.querySelector('[data-testid="chat-pill"]')).toBeNull();
		unmount(component);
	});

	it('appears once a handler is passed, as the online room does', () => {
		const { target, component } = renderGameBoard({ onchatopen: () => {} });

		expect(target.querySelector('[data-testid="chat-pill"]')).not.toBeNull();
		unmount(component);
	});

	it('calls the handler on click and shows the unread count', () => {
		let opened = 0;
		const { target, component } = renderGameBoard({
			onchatopen: () => opened++,
			chatUnread: 3
		});

		/** @type {HTMLButtonElement | null} */
		const pill = target.querySelector('[data-testid="chat-pill"]');
		expect(pill?.textContent).toContain('3');
		pill?.click();

		expect(opened).toBe(1);
		unmount(component);
	});
});

describe('the persistent claim/resign row', () => {
	// "Bí từ" used to mount only on the player's own turn, shifting everything
	// under the input each handover. Both controls now stay mounted for the
	// whole game and are disabled off-turn instead, so the row's height never
	// changes turn to turn.
	it('keeps both buttons mounted, enabled, on the player\'s own turn', () => {
		const { target, component } = renderGameBoard();

		/** @type {HTMLButtonElement | null} */
		const claim = target.querySelector('.claim-dead-end');
		/** @type {HTMLButtonElement | null} */
		const resign = target.querySelector('.resign');

		expect(claim).not.toBeNull();
		expect(resign).not.toBeNull();
		expect(claim?.disabled).toBe(false);
		expect(resign?.disabled).toBe(false);
		unmount(component);
	});

	it('disables rather than unmounts both once the turn moves on', () => {
		const { target, component } = renderGameBoard();

		receive('turnUpdate', {
			currentSyllable: 'yên',
			myTurn: false,
			turnSeq: 2,
			chainLength: 1,
			players: [{ playerId: 'p1', name: 'Minh', isMe: true, connected: true }],
			turnPlayerId: 'p2'
		});
		flushSync();

		/** @type {HTMLButtonElement | null} */
		const claim = target.querySelector('.claim-dead-end');
		/** @type {HTMLButtonElement | null} */
		const resign = target.querySelector('.resign');

		expect(claim).not.toBeNull();
		expect(resign).not.toBeNull();
		expect(claim?.disabled).toBe(true);
		expect(resign?.disabled).toBe(true);
		unmount(component);
	});
});

describe('refusals', () => {
	it('shows a room-wide error with a button that dismisses it', () => {
		const { target, component } = renderGameBoard();

		receive('error', { code: 'too_fast' });
		flushSync();
		/** @type {HTMLButtonElement | null} */
		const dismiss = target.querySelector('[role="alert"] button');
		expect(target.querySelector('[role="alert"]')?.textContent).toContain('Thao tác quá nhanh');
		dismiss?.click();
		flushSync();

		expect(game.state.error).toBeNull();
		expect(target.querySelector('[role="alert"]')).toBeNull();
		unmount(component);
	});

	it('answers a refused claim beside the button rather than in the top banner', () => {
		const { target, component } = renderGameBoard();

		receive('error', { code: 'not_a_dead_end' });
		flushSync();

		expect(game.state.error).toBeNull();
		const alert = target.querySelector('[role="alert"]');
		expect(alert?.previousElementSibling?.classList.contains('secondary')).toBe(true);
		alert?.querySelector('button')?.click();
		flushSync();

		expect(game.state.claimError).toBeNull();
		unmount(component);
	});
});
