// @vitest-environment jsdom

// Settings are the only state the server has no opinion about, so the risk
// here is not correctness of the values but survival when storage refuses.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { Difficulty } from '../src/lib/proto/noitu/v1/game_pb.js';
import { MAX_NICKNAME_LENGTH, createSettingsStore } from '../src/lib/stores/settings.svelte.js';

const realStorage = globalThis.localStorage;

/** Replaces localStorage for one test. */
function useStorage(stub) {
	Object.defineProperty(globalThis, 'localStorage', {
		value: stub,
		configurable: true,
		writable: true
	});
}

/** A storage that fails every operation, like a browser blocking site data. */
function hostileStorage() {
	const boom = () => {
		throw new DOMException('denied', 'SecurityError');
	};
	return { getItem: boom, setItem: boom, removeItem: boom, clear: boom, key: boom, length: 0 };
}

beforeEach(() => {
	useStorage(realStorage);
	localStorage.clear();
});

afterEach(() => {
	useStorage(realStorage);
});

describe('nickname', () => {
	it('persists what the player typed', () => {
		const store = createSettingsStore();
		store.setNickname('Minh');

		expect(store.state.nickname).toBe('Minh');
		expect(createSettingsStore().state.nickname).toBe('Minh');
	});

	it('caps at the server limit, counting characters rather than bytes', () => {
		const store = createSettingsStore();
		// Vietnamese is multi-byte; a byte cap would cut this far shorter.
		store.setNickname('ăăăăăăăăăăăăăăăăăăăăăăăăă');

		expect([...store.state.nickname]).toHaveLength(MAX_NICKNAME_LENGTH);
	});
});

describe('theme', () => {
	it('persists an explicit choice', () => {
		const store = createSettingsStore();
		store.setTheme('dark');

		expect(store.state.theme).toBe('dark');
		expect(createSettingsStore().state.theme).toBe('dark');
	});

	it('applies the theme to the document so the page repaints', () => {
		const store = createSettingsStore();
		store.setTheme('dark');
		expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
	});

	it('toggles between the two themes', () => {
		const store = createSettingsStore();
		store.setTheme('light');
		store.toggleTheme();
		expect(store.state.theme).toBe('dark');
		store.toggleTheme();
		expect(store.state.theme).toBe('light');
	});

	it('treats an unrecognised stored value as light rather than trusting it', () => {
		localStorage.setItem('noitu.theme', 'neon');
		expect(createSettingsStore().state.theme).toBe('light');
	});
});

describe('best scores', () => {
	it('records a first score as a new record', () => {
		const store = createSettingsStore();
		expect(store.recordScore(Difficulty.EASY, 30)).toBe(true);
		expect(store.bestScore(Difficulty.EASY)).toBe(30);
	});

	it('keeps records separate per difficulty', () => {
		const store = createSettingsStore();
		store.recordScore(Difficulty.EASY, 30);
		store.recordScore(Difficulty.HARD, 12);

		expect(store.bestScore(Difficulty.EASY)).toBe(30);
		expect(store.bestScore(Difficulty.HARD)).toBe(12);
		expect(store.bestScore(Difficulty.MEDIUM)).toBe(0);
	});

	it('does not call a tie a new record', () => {
		const store = createSettingsStore();
		store.recordScore(Difficulty.EASY, 30);
		expect(store.recordScore(Difficulty.EASY, 30)).toBe(false);
	});

	it('leaves the record alone when a later game scores lower', () => {
		const store = createSettingsStore();
		store.recordScore(Difficulty.EASY, 30);
		expect(store.recordScore(Difficulty.EASY, 10)).toBe(false);
		expect(store.bestScore(Difficulty.EASY)).toBe(30);
	});

	it('survives a reload', () => {
		createSettingsStore().recordScore(Difficulty.MEDIUM, 44);
		expect(createSettingsStore().bestScore(Difficulty.MEDIUM)).toBe(44);
	});

	it('degrades to no records when the stored value is not valid JSON', () => {
		localStorage.setItem('noitu.bestScores', '{not json');
		const store = createSettingsStore();
		expect(store.bestScore(Difficulty.EASY)).toBe(0);
	});

	it('ignores entries that are not finite non-negative numbers', () => {
		localStorage.setItem(
			'noitu.bestScores',
			JSON.stringify({ 1: 10, 2: 'lots', 3: -5, 4: null })
		);
		const store = createSettingsStore();

		expect(store.bestScore(1)).toBe(10);
		expect(store.bestScore(2)).toBe(0);
		expect(store.bestScore(3)).toBe(0);
		expect(store.bestScore(4)).toBe(0);
	});
});

describe('when storage is unavailable', () => {
	it('constructs with working defaults instead of throwing', () => {
		useStorage(hostileStorage());
		const store = createSettingsStore();

		expect(store.state.nickname).toBe('');
		expect(store.state.bestScores).toEqual({});
		expect(['light', 'dark']).toContain(store.state.theme);
	});

	it('keeps every setting usable in memory for the rest of the session', () => {
		useStorage(hostileStorage());
		const store = createSettingsStore();

		expect(() => store.setNickname('Minh')).not.toThrow();
		expect(() => store.setTheme('dark')).not.toThrow();
		expect(store.recordScore(Difficulty.HARD, 7)).toBe(true);

		expect(store.state.nickname).toBe('Minh');
		expect(store.state.theme).toBe('dark');
		expect(store.bestScore(Difficulty.HARD)).toBe(7);
	});

	it('survives a browser that throws on the property itself', () => {
		Object.defineProperty(globalThis, 'localStorage', {
			configurable: true,
			get() {
				throw new DOMException('denied', 'SecurityError');
			}
		});

		expect(() => createSettingsStore()).not.toThrow();
	});
});
