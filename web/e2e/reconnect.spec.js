import { expect, test } from '@playwright/test';
import {
	board,
	chainWords,
	playLegalMove,
	readyAndStart,
	setNickname,
	waitForMyTurn
} from './helpers.js';
import { cuttableSocket } from './socket-cut.js';

/**
 * The reconnect path, driven by cutting one player's socket.
 *
 * The cut is abrupt and unannounced, which is the case the grace window exists
 * for: a player who closes the tab politely is a different, easier story.
 */

/**
 * Seats two players in a room and returns them with the room code.
 *
 * @param {import('@playwright/test').Browser} browser
 */
async function pvpRoom(browser) {
	const hostContext = await browser.newContext();
	const guestContext = await browser.newContext();
	const host = await hostContext.newPage();
	const guest = await guestContext.newPage();

	await host.goto('/online');
	await setNickname(host, 'Minh');
	await host.getByRole('button', { name: 'Tạo phòng' }).click();
	const code = ((await host.getByTestId('room-code').textContent()) ?? '').replace(/\s+/g, '');

	await guest.goto('/online');
	await setNickname(guest, 'Lan');
	await guest.getByLabel('Mã phòng').fill(code);
	await guest.getByRole('button', { name: 'Vào phòng' }).click();
	await readyAndStart(host, guest);
	await waitForMyTurn(host);

	return {
		host,
		guest,
		code,
		async close() {
			await hostContext.close();
			await guestContext.close();
		}
	};
}

test.describe('losing the connection', () => {
	test('the opponent is told, and a return inside the window resumes the game', async ({
		browser
	}) => {
		const { host, guest, close } = await pvpRoom(browser);

		const used = new Set(await chainWords(host));
		await playLegalMove(host, used);
		await waitForMyTurn(guest);

		const hostSyllable = (await board(host).syllable.textContent())?.trim() ?? '';

		// Navigating away takes the socket with it, which is what the server
		// sees when a player loses their connection. The resume token lives in
		// this tab's session storage, so coming back can reclaim the seat.
		await guest.goto('about:blank');

		await expect(host.getByTestId('away-p2')).toContainText('Lan mất kết nối', {
			timeout: 20_000
		});

		// Back inside the grace window.
		await guest.goto('/online');

		// The seat is restored: the host stops waiting, and the returning player
		// is looking at the same position rather than the lobby.
		await expect(host.getByTestId('away-p2')).toHaveCount(0, { timeout: 20_000 });
		await expect(board(guest).syllable).toHaveText(hostSyllable, { timeout: 20_000 });
		await expect(board(guest).turn).toHaveText('Đến lượt bạn');

		await close();
	});

	test('an opponent who never comes back forfeits the game', async ({ browser }) => {
		const { host, guest, close } = await pvpRoom(browser);

		// Closing the tab: the socket goes and nothing reconnects, so the grace
		// window has to expire on its own.
		await guest.close();

		// NOITU_GRACE is 25s on the test server, so this waits it out.
		await expect(host.getByRole('heading', { name: 'Bạn thắng!' })).toBeVisible({
			timeout: 45_000
		});
		await expect(host.getByText('Có người đã rời trận.')).toBeVisible();

		await close();
	});

	test('a word cannot be typed into a connection that is down', async ({ browser }) => {
		const context = await browser.newContext();
		const page = await context.newPage();
		const socket = await cuttableSocket(page);

		await page.goto('/play?difficulty=1');
		await waitForMyTurn(page);

		await socket.cut({ sustained: true });

		await expect(page.getByText('Mất kết nối, đang thử lại…')).toBeVisible({ timeout: 20_000 });

		// Disabled rather than accepting a word that cannot go anywhere and
		// leaving the player to watch their turn expire.
		await expect(board(page).input).toBeDisabled();
		await expect(board(page).submit).toBeDisabled();

		// And it comes back on its own once the connection does.
		socket.restore();
		await expect(page.getByText('Đã kết nối')).toBeVisible({ timeout: 20_000 });
		await expect(board(page).input).toBeEnabled();

		await context.close();
	});
});
