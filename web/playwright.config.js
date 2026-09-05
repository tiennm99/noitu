import { defineConfig, devices } from '@playwright/test';

const PORT = 4173;

/**
 * CI installs Playwright's own Chromium. Some machines cannot reach that
 * download, so a locally installed browser can be used instead by setting
 * PLAYWRIGHT_CHANNEL=chrome (or msedge). Both drive the same engine.
 */
const channel = process.env.PLAYWRIGHT_CHANNEL;

export default defineConfig({
	testDir: './e2e',
	// A whole game per test, including a bot that deliberately pauses to think.
	timeout: 60_000,
	expect: { timeout: 10_000 },
	fullyParallel: false,
	forbidOnly: !!process.env.CI,
	retries: process.env.CI ? 1 : 0,
	workers: 1,
	reporter: process.env.CI ? [['github'], ['list']] : [['list']],

	use: {
		baseURL: `http://localhost:${PORT}`,
		trace: 'retain-on-failure',
		...devices['Desktop Chrome'],
		...(channel ? { channel } : {})
	},

	projects: [{ name: 'chromium' }],

	// The real binary, serving the real built frontend, against the fixture
	// dictionary. Nothing here downloads the 179 MB upstream release.
	webServer: {
		command: 'go run ./cmd/noitu-server',
		cwd: '../server',
		url: `http://localhost:${PORT}/healthz`,
		reuseExistingServer: !process.env.CI,
		timeout: 120_000,
		stdout: 'pipe',
		stderr: 'pipe',
		env: {
			NOITU_ADDR: `:${PORT}`,
			NOITU_DB_PATH: '../data/fixture.db',
			NOITU_WEB_DIR: '../web/build',
			// Long enough that no test races the turn clock. Timeout behaviour
			// has its own server-side test; here it would only cause flakes.
			NOITU_TURN_LIMIT: '120s',
			// Long enough that a client which notices a dead socket on its own
			// liveness check still has time to come back, and short enough to
			// wait out in the forfeit test.
			NOITU_GRACE: '25s'
		}
	}
});
