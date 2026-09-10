<script>
	import { t } from '$lib/i18n/vi.js';
</script>

<!--
	The user-visible half of the CC BY-SA 4.0 obligation. The wordlist is
	derived from a share-alike source, so the credit and the licence link have
	to reach the player, not only the repository.
-->
<footer class="credit">
	<p>
		{t.attributionIntro}
		<a href="https://vi.wiktionary.org/" target="_blank" rel="noreferrer noopener">
			{t.attributionSource}
		</a>, {t.attributionMiddle}
		<a
			href="https://creativecommons.org/licenses/by-sa/4.0/deed.vi"
			target="_blank"
			rel="noreferrer noopener"
		>
			{t.attributionLicense}
		</a>.
	</p>
</footer>

<style>
	/*
	 * Compact, because the game screens are bounded by the viewport now and
	 * every pixel here is one the board does not get. Still on every screen,
	 * still legible, still carrying both links: the wordlist is share-alike and
	 * the credit is an obligation, not a decoration.
	 */
	.credit {
		padding-block: var(--space-2);
		padding-left: max(16px, env(safe-area-inset-left));
		padding-right: max(16px, env(safe-area-inset-right));
		padding-bottom: max(var(--space-2), env(safe-area-inset-bottom));
		text-align: center;
		color: var(--text-muted);
		font-size: var(--text-2);
		line-height: 1.4;
	}

	.credit p {
		margin: 0;
	}

	a {
		color: inherit;
	}
</style>
