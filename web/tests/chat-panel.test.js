// @vitest-environment jsdom

// ChatPanel's unread accounting is what broke CI once already (the review
// that asked for this file cites it by name), and until now nothing mounted
// the component to prove it. jsdom is already a devDependency; Svelte 5
// components compiled by the vite plugin mount directly under it with no
// extra library.

import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import { create } from '@bufbuild/protobuf';
import ChatPanel from '../src/lib/components/ChatPanel.svelte';
import { ServerMessageSchema } from '../src/lib/proto/noitu/v1/game_pb.js';
import { game } from '../src/lib/stores/game.svelte.js';

/** @param {{ author?: string, text?: string, fromMe?: boolean }} [fields] */
function receiveLine({ author = 'Lan', text = 'chào', fromMe = false } = {}) {
	game.apply(
		create(ServerMessageSchema, {
			payload: {
				case: 'chatMessage',
				value: { fromMe, author, text, playerId: 'p2', sentUnixMs: 1n }
			}
		})
	);
}

/** @param {ConstructorParameters<typeof ChatPanel>[0]['props']} props */
function renderChatPanel(props) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const component = mount(ChatPanel, { target, props });
	flushSync();
	return { target, component };
}

beforeEach(() => {
	game.clearChat();
	// jsdom does not implement scrollTo; the panel calls it to keep the
	// newest line in view, which is not what this file is testing.
	Element.prototype.scrollTo = () => {};
});

afterEach(() => {
	document.body.innerHTML = '';
});

describe('folding', () => {
	it('starts folded when collapsible, with no log or input on screen', () => {
		const { target } = renderChatPanel({ collapsible: true, onsend: () => {} });

		expect(target.querySelector('[data-testid="chat-toggle"]')).not.toBeNull();
		expect(target.querySelector('[data-testid="chat-log"]')).toBeNull();
		expect(target.querySelector('[data-testid="chat-input"]')).toBeNull();
	});

	it('opens on a toggle press and shows the log', () => {
		receiveLine();
		const { target } = renderChatPanel({ collapsible: true, onsend: () => {} });

		/** @type {HTMLButtonElement | null} */
		const toggle = target.querySelector('[data-testid="chat-toggle"]');
		toggle?.click();
		flushSync();

		expect(target.querySelector('[data-testid="chat-log"]')).not.toBeNull();
		expect(toggle?.getAttribute('aria-expanded')).toBe('true');
	});

	it('never folds when not collapsible, regardless of the toggle', () => {
		const { target } = renderChatPanel({ collapsible: false, onsend: () => {} });

		expect(target.querySelector('[data-testid="chat-toggle"]')).toBeNull();
		expect(target.querySelector('[data-testid="chat-input"]')).not.toBeNull();
	});
});

describe('unread count', () => {
	it('counts a line that arrives while folded', () => {
		const { target } = renderChatPanel({ collapsible: true, onsend: () => {} });

		receiveLine({ text: 'một' });
		flushSync();

		expect(target.querySelector('[data-testid="chat-unread"]')?.textContent).toContain('1');
	});

	it('clears to zero once the panel is opened', () => {
		receiveLine({ text: 'một' });
		const { target } = renderChatPanel({ collapsible: true, onsend: () => {} });
		flushSync();

		/** @type {HTMLButtonElement | null} */
		const toggle = target.querySelector('[data-testid="chat-toggle"]');
		toggle?.click();
		flushSync();

		expect(target.querySelector('[data-testid="chat-unread"]')).toBeNull();
	});

	it('does not count anything while the panel is already open', () => {
		const { target } = renderChatPanel({ collapsible: false, onsend: () => {} });

		receiveLine({ text: 'một' });
		flushSync();

		expect(target.querySelector('[data-testid="chat-unread"]')).toBeNull();
	});

	it('resumes counting once folded again after having been read', () => {
		const { target, component } = renderChatPanel({ collapsible: true, onsend: () => {} });
		/** @type {HTMLButtonElement | null} */
		const toggle = target.querySelector('[data-testid="chat-toggle"]');

		receiveLine({ text: 'một' });
		toggle?.click(); // opens, marks it read
		flushSync();
		toggle?.click(); // folds again
		flushSync();
		receiveLine({ text: 'hai' });
		flushSync();

		expect(target.querySelector('[data-testid="chat-unread"]')?.textContent).toContain('1');
		unmount(component);
	});
});

describe('sending', () => {
	it('reports the typed text and clears the field on submit', () => {
		/** @type {string[]} */
		const sent = [];
		const { target } = renderChatPanel({ collapsible: false, onsend: (text) => sent.push(text) });

		/** @type {HTMLInputElement | null} */
		const input = target.querySelector('[data-testid="chat-input"]');
		if (input) {
			input.value = 'xin chào';
			input.dispatchEvent(new Event('input', { bubbles: true }));
		}
		flushSync();

		/** @type {HTMLButtonElement | null} */
		const send = target.querySelector('[data-testid="chat-send"]');
		expect(send?.disabled).toBe(false);
		target.querySelector('form')?.requestSubmit();
		flushSync();

		expect(sent).toEqual(['xin chào']);
		expect(input?.value).toBe('');
	});

	it('refuses a message that is only whitespace', () => {
		/** @type {string[]} */
		const sent = [];
		const { target } = renderChatPanel({ collapsible: false, onsend: (text) => sent.push(text) });

		/** @type {HTMLInputElement | null} */
		const input = target.querySelector('[data-testid="chat-input"]');
		if (input) {
			input.value = '   ';
			input.dispatchEvent(new Event('input', { bubbles: true }));
		}
		flushSync();

		expect(/** @type {HTMLButtonElement | null} */ (target.querySelector('[data-testid="chat-send"]'))?.disabled).toBe(
			true
		);
		expect(sent).toEqual([]);
	});
});
