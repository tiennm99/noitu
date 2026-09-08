import { expect, test } from '@playwright/test';
import {
	board,
	chainWords,
	chat,
	playLegalMove,
	readyAndStart,
	say,
	seats,
	setNickname,
	waitForMyTurn
} from './helpers.js';

/**
 * Two browser contexts, so each player has their own storage, nickname and
 * socket. One context with two tabs would share a session and prove nothing
 * about two people playing.
 */
async function twoPlayers(browser, options = {}) {
	// newContext does not inherit the project's `use` options, so a test about
	// a phone-sized screen has to pass the viewport in here.
	const hostContext = await browser.newContext(options);
	const guestContext = await browser.newContext(options);
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
		await expect(host.getByTestId('player-count')).toHaveText('1/4 người chơi');

		await joinRoom(guest, 'Lan', code);
		await readyAndStart(host, guest);

		// Both boards come up, and exactly one player is on turn.
		await waitForMyTurn(host);
		// Whose turn it is, by name: with four seats "the opponent" would stop
		// naming anybody.
		await expect(board(guest).turn).toHaveText('Đến lượt Minh…');

		// Each side is shown the other's server-sanitized name. Scoped to the
		// scoreboard: the turn indicator names a player too, so an unscoped
		// match is ambiguous for whoever is not on turn.
		await expect(host.getByTestId('scoreboard').getByText('Lan')).toBeVisible();
		await expect(guest.getByTestId('scoreboard').getByText('Minh')).toBeVisible();

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

		// The series score of the room, which the finished game has just moved:
		// the guest took it, so the owner's lobby shows 1 against their seat.
		await expect(host.getByTestId('wins-p2')).toContainText('Tỉ số 1');
		await expect(host.getByTestId('wins-p1')).toContainText('Tỉ số 0');

		await readyAndStart(host, guest);

		// A new game in the same room: the code is unchanged and the board is
		// back to a single opening word.
		await waitForMyTurn(host);
		await expect(host.getByText(code)).toBeVisible();
		expect(await chainWords(host)).toHaveLength(1);

		// And the tally is carried into it, where the board shows it beside
		// each player's score for this game.
		await expect(host.getByTestId('series-p2')).toContainText('Tỉ số 1');

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
		await expect(host.getByTestId('ready-p2')).toHaveText('Đã sẵn sàng');
		await expect(host.getByTestId('start-game')).toBeEnabled();

		// Ready is a commitment: the way out is to take it back first.
		await expect(guest.getByRole('button', { name: 'Rời phòng' })).toBeDisabled();
		await guest.getByTestId('ready').click();
		await expect(guest.getByRole('button', { name: 'Rời phòng' })).toBeEnabled();

		await guest.getByRole('button', { name: 'Rời phòng' }).click();

		// The room survives with its owner in it, and the rest going spare.
		await expect(host.getByTestId('player-count')).toHaveText('1/4 người chơi');
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
		await expect(host.getByTestId('player-count')).toHaveText('1/4 người chơi');

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
		await expect(guest.getByTestId('player-count')).toHaveText('1/4 người chơi');

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

	test('the two players can talk, in the lobby and in the game', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);

		await say(host, 'chào bạn');
		// Each side is shown its own words and the other's, with a name on the
		// half that is not theirs.
		await expect(chat(guest).log).toContainText('chào bạn');
		await expect(chat(guest).log).toContainText('Minh');
		await expect(chat(host).log).toContainText('chào bạn');

		await say(guest, 'chào!');
		await expect(chat(host).log).toContainText('chào!');

		// The log is a list of lines, each naming its author and written in
		// that seat's own colour, so four people talking stay tellable apart.
		const lines = chat(host).log.getByRole('listitem');
		await expect(lines.first()).toContainText('Minh:');
		await expect(lines.nth(1)).toContainText('Lan:');
		const [first, second] = await Promise.all([
			lines.first().evaluate((el) => getComputedStyle(el).color),
			lines.nth(1).evaluate((el) => getComputedStyle(el).color)
		]);
		expect(first).not.toBe(second);

		// And the conversation follows them into the game, where a screen this
		// wide keeps it beside the board rather than folding it away.
		await readyAndStart(host, guest);
		await waitForMyTurn(host);
		await expect(chat(host).log).toContainText('chào bạn');

		await close();
	});

	test('the board panel opens with nothing unread from the lobby', async ({ browser }) => {
		// A phone: one column, so the board's chat folds behind an unread count
		// instead of sitting beside the game. Nothing folds on a wide screen,
		// and a badge for a panel that is always open would be a lie.
		const { host, guest, close } = await twoPlayers(browser, {
			viewport: { width: 420, height: 900 }
		});

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);

		// A conversation both players have already read.
		await say(host, 'một');
		await expect(chat(guest).log).toContainText('một');
		await say(guest, 'hai');
		await expect(chat(host).log).toContainText('hai');

		await readyAndStart(host, guest);
		await waitForMyTurn(host);

		// The panel that folds on the way into the game is the one that was
		// open in the lobby. What it had already shown is not new mail.
		await expect(chat(host).unread).toHaveCount(0);

		// And it does start counting what actually arrives while folded. The
		// guest's panel is folded too, so it has to be opened before there is
		// anything to type into.
		await guest.getByRole('button', { name: 'Trò chuyện' }).click();
		await say(guest, 'ba');
		await expect(chat(host).unread).toHaveText('1 tin mới');

		await host.getByRole('button', { name: 'Trò chuyện' }).click();
		await expect(chat(host).unread).toHaveCount(0);

		await close();
	});

	test('a turn arriving does not take the chat field away', async ({ browser }) => {
		const { host, guest, close } = await playingPair(browser);

		// The guest starts typing while the host is still on turn. The word
		// field wants focus the moment a turn lands, and taking it here would
		// drop the rest of the sentence into the game.
		await chat(guest).input.click();
		await chat(guest).input.fill('đang gõ dở');

		await playLegalMove(host, new Set(await chainWords(host)));
		await waitForMyTurn(guest);

		await expect(chat(guest).input).toBeFocused();
		await expect(chat(guest).input).toHaveValue('đang gõ dở');

		await close();
	});

	test('a message is rendered as text, never as markup', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);

		await say(host, '<b>đậm</b>');
		await expect(chat(guest).log).toContainText('<b>đậm</b>');
		await expect(chat(guest).log.locator('b')).toHaveCount(0);

		await close();
	});

	test('sending is refused until there is something to send', async ({ page }) => {
		await createRoom(page, 'Minh');
		const host = page;

		await expect(chat(host).send).toBeDisabled();
		// Whitespace and an invisible character are both nothing once the
		// server has sanitized them, so neither may be sent.
		await chat(host).input.fill('   ');
		await expect(chat(host).send).toBeDisabled();
		await chat(host).input.fill('\u200b');
		await expect(chat(host).send).toBeDisabled();
		await chat(host).input.fill('có chữ');
		await expect(chat(host).send).toBeEnabled();
	});

	test('a refusal is visible in the lobby, which shows no errors of its own', async ({
		browser
	}) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);

		// Past the burst the server refuses, and the panel is the only surface
		// in this phase that can say so.
		for (let i = 0; i < 8; i++) {
			await say(host, `tin ${i}`);
		}

		// containText, not haveText: the box carries its own dismiss button.
		await expect(chat(host).error).toContainText('Bạn thao tác quá nhanh');

		await close();
	});

	test('a reload brings the conversation back, and a new room does not', async ({ browser }) => {
		const { host, guest, close } = await twoPlayers(browser);

		const code = await createRoom(host, 'Minh');
		await joinRoom(guest, 'Lan', code);
		await say(host, 'nhớ nhé');
		await expect(chat(guest).log).toContainText('nhớ nhé');

		await guest.reload();
		await expect(chat(guest).log).toContainText('nhớ nhé');

		// A different room is a different conversation, even in the same tab.
		// Left properly rather than navigated away from: an open tab that walks
		// off is resumed back into the room it was in, which is its own
		// feature.
		await guest.getByRole('button', { name: 'Rời phòng' }).click();
		await guest.getByRole('button', { name: 'Tạo phòng' }).click();
		await expect(guest.getByTestId('room-code')).toBeVisible();
		await expect(guest.getByText('Chưa có tin nhắn nào.')).toBeVisible();

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

	test('a game in progress turns a latecomer away', async ({ browser }) => {
		// The room has seats going spare; arriving in the middle of a game is
		// what is refused, not the room being full.
		const { host, code, close } = await playingPair(browser);
		void host;

		const thirdContext = await browser.newContext();
		const third = await thirdContext.newPage();
		await joinRoom(third, 'Nam', code);

		await expect(third.getByTestId('join-error')).toHaveText('Ván đấu đang diễn ra.');

		await thirdContext.close();
		await close();
	});

	test('four players fill the room, and a fifth is turned away', async ({ browser }) => {
		const { host, guest, code, close } = await twoPlayers(browser).then(async (pair) => ({
			...pair,
			code: await createRoom(pair.host, 'Minh')
		}));
		await joinRoom(guest, 'Lan', code);

		/** @type {import('@playwright/test').BrowserContext[]} */
		const extra = [];
		/** @type {import('@playwright/test').Page[]} */
		const players = [];
		for (const name of ['Nam', 'Hà']) {
			const context = await browser.newContext();
			extra.push(context);
			const page = await context.newPage();
			players.push(page);
			await joinRoom(page, name, code);
		}

		await expect(host.getByTestId('player-count')).toHaveText('4/4 người chơi');
		await expect(seats(host)).toHaveCount(4);

		const fifthContext = await browser.newContext();
		const fifth = await fifthContext.newPage();
		await joinRoom(fifth, 'Bình', code);
		await expect(fifth.getByTestId('join-error')).toHaveText('Phòng đã đủ người.');

		// Every guest has to say yes, not just the first one.
		await guest.getByTestId('ready').click();
		await expect(host.getByTestId('start-game')).toBeDisabled();
		await readyAndStart(host, ...players);

		// Four boards, and exactly one player on turn.
		await waitForMyTurn(host);
		for (const page of [guest, ...players]) {
			await expect(board(page).turn).toHaveText('Đến lượt Minh…');
		}

		await fifthContext.close();
		for (const context of extra) await context.close();
		await close();
	});

	test('a player who goes out is a spectator, and the rest play on', async ({ browser }) => {
		// Three players, so the game outlives the first knockout. Resignation
		// rather than the clock: the turn limit on the test server is two
		// minutes, and both go through the same elimination.
		const { host, guest, code, close } = await twoPlayers(browser).then(async (pair) => ({
			...pair,
			code: await createRoom(pair.host, 'Minh')
		}));
		await joinRoom(guest, 'Lan', code);

		const thirdContext = await browser.newContext();
		const third = await thirdContext.newPage();
		await joinRoom(third, 'Nam', code);

		await readyAndStart(host, guest, third);
		await waitForMyTurn(host);

		host.on('dialog', (dialog) => dialog.accept());
		await host.getByRole('button', { name: 'Đầu hàng' }).click();

		// Out, but still in the room: no input, no result screen, and the game
		// carrying on in front of them.
		await expect(host.getByTestId('eliminated')).toBeVisible();
		await expect(host.getByRole('textbox', { name: 'Nhập từ của bạn' })).toHaveCount(0);
		await expect(host.getByRole('heading', { name: 'Bạn thua.' })).toHaveCount(0);
		await expect(host.getByTestId('turn-indicator')).toHaveText('Đến lượt Lan…');

		// The others are told who went out, and one of them is now on turn.
		await expect(guest.getByTestId('player-out')).toContainText('Minh');
		await waitForMyTurn(guest);

		// The last two settle it, and everybody sees the same table.
		guest.on('dialog', (dialog) => dialog.accept());
		await guest.getByRole('button', { name: 'Đầu hàng' }).click();

		await expect(third.getByRole('heading', { name: 'Bạn thắng!' })).toBeVisible();
		await expect(host.getByRole('heading', { name: 'Bạn thua.' })).toBeVisible();
		await expect(host.getByTestId('standings').locator('li')).toHaveCount(3);

		await thirdContext.close();
		await close();
	});
});
