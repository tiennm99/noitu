<script>
	import { fill, t } from '$lib/i18n/vi.js';

	// Mirrors basePoints, chainBonus, chainBonusWords, syllableBonus,
	// vietnamese.MinSyllables, speedBonus, rarityBonus, rarityHalvingPenalty
	// and maxPointsPerWord in server/internal/game/engine.go. Copied rather than
	// asked for over the wire: these are constants, not something a game state
	// carries, and this page is static.
	const SCORING = {
		base: 10,
		chainBonus: 2,
		chainBonusWords: 15,
		syllableBonus: 5,
		minSyllables: 2,
		speedBonus: 10,
		rarityBonus: 15,
		rarityHalvingPenalty: 3,
		maxPointsPerWord: 100
	};
</script>

<svelte:head>
	<title>{t.titleRules}</title>
</svelte:head>

<section class="rules">
	<h1>{t.rulesLink}</h1>
	<p class="intro">{t.rulesIntro}</p>

	<nav class="toc" aria-label={t.rulesLink}>
		<a href="#chain">{t.rulesNavChain}</a>
		<a href="#clock">{t.rulesNavClock}</a>
		<a href="#dead-end">{t.rulesNavDeadEnd}</a>
		<a href="#elimination">{t.rulesNavElimination}</a>
		<a href="#scoring">{t.rulesNavScoring}</a>
		<a href="#room">{t.rulesNavRoom}</a>
		<a href="#reconnect">{t.rulesNavReconnect}</a>
	</nav>

	<section id="chain">
		<h2>{t.rulesChainTitle}</h2>
		<p>{t.rulesChainBody}</p>
	</section>

	<section id="clock">
		<h2>{t.rulesClockTitle}</h2>
		<p>{t.rulesClockBody}</p>
	</section>

	<section id="dead-end">
		<h2>{t.rulesDeadEndTitle}</h2>
		<p>{t.rulesDeadEndBody}</p>
	</section>

	<section id="elimination">
		<h2>{t.rulesEliminationTitle}</h2>
		<p>{t.rulesEliminationBody}</p>
	</section>

	<section id="scoring">
		<h2>{t.rulesScoringTitle}</h2>
		<ul>
			<li>{fill(t.rulesScoringBase, { base: SCORING.base })}</li>
			<li>
				{fill(t.rulesScoringChain, { bonus: SCORING.chainBonus, cap: SCORING.chainBonusWords })}
			</li>
			<li>
				{fill(t.rulesScoringSyllable, {
					bonus: SCORING.syllableBonus,
					min: SCORING.minSyllables
				})}
			</li>
			<li>{fill(t.rulesScoringSpeed, { bonus: SCORING.speedBonus })}</li>
			<li>
				{fill(t.rulesScoringRarity, {
					bonus: SCORING.rarityBonus,
					penalty: SCORING.rarityHalvingPenalty
				})}
			</li>
		</ul>
		<p>{fill(t.rulesScoringCap, { cap: SCORING.maxPointsPerWord })}</p>
		<p>{t.rulesScoringBreakdown}</p>
	</section>

	<section id="room">
		<h2>{t.rulesRoomTitle}</h2>
		<p>{t.rulesRoomBody}</p>
	</section>

	<section id="reconnect">
		<h2>{t.rulesReconnectTitle}</h2>
		<p>{t.rulesReconnectBody}</p>
	</section>

	<a class="back" href="/">{t.back}</a>
</section>

<style>
	.rules {
		display: flex;
		flex-direction: column;
		gap: var(--space-5);
		padding-top: var(--space-3);
		padding-bottom: var(--space-4);
	}

	h1 {
		margin: 0;
		font-size: var(--text-8);
	}

	.intro {
		margin: 0;
		color: var(--text-muted);
	}

	.toc {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}

	.toc a {
		padding: 4px var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-pill);
		background: var(--surface-alt);
		color: var(--text);
		font-size: var(--text-3);
		text-decoration: none;
	}

	section section {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		/* Anchored to, not just linked from: a jump from the nav should not
		   land the heading under the sticky-feeling top of a phone browser. */
		scroll-margin-top: var(--space-4);
	}

	h2 {
		margin: 0;
		font-size: var(--text-7);
	}

	p {
		margin: 0;
		line-height: 1.6;
	}

	ul {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin: 0;
		padding-left: 1.2em;
	}

	li {
		line-height: 1.6;
	}

	.back {
		align-self: flex-start;
		color: var(--text-muted);
		font-size: var(--text-5);
	}
</style>
