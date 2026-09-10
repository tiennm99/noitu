<script>
	import '../app.css';
	import { page } from '$app/state';
	import AttributionFooter from '$lib/components/AttributionFooter.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';
	import { t } from '$lib/i18n/vi.js';

	let { children } = $props();

	// The online screen puts the game and the room's conversation side by side
	// where the screen allows it, and two columns do not fit in a width chosen
	// for a phone. Every other screen is one column of reading, which is what
	// the narrower shell is for.
	const wide = $derived(page.route.id === '/online');
</script>

<div class="shell" class:wide>
	<a class="skip" href="#main">{t.skipToContent}</a>

	<header>
		<a class="brand" href="/">{t.appName}</a>
		<ThemeToggle />
	</header>

	<main id="main">
		{@render children()}
	</main>

	<AttributionFooter />
</div>

<style>
	/*
	 * A definite height, not a minimum.
	 *
	 * The board is built to keep the syllable, the clock and the word field
	 * still while the chain grows inside its own scroller — but a flex item
	 * only shrinks when its container has a height to shrink against, and
	 * min-height let the whole column grow instead. So the chain kept its full
	 * height, the page got taller with every turn, and the things that were
	 * supposed to stay put walked off the bottom of the screen.
	 */
	.shell {
		display: flex;
		flex-direction: column;
		height: 100vh;
		height: 100dvh;
		max-width: 560px;
		margin: 0 auto;
	}

	.shell.wide {
		max-width: 1040px;
	}

	/* viewport-fit=cover is opted into for the notch, which means the gutters
	   have to clear it themselves: 16px normally, more where the cutout or a
	   rounded corner eats into the edge. */
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		/* The header is chrome: every pixel of it is one the board and the
		   lobby below do not get, and on a 1080p screen the post-game panel
		   was the one that had to scroll for them. */
		padding-block: var(--space-2);
		padding-left: max(16px, env(safe-area-inset-left));
		padding-right: max(16px, env(safe-area-inset-right));
	}

	.brand {
		color: inherit;
		font-size: var(--text-7);
		font-weight: 700;
		text-decoration: none;
	}

	/*
	 * The last resort, and only that. Bounded height means the chain gives up
	 * its space first; this catches whatever still does not fit — a long lobby,
	 * a game-over panel with standings and suggestions on a short screen — so
	 * nothing is ever clipped out of reach.
	 */
	main {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		padding-left: max(16px, env(safe-area-inset-left));
		padding-right: max(16px, env(safe-area-inset-right));
	}
</style>
