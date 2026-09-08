import { expect, test } from '@playwright/test';
import { ONE_SYLLABLE_WORD, UNKNOWN_WORD } from './fixture-dictionary.js';
import {
	board,
	chainWords,
	chooseDifficulty,
	playLegalMove,
	setNickname,
	submitWord,
	waitForMyTurn
} from './helpers.js';

test.describe('playing the bot', () => {
	test('a game runs end to end at the hardest difficulty', async ({ page }) => {
		await page.goto('/');
		await setNickname(page, 'Minh');
		await chooseDifficulty(page, /Khó/);
		await page.getByRole('button', { name: 'Chơi với máy' }).click();

		await expect(page).toHaveURL(/\/play\?difficulty=3/);
		await waitForMyTurn(page);

		// The fixture graph has one syllable productive enough to open on, so
		// the opening word always ends in it.
		await expect(board(page).syllable).toHaveText('sinh');

		const used = new Set(await chainWords(page));
		const mine = await playLegalMove(page, used);

		// The bot answers, which is the only thing that moves the turn back.
		await waitForMyTurn(page);

		const words = await chainWords(page);
		expect(words).toContain(mine);
		expect(words.length).toBeGreaterThanOrEqual(3); // opening, mine, the bot's
	});

	test('the turn arrives with the required syllable already in the field', async ({ page }) => {
		await page.goto('/play?difficulty=1');
		await waitForMyTurn(page);

		const syllable = (await board(page).syllable.textContent())?.trim();
		// A trailing space, so the player types the rest of the word and not
		// the half of it that was never in question.
		await expect(board(page).input).toHaveValue(`${syllable} `);
	});

	test('the chain lists the newest word first', async ({ page }) => {
		await page.goto('/play?difficulty=1');
		await waitForMyTurn(page);

		const [opening] = await chainWords(page);
		const mine = await playLegalMove(page, new Set([opening]));

		// The opening word was played before mine, so it sits below it — and
		// stays at the bottom however many words the bot adds on top.
		const words = await chainWords(page);
		expect(words.indexOf(mine)).toBeLessThan(words.indexOf(opening));
		expect(words[words.length - 1]).toBe(opening);
	});

	test('the score rises and the board shows both sides', async ({ page }) => {
		await page.goto('/play?difficulty=1');
		await waitForMyTurn(page);

		const used = new Set(await chainWords(page));
		await playLegalMove(page, used);
		await waitForMyTurn(page);

		// Scores are the server's; the client only renders them.
		const scores = await page.locator('.score').allTextContents();
		expect(scores).toHaveLength(2);
		expect(Number(scores[0])).toBeGreaterThan(0);
	});

	test.describe('rejections', () => {
		test('a word outside the dictionary is refused in Vietnamese', async ({ page }) => {
			await page.goto('/play?difficulty=1');
			await waitForMyTurn(page);

			await submitWord(page, UNKNOWN_WORD);

			await expect(board(page).rejection).toHaveText('Không tìm thấy từ này trong từ điển.');
		});

		test('a single syllable is refused for being too short', async ({ page }) => {
			await page.goto('/play?difficulty=1');
			await waitForMyTurn(page);

			await submitWord(page, ONE_SYLLABLE_WORD);

			await expect(board(page).rejection).toHaveText('Từ phải có ít nhất 2 tiếng.');
		});

		test('a real word that does not link names the syllable it should start with', async ({
			page
		}) => {
			await page.goto('/play?difficulty=1');
			await waitForMyTurn(page);

			// "toán học" is in the fixture but never starts with the opening
			// syllable, so this is a wrong link rather than an unknown word.
			await submitWord(page, 'toán học');

			await expect(board(page).rejection).toHaveText('Từ phải bắt đầu bằng tiếng “sinh”.');
		});

		test('a refused word costs the player the attempt, not the turn', async ({ page }) => {
			await page.goto('/play?difficulty=1');
			await waitForMyTurn(page);

			await submitWord(page, UNKNOWN_WORD);
			await expect(board(page).rejection).toBeVisible();

			// The word reached the server, so the typed text is gone and the
			// field is back to the syllable it seeds every turn with. What must
			// not happen is the turn moving on, and the player must be able to
			// try again immediately.
			await expect(board(page).input).toHaveValue('sinh ');
			await expect(board(page).turn).toHaveText('Đến lượt bạn');
			await expect(board(page).input).toBeEnabled();
		});
	});

	test('resigning ends the game and offers another', async ({ page }) => {
		await page.goto('/play?difficulty=1');
		await waitForMyTurn(page);

		page.on('dialog', (dialog) => dialog.accept());
		await page.getByRole('button', { name: 'Đầu hàng' }).click();

		await expect(page.getByRole('heading', { name: 'Bạn thua.' })).toBeVisible();
		await expect(page.getByRole('button', { name: 'Chơi lại' })).toBeVisible();
	});

	test('a loss shows what could have been played', async ({ page }) => {
		await page.goto('/play?difficulty=1');
		await waitForMyTurn(page);

		const syllable = (await board(page).syllable.textContent())?.trim() ?? '';

		page.on('dialog', (dialog) => dialog.accept());
		await page.getByRole('button', { name: 'Đầu hàng' }).click();

		await expect(page.getByRole('heading', { name: 'Bạn có thể nối' })).toBeVisible();
		const offered = await page.locator('.suggestions li').allTextContents();
		expect(offered.length).toBeGreaterThan(0);
		expect(offered.length).toBeLessThanOrEqual(3);
		for (const word of offered) {
			expect(word.startsWith(`${syllable} `)).toBe(true);
		}
	});

	test('a finished game can be downloaded as a transcript', async ({ page }) => {
		await page.goto('/play?difficulty=1');
		await waitForMyTurn(page);

		const [opening] = await chainWords(page);
		await playLegalMove(page, new Set([opening]));

		page.on('dialog', (dialog) => dialog.accept());
		await page.getByRole('button', { name: 'Đầu hàng' }).click();

		const download = page.waitForEvent('download');
		await page.getByRole('button', { name: 'Tải chuỗi từ' }).click();
		const file = await download;

		expect(file.suggestedFilename()).toMatch(/^noi-tu-\d{4}-\d{2}-\d{2}-\d{4}\.txt$/);
		const stream = await file.createReadStream();
		const chunks = [];
		for await (const chunk of stream) chunks.push(chunk);
		const text = Buffer.concat(chunks).toString('utf8');
		expect(text).toContain(opening);
		expect(text).toContain('Bạn thua.');
	});

	test('a rematch starts exactly one new game', async ({ page }) => {
		// The regression this guards: the screen used to infer "start a game"
		// from the board being empty, so clearing it for a rematch sent a second
		// request and the server built two rooms that destroyed each other.
		await page.goto('/play?difficulty=1');
		await waitForMyTurn(page);

		page.on('dialog', (dialog) => dialog.accept());
		await page.getByRole('button', { name: 'Đầu hàng' }).click();
		await expect(page.getByRole('button', { name: 'Chơi lại' })).toBeVisible();

		await page.getByRole('button', { name: 'Chơi lại' }).click();
		await waitForMyTurn(page);

		// One game means one opening word on the board.
		const words = await chainWords(page);
		expect(words).toHaveLength(1);

		// And it stays one game: a second room would take the turn away again
		// or reject the next move as stale.
		const used = new Set(words);
		await playLegalMove(page, used);
		await waitForMyTurn(page);
		await expect(board(page).rejection).toHaveCount(0);
	});

	test('the personal best is kept per difficulty', async ({ page }) => {
		await page.goto('/play?difficulty=2');
		await waitForMyTurn(page);

		const used = new Set(await chainWords(page));
		await playLegalMove(page, used);
		await waitForMyTurn(page);

		page.on('dialog', (dialog) => dialog.accept());
		await page.getByRole('button', { name: 'Đầu hàng' }).click();
		await expect(page.getByRole('button', { name: 'Về trang chủ' })).toBeVisible();

		await page.getByRole('button', { name: 'Về trang chủ' }).click();

		// The record shows against Trung bình, and only against it.
		const medium = page.getByRole('radio', { name: /Trung bình/ });
		await expect(medium).toBeVisible();
		await expect(page.locator('label', { has: medium })).not.toContainText('Chưa có');
		await expect(
			page.locator('label', { has: page.getByRole('radio', { name: /^Dễ/ }) })
		).toContainText('Chưa có');
	});

	test('the attribution footer credits the dictionary source', async ({ page }) => {
		await page.goto('/');

		const link = page.getByRole('link', { name: 'Wiktionary tiếng Việt' });
		await expect(link).toHaveAttribute('href', 'https://vi.wiktionary.org/');
		await expect(page.getByRole('link', { name: /CC BY-SA 3\.0/ })).toBeVisible();
	});

	test('a deep link is served by the binary, not a 404', async ({ page }) => {
		const response = await page.goto('/play?difficulty=3');
		expect(response?.status()).toBe(200);
		await waitForMyTurn(page);
	});
});
