<script>
	import '../app.css';
	import AttributionFooter from '$lib/components/AttributionFooter.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';
	import { t } from '$lib/i18n/vi.js';

	let { children } = $props();
</script>

<div class="shell">
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
