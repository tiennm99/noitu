import adapter from '@sveltejs/adapter-static';

/**
 * A single-page app: the Go binary serves the built assets and falls back to
 * index.html for unknown paths, so every route is resolved in the browser.
 * Prerendering is disabled in src/routes/+layout.js rather than here, because
 * the game has nothing static to render — every screen depends on a socket.
 *
 * @type {import('@sveltejs/kit').Config}
 */
export default {
	kit: {
		adapter: adapter({ fallback: 'index.html' })
	}
};
