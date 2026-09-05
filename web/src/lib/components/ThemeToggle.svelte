<script>
	import { t } from '$lib/i18n/vi.js';
	import { settings } from '$lib/stores/settings.svelte.js';

	const dark = $derived(settings.state.theme === 'dark');
</script>

<button
	type="button"
	class="toggle"
	aria-pressed={dark}
	title={dark ? t.themeLight : t.themeDark}
	onclick={() => settings.toggleTheme()}
>
	<span aria-hidden="true">{dark ? '☀' : '☾'}</span>
	<span class="sr-only">{t.theme}: {dark ? t.themeDark : t.themeLight}</span>
</button>

<style>
	.toggle {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 36px;
		height: 36px;
		border: 1px solid var(--border);
		border-radius: 50%;
		background: var(--surface);
		font-size: 1rem;
		line-height: 1;
	}

	.toggle:hover {
		background: var(--surface-alt);
	}
</style>
