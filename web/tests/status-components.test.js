// @vitest-environment jsdom

// The small display components whose logic is worth pinning: what the clock
// shows and when it turns urgent, what the away banner announces, what the
// game-over panel shows and whether it may take focus, and the two-press
// button. Mounted directly under jsdom, as the other component suites are.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync, mount, unmount } from 'svelte';
import ArmedButton from '../src/lib/components/ArmedButton.svelte';
import CountdownRing from '../src/lib/components/CountdownRing.svelte';
import GameOverPanel from '../src/lib/components/GameOverPanel.svelte';
import PlayerStatus from '../src/lib/components/PlayerStatus.svelte';
import { GameEndReason } from '../src/lib/proto/noitu/v1/game_pb.js';
import { game } from '../src/lib/stores/game.svelte.js';
import { Status, connection } from '../src/lib/ws/connection.svelte.js';
import { receive, render, startGame } from './component-support.js';
import { reactiveProps } from './reactive-props.svelte.js';

beforeEach(() => {
	game.leave();
	connection.status = Status.OPEN;
});

afterEach(() => {
	document.body.innerHTML = '';
	connection.status = Status.CLOSED;
	vi.useRealTimers();
	vi.restoreAllMocks();
});

/**
 * Starts a game whose turn ends `ms` from the frozen clock.
 * @param {{ ms: number, myTurn: boolean }} args
 */
function turnEndingIn({ ms, myTurn }) {
	receive('gameStarted', {
		openingWord: 'bình yên',
		currentSyllable: 'yên',
		myTurn,
		// The ring stops the clock 300ms short of the server's deadline.
		deadlineUnixMs: BigInt(Date.now() + ms + 300),
		turnSeq: 1,
		turnLimitMs: 20_000,
		players: [
			{ playerId: 'p1', name: 'Minh', isMe: true, connected: true },
			{ playerId: 'p2', name: 'Lan', isMe: false, connected: true }
		],
		turnPlayerId: myTurn ? 'p1' : 'p2'
	});
}

describe('CountdownRing', () => {
	beforeEach(() => {
		vi.useFakeTimers({ toFake: ['Date'], now: 1_700_000_000_000 });
		// jsdom's frame loop would run against the real clock; the value is
		// read once at mount, which is what these assertions need.
		vi.stubGlobal('requestAnimationFrame', () => 1);
		vi.stubGlobal('cancelAnimationFrame', () => {});
	});

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	it('shows a dash and no urgency between games', () => {
		const { target, component } = render(CountdownRing, {});
		const ring = target.querySelector('[role="timer"]');

		expect(ring?.classList.contains('idle')).toBe(true);
		expect(target.querySelector('.value')?.textContent).toBe('–');
		unmount(component);
	});

	it('shows whole seconds, rounded up, and labels them for assistive tech', () => {
		turnEndingIn({ ms: 12_400, myTurn: true });
		const { target, component } = render(CountdownRing, {});

		expect(target.querySelector('.value')?.textContent).toBe('13');
		expect(target.querySelector('[role="timer"]')?.getAttribute('aria-label')).toBe('13 giây');
		unmount(component);
	});

	it('turns urgent in the last five seconds of the player\'s own turn only', () => {
		turnEndingIn({ ms: 4_000, myTurn: true });
		const mine = render(CountdownRing, {});
		expect(mine.target.querySelector('[role="timer"]')?.classList.contains('urgent')).toBe(true);
		unmount(mine.component);

		turnEndingIn({ ms: 4_000, myTurn: false });
		const theirs = render(CountdownRing, {});
		const ring = theirs.target.querySelector('[role="timer"]');
		expect(ring?.classList.contains('urgent')).toBe(false);
		expect(ring?.classList.contains('mine')).toBe(false);
		unmount(theirs.component);
	});

	it('says it is stalled, rather than urgent, while the socket is down', () => {
		turnEndingIn({ ms: 4_000, myTurn: true });
		connection.status = Status.RECONNECTING;
		const { target, component } = render(CountdownRing, {});
		const ring = target.querySelector('[role="timer"]');

		expect(ring?.classList.contains('stalled')).toBe(true);
		expect(ring?.classList.contains('urgent')).toBe(false);
		unmount(component);
	});

	it('speaks the ten and five second marks on the player\'s own turn, and never otherwise', () => {
		turnEndingIn({ ms: 9_500, myTurn: true });
		const mine = render(CountdownRing, {});
		expect(mine.target.querySelector('[role="status"]')?.textContent?.trim()).toBe(
			'Còn 10 giây cho lượt của bạn'
		);
		unmount(mine.component);

		turnEndingIn({ ms: 9_500, myTurn: false });
		const theirs = render(CountdownRing, {});
		expect(theirs.target.querySelector('[role="status"]')?.textContent?.trim()).toBe('');
		unmount(theirs.component);
	});
});

