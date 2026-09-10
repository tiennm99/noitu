import { expect } from '@playwright/test';
import { continuation } from './fixture-dictionary.js';

/**
 * Shared page driving for the end-to-end specs.
 *
 * Everything here waits on a state the server produced — the turn indicator,
 * the syllable, a word appearing in the chain — rather than on a fixed delay.
 * A sleep would pass or fail depending on how loaded the machine is, which is
 * the usual reason a browser suite goes intermittently red.
 */

/** @param {import('@playwright/test').Page} page */
export function board(page) {
	return {
		syllable: page.getByTestId('current-syllable'),
		turn: page.getByTestId('turn-indicator'),
		input: page.getByRole('textbox', { name: 'Nhập từ của bạn' }),
		submit: page.getByTestId('word-submit'),
		chain: page.getByRole('list').first(),
		rejection: page.getByRole('alert')
	};
}

/**
 * Waits until it is this player's turn to move.
 *
 * @param {import('@playwright/test').Page} page
 */
export async function waitForMyTurn(page) {
	await expect(board(page).turn).toHaveText('Đến lượt bạn', { timeout: 30_000 });
}

/**
 * Waits until one of these players is on turn, and sorts them into the one who
 * has it and the ones who do not.
 *
 * Who moves first is drawn when the game starts, so a test that plays a move
 * has to ask rather than assume the room's owner. It polls the indicators
 * together instead of waiting on one page, because the state being waited for
 * belongs to the room and not to any single player.
 *
 * @param {import('@playwright/test').Page[]} pages
 * @returns {Promise<{lead: import('@playwright/test').Page, waits: import('@playwright/test').Page[]}>}
 */
export async function awaitTurn(...pages) {
	let onTurn = -1;
	await expect
		.poll(
			async () => {
				const shown = await Promise.all(
					pages.map((page) => board(page).turn.textContent().catch(() => null))
				);
				onTurn = shown.findIndex((text) => (text ?? '').trim() === 'Đến lượt bạn');
				return onTurn;
			},
			{ timeout: 30_000, message: 'nobody was dealt the turn' }
		)
		.toBeGreaterThanOrEqual(0);

	return { lead: pages[onTurn], waits: pages.filter((_, i) => i !== onTurn) };
}

/**
 * Types a word and sends it. The field is uncontrolled on purpose, so this
 * fills and submits exactly as a player would.
 *
 * @param {import('@playwright/test').Page} page
 * @param {string} word
 */
export async function submitWord(page, word) {
	const b = board(page);
	await b.input.fill(word);
	await b.submit.click();
}

/**
 * Plays one legal move for whatever syllable is currently required, and returns
 * the word it played.
 *
 * @param {import('@playwright/test').Page} page
 * @param {Set<string>} used
 * @returns {Promise<string>}
 */
export async function playLegalMove(page, used) {
	await waitForMyTurn(page);

	const syllable = (await board(page).syllable.textContent())?.trim() ?? '';
	const word = continuation(syllable, used);
	expect(word, `the fixture has no unused continuation of "${syllable}"`).toBeTruthy();

	used.add(/** @type {string} */ (word));
	await submitWord(page, /** @type {string} */ (word));
	await expect(page.getByText(/** @type {string} */ (word), { exact: true }).first()).toBeVisible();
	return /** @type {string} */ (word);
}

/**
 * The chat panel's parts, for the tests that drive a conversation.
 *
 * @param {import('@playwright/test').Page} page
 */
export function chat(page) {
	return {
		log: page.getByTestId('chat-log'),
		input: page.getByTestId('chat-input'),
		send: page.getByTestId('chat-send'),
		unread: page.getByTestId('chat-unread')
	};
}

/**
 * Types a line and sends it, the way a player does.
 *
 * @param {import('@playwright/test').Page} page
 * @param {string} text
 */
export async function say(page, text) {
	const c = chat(page);
	await c.input.fill(text);
	await c.send.click();
}

/**
 * Takes a seated pair from their lobby into a game: the guest readies, the
 * owner starts. Nothing begins on its own now, so every online test that is
 * about a game goes through here.
 *
 * @param {import('@playwright/test').Page} owner
 * @param {import('@playwright/test').Page} guest
 */
export async function readyAndStart(owner, ...guests) {
	for (const guest of guests) {
		await guest.getByTestId('ready').click();
	}
	const start = owner.getByTestId('start-game');
	await expect(start).toBeEnabled();
	await start.click();
}

/**
 * The seat rows in the lobby, one per player who is actually in the room. The
 * free seats are drawn too, so counting rows would count the room's size
 * rather than its occupants.
 *
 * @param {import('@playwright/test').Page} page
 */
export function seats(page) {
	return page.locator('.seat:not(.empty)');
}

/**
 * Picks a difficulty by clicking its card.
 *
 * The radio itself is visually hidden so the platform supplies arrow-key
 * behaviour, so the card is what a player actually clicks.
 *
 * @param {import('@playwright/test').Page} page
 * @param {string | RegExp} label
 */
export async function chooseDifficulty(page, label) {
	await page.locator('label', { has: page.getByRole('radio', { name: label }) }).click();
	await expect(page.getByRole('radio', { name: label })).toBeChecked();
}

/**
 * Sets the nickname on the home screen.
 *
 * @param {import('@playwright/test').Page} page
 * @param {string} name
 */
export async function setNickname(page, name) {
	await page.getByLabel('Tên của bạn').fill(name);
}

/**
 * Gives up the game.
 *
 * Two presses of the same button: the confirmation is inline now, because a
 * native confirm() blocks the frame loop the countdown ring runs on and could
 * cost the turn it was protecting. The accessible name still contains "Đầu
 * hàng" in both states, so one locator drives both presses.
 *
 * @param {import('@playwright/test').Page} page
 */
export async function resign(page) {
	const button = page.getByRole('button', { name: 'Đầu hàng' });
	await button.click();
	await button.click();
}

/**
 * Puts a player out of the room. Two presses, for the same reason as resign.
 *
 * @param {import('@playwright/test').Locator} kick
 */
export async function confirmKick(kick) {
	await kick.click();
	await kick.click();
}

/**
 * Reads the words currently in the chain, opening word included.
 *
 * @param {import('@playwright/test').Page} page
 * @returns {Promise<string[]>}
 */
export async function chainWords(page) {
	return page.locator('ol li .word').allTextContents();
}

/**
 * Reads the words whose meaning panel is open.
 *
 * @param {import('@playwright/test').Page} page
 * @returns {Promise<string[]>}
 */
export async function openMeanings(page) {
	return page.locator('ol li:has(> .meanings) .word').allTextContents();
}
