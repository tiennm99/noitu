<script>
	import { t } from '$lib/i18n/vi.js';

	/** @type {{ code: string }} */
	let { code } = $props();

	/** @type {'' | 'code' | 'link'} */
	let copied = $state('');
	/** @type {any} */
	let clearTimer;

	const inviteUrl = $derived(
		typeof location === 'undefined' ? '' : `${location.origin}/online?code=${code}`
	);

	// Grouped in threes: the code exists to be read aloud down a phone line, and
	// six unbroken letters are read back wrong.
	const grouped = $derived(`${code.slice(0, 3)} ${code.slice(3)}`);

	/**
	 * @param {'code' | 'link'} what
	 * @param {string} text
	 */
	async function copy(what, text) {
		try {
			await navigator.clipboard.writeText(text);
			copied = what;
			clearTimeout(clearTimer);
			clearTimer = setTimeout(() => (copied = ''), 2000);
		} catch {
			// Clipboard access is refused outside a secure context and in some
			// embedded browsers. The code is on screen either way, so there is
			// nothing to recover — only a confirmation not to show.
		}
	}

	async function share() {
		try {
			await navigator.share({ title: t.appName, text: t.shareInvite, url: inviteUrl });
		} catch {
			// Includes the user simply dismissing the sheet, which is not an error.
		}
	}
</script>

<div class="panel">
	<p class="label">{t.yourRoomCode}</p>
	<p class="code" data-testid="room-code" aria-label={code.split('').join(' ')}>{grouped}</p>

	<div class="actions">
		<button type="button" onclick={() => copy('code', code)}>
			{copied === 'code' ? t.copied : t.copyCode}
		</button>
		<button type="button" onclick={() => copy('link', inviteUrl)}>
			{copied === 'link' ? t.copied : t.copyLink}
		</button>
		{#if typeof navigator !== 'undefined' && 'share' in navigator}
			<button type="button" onclick={share}>{t.shareLink}</button>
		{/if}
	</div>
</div>

<style>
	.panel {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 10px;
		padding: 20px;
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--surface);
	}

	.label {
		margin: 0;
		color: var(--text-muted);
		font-size: 0.85rem;
	}

	.code {
		margin: 0;
		font-size: 2rem;
		font-weight: 700;
		font-variant-numeric: tabular-nums;
		letter-spacing: 0.12em;
	}

	.actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: 8px;
	}

	.actions button {
		padding: 8px 14px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface-alt);
		font-size: 0.85rem;
	}
</style>
