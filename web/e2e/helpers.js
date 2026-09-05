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
		submit: page.getByRole('button', { name: 'Gửi', exact: true }),
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
 * Reads the words currently in the chain, opening word included.
 *
 * @param {import('@playwright/test').Page} page
 * @returns {Promise<string[]>}
 */
export async function chainWords(page) {
	return page.locator('ol li .word').allTextContents();
}