describe('PlayerStatus', () => {
	/** @param {boolean} connected */
	function roomWithLan(connected) {
		receive('roomState', {
			roomCode: 'ABCD',
			canStart: false,
			maxPlayers: 4,
			minPlayers: 2,
			graceMs: 30_000,
			players: [
				{ playerId: 'p1', name: 'Minh', isMe: true, connected: true },
				{ playerId: 'p2', name: 'Lan', isMe: false, connected }
			]
		});
		flushSync();
	}

	beforeEach(() => {
		vi.useFakeTimers({ now: 1_700_000_000_000 });
	});

	it('announces who dropped in a live region that does not hold the countdown', () => {
		roomWithLan(false);
		const { target, component } = render(PlayerStatus, {});
		flushSync();

		const banner = target.querySelector('[data-testid="away-p2"]');
		const live = banner?.querySelector('[role="status"]');
		expect(live?.textContent).toBe('Lan mất kết nối…');
		expect(live?.textContent).not.toMatch(/\d/);

		const counter = banner?.querySelector('[aria-hidden="true"]');
		expect(counter?.textContent).toBe('(30s)');
		unmount(component);
	});

	it('counts the grace window down, and drops the number at zero', () => {
		roomWithLan(false);
		const { target, component } = render(PlayerStatus, {});
		flushSync();

		vi.advanceTimersByTime(10_000);
		flushSync();
		expect(target.querySelector('[data-testid="away-p2"] [aria-hidden="true"]')?.textContent).toBe(
			'(20s)'
		);

		vi.advanceTimersByTime(25_000);
		flushSync();
		expect(target.querySelector('[data-testid="away-p2"] [aria-hidden="true"]')).toBeNull();
		expect(target.querySelector('[data-testid="away-p2"]')?.textContent).toContain('Lan mất kết nối');
		unmount(component);
	});

	it('takes the banner away when the player is back', () => {
		roomWithLan(false);
		const { target, component } = render(PlayerStatus, {});
		flushSync();
		expect(target.querySelector('[data-testid="away-p2"]')).not.toBeNull();

		roomWithLan(true);
		flushSync();

		expect(target.querySelector('[data-testid="away-p2"]')).toBeNull();
		unmount(component);
	});
});

/**
 * jsdom's Blob has no `text()`.
 * @param {Blob} blob
 * @returns {Promise<string>}
 */
function readText(blob) {
	return new Promise((resolve, reject) => {
		const reader = new FileReader();
		reader.onload = () => resolve(String(reader.result));
		reader.onerror = () => reject(reader.error);
		reader.readAsText(blob);
	});
}

