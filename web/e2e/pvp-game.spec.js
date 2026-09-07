import { expect, test } from '@playwright/test';
import {
	board,
	chainWords,
	playLegalMove,
	readyAndStart,
	setNickname,
	waitForMyTurn
} from './helpers.js';

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

/** Seats a pair and plays them into a game, which is where most tests start. */
async function playingPair(browser) {
	const pair = await twoPlayers(browser);
	const code = await createRoom(pair.host, 'Minh');
	await joinRoom(pair.guest, 'Lan', code);
	await readyAndStart(pair.host, pair.guest);
	await waitForMyTurn(pair.host);
	return { ...pair, code };
}

test.describe('playing a stranger', () => {
	test('two players join by code and alternate turns', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		expect(code).toMatch(/^[23456789ABCDEFGHJKMNPQRSTUVWXYZ]{6}$/);
		await expect(host.getByText('Còn trống')).toBeVisible();

		await joinRoom(guest, 'Lan', code);
		await readyAndStart(host, guest);

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

		// The link seats them; the lobby is still where the game is agreed.
		await expect(guest.getByTestId('ready')).toBeVisible();
		await readyAndStart(host, guest);

		await waitForMyTurn(host);
		await expect(board(guest).syllable).toBeVisible();

		await close();
	});

	test('resigning ends the game on both sides with the right winner', async ({ browser }) => {
		const { host, guest, close } = await playingPair(browser);

		host.on('dialog', (dialog) => dialog.accept());
		await host.getByRole('button', { name: 'Đầu hàng' }).click();

		await expect(host.getByRole('heading', { name: 'Bạn thua.' })).toBeVisible();
		await expect(guest.getByRole('heading', { name: 'Bạn thắng!' })).toBeVisible();

		await close();
	});

	test('a second game is agreed in the lobby the first one ends in', async ({ browser }) => {
		const { host, guest, code, close } = await playingPair(browser);

		host.on('dialog', (dialog) => dialog.accept());
		await host.getByRole('button', { name: 'Đầu hàng' }).click();
		await expect(host.getByRole('heading', { name: 'Bạn thua.' })).toBeVisible();

		// The readiness that started the first game is spent, so the owner
		// cannot simply start another.
		await expect(host.getByTestId('start-game')).toBeDisabled();
		await readyAndStart(host, guest);

		// A new game in the same room: the code is unchanged and the board is
		// back to a single opening word.
		await waitForMyTurn(host);
		await expect(host.getByText(code)).toBeVisible();
		expect(await chainWords(host)).toHaveLength(1);

		await close();
	});

	test('the guest readies, and cannot leave without taking it back', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);

		// Nothing starts until the owner says so.
		await expect(host.getByTestId('start-game')).toBeDisabled();
		await guest.getByTestId('ready').click();

		await expect(guest.getByTestId('my-ready')).toHaveText('Đã sẵn sàng');
		await expect(host.getByTestId('opponent-ready')).toHaveText('Đã sẵn sàng');
		await expect(host.getByTestId('start-game')).toBeEnabled();

		// Ready is a commitment: the way out is to take it back first.
		await expect(guest.getByRole('button', { name: 'Rời phòng' })).toBeDisabled();
		await guest.getByTestId('ready').click();
		await expect(guest.getByRole('button', { name: 'Rời phòng' })).toBeEnabled();

		await guest.getByRole('button', { name: 'Rời phòng' }).click();

		// The room survives with its owner in it, one seat free.
		await expect(host.getByText('Còn trống')).toBeVisible();
		await expect(host.getByTestId('start-game')).toBeDisabled();
		// And the player who left is back on the join screen.
		await expect(guest.getByRole('button', { name: 'Tạo phòng' })).toBeVisible();

		await close();
	});

	test('the owner can put an unready guest out of the room', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);

		const kick = host.getByRole('button', { name: 'Mời ra khỏi phòng' });
		await expect(kick).toBeEnabled();

		// A guest who is ready is waiting on the owner, not in the way.
		await guest.getByTestId('ready').click();
		await expect(kick).toBeDisabled();

		await guest.getByTestId('ready').click();
		await expect(kick).toBeEnabled();
		host.on('dialog', (dialog) => dialog.accept());
		await kick.click();

		await expect(guest.getByTestId('join-error')).toHaveText('Bạn đã bị mời ra khỏi phòng.');
		await expect(host.getByText('Còn trống')).toBeVisible();

		await close();
	});

	test('the room outlives its owner, who hands it to whoever is left', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);
		await expect(guest.locator('.role', { hasText: 'Chủ phòng' })).toBeVisible();

		await host.getByRole('button', { name: 'Rời phòng' }).click();

		// The guest is the owner now, which is visible in what they are
		// offered rather than only in a label.
		await expect(guest.getByTestId('start-game')).toBeVisible();
		await expect(guest.getByText('Còn trống')).toBeVisible();

		// And the promotion is real: the next person in can be played against.
		const thirdContext = await browser.newContext();
		const third = await thirdContext.newPage();
		await joinRoom(third, 'Nam', code);
		await readyAndStart(guest, third);

		// Both boards come up. Which of them moves first is the engine's
		// business, and asserting it here would test seat order instead of the
		// promotion.
		await expect(board(guest).syllable).toBeVisible();
		await expect(board(third).syllable).toBeVisible();

		await thirdContext.close();
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
		const { host, code, close } = await playingPair(browser);
		void host;

		const thirdContext = await browser.newContext();
		const third = await thirdContext.newPage();
		await joinRoom(third, 'Nam', code);

		await expect(third.getByTestId('join-error')).toHaveText('Phòng đã đủ người.');

		await thirdContext.close();
		await close();
	});
});
