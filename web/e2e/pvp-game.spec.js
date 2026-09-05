import { expect, test } from '@playwright/test';
import { board, chainWords, playLegalMove, setNickname, waitForMyTurn } from './helpers.js';

/**
 * Two browser contexts, so each player has their own storage, nickname and
 * socket. One context with two tabs would share a session and prove nothing
 * about two people playing.
 */
async function twoPlayers(browser) {
	const hostContext = await browser.newContext();
	const guestContext = await browser.newContext();
	return {
		host: await hostContext.newPage(),
		guest: await guestContext.newPage(),
		async close() {
			await hostContext.close();
			await guestContext.close();
		}
	};
}

/** Creates a room as the host and returns its code. */
async function createRoom(page, nickname) {
	await page.goto('/online');
	await setNickname(page, nickname);
	await page.getByRole('button', { name: 'Tạo phòng' }).click();

	const code = await page.getByTestId('room-code').textContent();
	return (code ?? '').replace(/\s+/g, '');
}

async function joinRoom(page, nickname, code) {
	await page.goto('/online');
	await setNickname(page, nickname);
	await page.getByLabel('Mã phòng').fill(code);
	await page.getByRole('button', { name: 'Vào phòng' }).click();
}

test.describe('playing a stranger', () => {
	test('two players join by code and alternate turns', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		expect(code).toMatch(/^[23456789ABCDEFGHJKMNPQRSTUVWXYZ]{6}$/);
		await expect(host.getByTestId('waiting-status')).toBeVisible();

		await joinRoom(guest, 'Lan', code);

		// Both boards come up, and exactly one player is on turn.
		await waitForMyTurn(host);
		await expect(board(guest).turn).toHaveText('Đối thủ đang suy nghĩ…');

		// Each side is shown the other's server-sanitized name.
		await expect(host.locator('.who', { hasText: 'Lan' })).toBeVisible();
		await expect(guest.locator('.who', { hasText: 'Minh' })).toBeVisible();

		const used = new Set(await chainWords(host));
		const hostWord = await playLegalMove(host, used);

		// The move crosses to the other browser, and the turn goes with it.
		await expect(guest.getByText(hostWord, { exact: true }).first()).toBeVisible();
		await waitForMyTurn(guest);

		const guestWord = await playLegalMove(guest, used);
		await expect(host.getByText(guestWord, { exact: true }).first()).toBeVisible();
		await waitForMyTurn(host);

		await close();
	});

	test('an invite link opens straight into the room', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');

		// No code typed, no button pressed beyond opening the link.
		await guest.goto(`/online?code=${code}`);

		await waitForMyTurn(host);
		await expect(board(guest).syllable).toBeVisible();

		await close();
	});

	test('resigning ends the game on both sides with the right winner', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);
		await waitForMyTurn(host);

		host.on('dialog', (dialog) => dialog.accept());
		await host.getByRole('button', { name: 'Đầu hàng' }).click();

		await expect(host.getByRole('heading', { name: 'Bạn thua.' })).toBeVisible();
		await expect(guest.getByRole('heading', { name: 'Bạn thắng!' })).toBeVisible();

		await close();
	});

	test('a rematch restarts in the same room once both agree', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);
		await waitForMyTurn(host);

		host.on('dialog', (dialog) => dialog.accept());
		await host.getByRole('button', { name: 'Đầu hàng' }).click();
		await expect(guest.getByTestId('rematch-prompt')).toBeVisible();

		// One acceptance is not enough: the other player is still being asked.
		await host.getByTestId('rematch-accept').click();
		await expect(guest.getByTestId('rematch-prompt')).toContainText('Đối thủ muốn chơi lại');
		await expect(host.getByTestId('rematch-prompt')).toContainText('Đang chờ đối thủ');

		await guest.getByTestId('rematch-accept').click();

		// A new game in the same room: the code is unchanged and the board is
		// back to a single opening word.
		await waitForMyTurn(host);
		await expect(host.getByText(code)).toBeVisible();
		expect(await chainWords(host)).toHaveLength(1);

		await close();
	});

	test('an unknown room code is refused in Vietnamese', async ({ page }) => {
		await page.goto('/online');
		await setNickname(page, 'Minh');
		await page.getByLabel('Mã phòng').fill('ZZZZZZ');
		await page.getByRole('button', { name: 'Vào phòng' }).click();

		await expect(page.getByTestId('join-error')).toHaveText(
			'Không tìm thấy phòng với mã này.'
		);
	});

	test('a malformed code is caught before it reaches the server', async ({ page }) => {
		await page.goto('/online');
		await page.getByLabel('Mã phòng').fill('ABC');
		await page.getByRole('button', { name: 'Vào phòng' }).click();

		await expect(page.getByText('Mã phòng gồm sáu ký tự.')).toBeVisible();
		await expect(page.getByTestId('join-error')).toHaveCount(0);
	});

	test('a full room turns a third player away', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);
		await waitForMyTurn(host);

		const thirdContext = await browser.newContext();
		const third = await thirdContext.newPage();
		await joinRoom(third, 'Nam', code);

		await expect(third.getByTestId('join-error')).toHaveText('Phòng đã đủ người.');

		await thirdContext.close();
		await close();
	});
});
