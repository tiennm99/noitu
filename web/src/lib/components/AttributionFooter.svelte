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
	.credit {
		padding: 20px 16px 28px;
		text-align: center;
		color: var(--text-muted);
		font-size: 0.8rem;
	}

	.credit p {
		margin: 0;
	}

	a {
		color: inherit;
	}
</style>
