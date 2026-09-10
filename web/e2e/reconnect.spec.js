import { expect, test } from '@playwright/test';
import {
	awaitTurn,
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
 * The pair comes back twice over: as owner and joiner, which is what the seat
 * ids and the names follow, and as lead and second, which is who the first
 * turn was drawn for.
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
	const {
		lead,
		waits: [second]
	} = await awaitTurn(host, guest);

	return {
		host,
		guest,
		code,
		lead,
		second,
		/** @param {import('@playwright/test').Page} page */
		seatOf: (page) => (page === host ? 'p1' : 'p2'),
		/** @param {import('@playwright/test').Page} page */
		nameOf: (page) => (page === host ? 'Minh' : 'Lan'),
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
		const { lead, second, seatOf, nameOf, close } = await pvpRoom(browser);

		// The player who drops is the one holding the turn, so what has to come
		// back is a position somebody owes a move to.
		const used = new Set(await chainWords(lead));
		await playLegalMove(lead, used);
		await waitForMyTurn(second);

		const syllable = (await board(lead).syllable.textContent())?.trim() ?? '';

		// Navigating away takes the socket with it, which is what the server
		// sees when a player loses their connection. The resume token lives in
		// this tab's session storage, so coming back can reclaim the seat.
		await second.goto('about:blank');

		const away = lead.getByTestId(`away-${seatOf(second)}`);
		await expect(away).toContainText(`${nameOf(second)} mất kết nối`, { timeout: 20_000 });

		// Back inside the grace window.
		await second.goto('/online');

		// The seat is restored: the other player stops waiting, and the one who
		// returned is looking at the same position rather than the lobby.
		await expect(away).toHaveCount(0, { timeout: 20_000 });
		await expect(board(second).syllable).toHaveText(syllable, { timeout: 20_000 });
		await expect(board(second).turn).toHaveText('Đến lượt bạn');

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

		// Two surfaces, deliberately: the badge is the status, and the banner
		// over the board is the one the player can act on — it carries the
		// retry that saves waiting out a backoff of up to eight seconds with a
		// turn timer running.
		const badge = page.locator('.badge', { hasText: 'Mất kết nối, đang thử lại…' });
		await expect(badge).toBeVisible({ timeout: 20_000 });
		await expect(page.getByRole('button', { name: 'Thử lại' })).toBeVisible();

		// The word cannot be sent, but the field itself stays alive: `disabled`
		// on a focused input blurs it, and a blurred input closes the on-screen
		// keyboard that nothing can then reopen without a tap. So the send is
		// what refuses, and the field says whose turn it is instead of inviting
		// a word it cannot carry.
		await expect(board(page).submit).toBeDisabled();
		await expect(board(page).input).toHaveAttribute('aria-disabled', 'true');
		await expect(board(page).input).toHaveAttribute(
			'placeholder',
			'Mất kết nối, đang thử lại…'
		);

		// And it comes back on its own once the connection does.
		socket.restore();
		await expect(page.getByText('Đã kết nối')).toBeVisible({ timeout: 20_000 });
		await expect(board(page).submit).toBeEnabled();
		await expect(board(page).input).toHaveAttribute('aria-disabled', 'false');

		await context.close();
	});
});
