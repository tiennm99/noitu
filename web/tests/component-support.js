// Shared set-up for the suites that mount a component under jsdom. Not a
// test file itself: Vitest only collects `*.test.js`.

import { flushSync, mount } from 'svelte';
import { create } from '@bufbuild/protobuf';
import { ServerMessageSchema } from '../src/lib/proto/noitu/v1/game_pb.js';
import { game } from '../src/lib/stores/game.svelte.js';

/**
 * Feeds the shared store one decoded server message, as the socket would.
 * @param {string} kind - the payload oneof case
 * @param {object} value
 */
export function receive(kind, value) {
	game.apply(
		create(ServerMessageSchema, { payload: { case: kind, value } })
	);
}

/**
 * Starts a one-seat game with this player on turn.
 * @param {{ turnSeq?: number, currentSyllable?: string }} [fields]
 */
export function startGame({ turnSeq = 1, currentSyllable = 'yên' } = {}) {
	receive('gameStarted', {
		openingWord: 'bình yên',
		currentSyllable,
		myTurn: true,
		deadlineUnixMs: 1_700_000_020_000n,
		turnSeq,
		turnLimitMs: 20_000,
		players: [{ playerId: 'p1', name: 'Minh', isMe: true, connected: true }],
		turnPlayerId: 'p1'
	});
}

/**
 * Mounts a component into a fresh element on the page and settles it.
 * @template {Record<string, unknown>} Props
 * @param {import('svelte').Component<Props>} component
 * @param {Props} props
 */
export function render(component, props) {
	const target = document.createElement('div');
	document.body.appendChild(target);
	const instance = mount(component, { target, props });
	flushSync();
	return { target, component: instance };
}
