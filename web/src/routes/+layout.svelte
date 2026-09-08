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
	<header>
		<a class="brand" href="/">{t.appName}</a>
		<ThemeToggle />
	</header>

	<main>
		{@render children()}
	</main>

	<AttributionFooter />
</div>

<style>
	.shell {
		display: flex;
		flex-direction: column;
		min-height: 100vh;
		min-height: 100dvh;
		max-width: 560px;
		margin: 0 auto;
	}

	.shell.wide {
		max-width: 1040px;
	}

	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 14px 16px;
	}

	.brand {
		color: inherit;
		font-size: 1.1rem;
		font-weight: 700;
		text-decoration: none;
	}

	main {
		display: flex;
		flex-direction: column;
		flex: 1;
		min-height: 0;
		padding: 0 16px;
	}
</style>
