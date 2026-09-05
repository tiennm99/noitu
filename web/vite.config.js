import { fileURLToPath } from 'node:url';
import { sveltekit } from '@sveltejs/kit/vite';
// vitest/config, not vite: the `test` block below is Vitest's, and vite's own
// config type does not know about it.
import { defineConfig } from 'vitest/config';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		// Dev runs Vite and the Go binary on different ports, so the socket has
		// to be proxied. That keeps the client's URL logic identical in both
		// environments: it always connects to its own origin.
		proxy: {
			'/ws': { target: 'ws://localhost:8080', ws: true },
			'/healthz': 'http://localhost:8080'
		}
	},
	test: {
		// The default node environment: the cross-language fixture test reads
		// files through import.meta.url, which jsdom rewrites to an http URL.
		// The two suites that need a DOM opt in with a per-file docblock.
		include: ['tests/**/*.test.js'],
		// SvelteKit resolves $lib during a build, not under Vitest, so the test
		// run needs the same mapping stated explicitly. It points at the same
		// directory the framework uses, so there is one meaning of $lib.
		alias: {
			$lib: fileURLToPath(new URL('./src/lib', import.meta.url))
		}
	}
});