describe('GameOverPanel', () => {
	/** @param {object} [fields] */
	function finish(fields = {}) {
		receive('gameOver', {
			iWon: true,
			reason: GameEndReason.TIMEOUT,
			chainLength: 6,
			standings: [
				{ playerId: 'p2', name: 'Lan', isMe: false, score: 9, rank: 2 },
				{ playerId: 'p1', name: 'Minh', isMe: true, score: 14, rank: 1 }
			],
			...fields
		});
		flushSync();
	}

	beforeEach(() => {
		startGame();
	});

	it('renders nothing until there is a result', () => {
		const { target, component } = render(GameOverPanel, { isRecord: false, onhome: () => {} });

		expect(target.querySelector('[role="group"]')).toBeNull();
		unmount(component);
	});

	it('lists the standings by rank with the winner marked', () => {
		finish();
		const { target, component } = render(GameOverPanel, { isRecord: false, onhome: () => {} });

		const rows = [...target.querySelectorAll('[data-testid="standings"] li')];
		expect(rows).toHaveLength(2);
		expect(rows.map((r) => r.querySelector('.rank')?.textContent)).toEqual(['2', '1']);
		expect(rows[1].classList.contains('winner')).toBe(true);
		expect(rows[1].classList.contains('me')).toBe(true);
		expect(rows[0].classList.contains('winner')).toBe(false);
		unmount(component);
	});

	it('shows no table for a one-row result', () => {
		finish({ standings: [{ playerId: 'p1', name: 'Minh', isMe: true, score: 3, rank: 1 }] });
		const { target, component } = render(GameOverPanel, { isRecord: false, onhome: () => {} });

		expect(target.querySelector('[data-testid="standings"]')).toBeNull();
		unmount(component);
	});

	it('takes focus when the game ends', () => {
		const { target, component } = render(GameOverPanel, { isRecord: false, onhome: () => {} });

		finish();

		expect(document.activeElement).toBe(target.querySelector('[role="group"]'));
		unmount(component);
	});

	it('leaves focus alone when the player is typing in another field', () => {
		const chat = document.createElement('input');
		document.body.appendChild(chat);
		chat.focus();
		const { component } = render(GameOverPanel, { isRecord: false, onhome: () => {} });

		finish();

		expect(document.activeElement).toBe(chat);
		unmount(component);
	});

	it('offers a rematch only when the screen supplies one', () => {
		finish();
		const without = render(GameOverPanel, { isRecord: false, onhome: () => {} });
		expect(without.target.querySelector('button.primary')).toBeNull();
		unmount(without.component);

		const onrematch = vi.fn();
		const withIt = render(GameOverPanel, { isRecord: false, onhome: () => {}, onrematch });
		withIt.target.querySelector('button.primary')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
		expect(onrematch).toHaveBeenCalledTimes(1);
		unmount(withIt.component);
	});

	it('hands the chain over as a text file when export is pressed', async () => {
		finish();
		/** @type {Blob | null} */
		let saved = null;
		URL.createObjectURL = (blob) => {
			saved = /** @type {Blob} */ (blob);
			return 'blob:test';
		};
		URL.revokeObjectURL = () => {};
		/** @type {string[]} */
		const names = [];
		vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function () {
			names.push(/** @type {HTMLAnchorElement} */ (this).download);
		});
		const { target, component } = render(GameOverPanel, { isRecord: false, onhome: () => {} });

		target.querySelector('button.export')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));

		expect(names).toHaveLength(1);
		expect(names[0]).toMatch(/^noi-tu-\d{4}-\d{2}-\d{2}-\d{4}\.txt$/);
		expect(await readText(/** @type {Blob} */ (saved))).toContain('bình yên');
		unmount(component);
	});
});

describe('ArmedButton', () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});

	/** @param {object} [extra] */
	function armed(extra = {}) {
		const onconfirm = vi.fn();
		const { target, component } = render(ArmedButton, {
			label: 'Đầu hàng',
			confirmLabel: 'Bấm lại để đầu hàng',
			onconfirm,
			...extra
		});
		/** @type {HTMLButtonElement} */
		const button = target.querySelector('button');
		const press = () => {
			button.click();
			flushSync();
		};
		return { button, press, onconfirm, component };
	}

	it('arms on the first press without confirming, and says so', () => {
		const { button, press, onconfirm, component } = armed();

		press();

		expect(onconfirm).not.toHaveBeenCalled();
		expect(button.getAttribute('aria-pressed')).toBe('true');
		expect(button.getAttribute('aria-label')).toBe('Bấm lại để đầu hàng');
		unmount(component);
	});

	it('confirms on the second press and disarms', () => {
		const { button, press, onconfirm, component } = armed();

		press();
		press();

		expect(onconfirm).toHaveBeenCalledTimes(1);
		expect(button.getAttribute('aria-pressed')).toBe('false');
		unmount(component);
	});

	it('disarms by itself after the arm window, so a stray press is not a confirmation', () => {
		const { button, press, onconfirm, component } = armed({ armMs: 1000 });

		press();
		vi.advanceTimersByTime(1000);
		flushSync();
		expect(button.getAttribute('aria-pressed')).toBe('false');

		press();
		expect(onconfirm).not.toHaveBeenCalled();
		unmount(component);
	});

	it('disarms the moment it is disabled, and needs two presses again afterwards', () => {
		const target = document.createElement('div');
		document.body.appendChild(target);
		const onconfirm = vi.fn();
		const props = reactiveProps({
			label: 'Mời ra',
			confirmLabel: 'Bấm lại để mời ra',
			onconfirm,
			disabled: false
		});
		const component = mount(ArmedButton, { target, props });
		flushSync();
		/** @type {HTMLButtonElement} */
		const button = target.querySelector('button');

		button.click();
		flushSync();
		expect(button.getAttribute('aria-pressed')).toBe('true');

		props.disabled = true;
		flushSync();
		expect(button.getAttribute('aria-pressed')).toBe('false');

		props.disabled = false;
		flushSync();
		button.click();
		flushSync();
		expect(onconfirm).not.toHaveBeenCalled();
		unmount(component);
	});
});
