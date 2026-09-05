<script>
	import { t } from '$lib/i18n/vi.js';
</script>

<!--
	The online 1v1 flows land in phase 7. This route exists now so the home
	screen has somewhere real to point and the deep link is already covered by
	the server's single-page fallback.
-->
<section class="online">
	<h1>{t.onlineTitle}</h1>
	<p>{t.onlineComingSoon}</p>
	<a href="/">{t.back}</a>
</section>

<style>
	.online {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding-top: 12px;
	}

	h1 {
		margin: 0;
		font-size: 1.3rem;
	}

	p {
		margin: 0;
		color: var(--text-muted);
	}

	a {
		align-self: flex-start;
		padding: 10px 16px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		color: inherit;
		font-weight: 600;
		text-decoration: none;
	}
</style>
