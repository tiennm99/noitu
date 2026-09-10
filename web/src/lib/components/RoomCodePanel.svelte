<script>
	import { t } from '$lib/i18n/vi.js';

	/**
	 * The room's code, and the three ways of getting it to somebody else.
	 *
	 * `compact` is the one-line variant for the lobby that sits under a finished
	 * game: the full panel is a screen's worth of vertical space, but that is
	 * exactly the moment a room wants to invite a replacement, so the code and a
	 * copy button still have to be reachable.
	 *
	 * @type {{ code: string, compact?: boolean }}
	 */
	let { code, compact = false } = $props();

	/** @type {'' | 'code' | 'link'} */
	let copied = $state('');
	let failed = $state(false);
	/** @type {any} */
	let clearTimer;
	/** @type {HTMLElement | undefined} */
	let codeEl = $state();

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
		clearTimeout(clearTimer);
		try {
			await navigator.clipboard.writeText(text);
			copied = what;
			failed = false;
			clearTimer = setTimeout(() => (copied = ''), 2000);
		} catch {
			// Clipboard access is refused outside a secure context, which is
			// exactly the self-hosted http://<lan-ip> case a single Go binary
			// invites. Selecting the code is the next best thing — the player can
			// copy it by hand — and saying so beats a button that does nothing.
			copied = '';
			failed = true;
			if (codeEl) getSelection()?.selectAllChildren(codeEl);
			clearTimer = setTimeout(() => (failed = false), 6000);
		}
	}

	async function share() {
		try {
			await navigator.share({ title: t.appName, text: t.shareInvite, url: inviteUrl });
		} catch {
			// Includes the user simply dismissing the sheet, which is not an error.
		}
	}

	$effect(() => () => clearTimeout(clearTimer));
</script>

<div class="panel" class:compact>
	{#if !compact}
		<p class="label">{t.roomCodeLabel}</p>
	{/if}

	<p
		class="code"
		bind:this={codeEl}
		data-testid="room-code"
		aria-label={code.split('').join(' ')}
	>
		{grouped}
	</p>

	<div class="actions">
		<button type="button" onclick={() => copy('code', code)}>
			{copied === 'code' ? t.copied : t.copyCode}
		</button>
		{#if !compact}
			<button type="button" onclick={() => copy('link', inviteUrl)}>
				{copied === 'link' ? t.copied : t.copyLink}
			</button>
			{#if typeof navigator !== 'undefined' && 'share' in navigator}
				<button type="button" onclick={share}>{t.shareLink}</button>
			{/if}
		{/if}
	</div>

	{#if failed}
		<p class="failed" role="alert">{t.copyFailed}</p>
	{/if}

	<!-- The code is on screen whatever the clipboard does; the link only existed
	     inside a button. Selectable text, so there is always something to share
	     by hand. -->
	{#if !compact}
		<p class="link">
			<span class="link-label">{t.inviteLinkLabel}</span>
			<span class="url">{inviteUrl}</span>
		</p>
	{/if}
</div>

<style>
	.panel {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 10px;
		padding: var(--space-5);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--surface);
	}

	/* One line under a finished game: the code, and the one button that matters
	   when somebody has to be invited into the next one. A line rather than a
	   card — the card's border and padding were 30px of a screen that also has
	   the result, the seats, the buttons and the chain to fit. */
	.panel.compact {
		flex-direction: row;
		justify-content: center;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3);
		padding: 0;
		border: 0;
		background: none;
	}

	.label {
		margin: 0;
		color: var(--text-muted);
		font-size: var(--text-4);
	}

	/*
	 * Monospaced, and only here. The alphabet already drops 0/O and 1/I/L, but
	 * 5/S, 2/Z and 8/B still pair off in a proportional face — which matters
	 * when the code is being copied off a screenshot rather than read aloud.
	 */
	.code {
		margin: 0;
		font-family: ui-monospace, 'SFMono-Regular', 'Cascadia Mono', Menlo, monospace;
		font-size: var(--text-9);
		font-weight: 700;
		letter-spacing: 0.12em;
	}

	.panel.compact .code {
		font-size: var(--text-7);
	}

	.actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: var(--space-2);
	}

	.actions button {
		min-height: 44px;
		padding: 10px var(--space-4);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface-alt);
		font-size: var(--text-4);
		font-weight: 600;
	}

	.failed {
		margin: 0;
		color: var(--danger);
		font-size: var(--text-3);
		text-align: center;
	}

	.link {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
		margin: 0;
		max-width: 100%;
		color: var(--text-muted);
		font-size: var(--text-3);
	}

	.link-label {
		font-weight: 600;
	}

	.url {
		max-width: 100%;
		/* The player may have to select this by hand, so it wraps rather than
		   being cut off. */
		overflow-wrap: anywhere;
		user-select: all;
	}
</style>
