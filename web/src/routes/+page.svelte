<script>
	import { goto } from '$app/navigation';
	import DifficultyPicker from '$lib/components/DifficultyPicker.svelte';
	import NicknameInput from '$lib/components/NicknameInput.svelte';
	import { Difficulty } from '$lib/proto/noitu/v1/game_pb.js';
	import { fill, t } from '$lib/i18n/vi.js';
	import { settings } from '$lib/stores/settings.svelte.js';

	// Resumes the rung last played rather than always opening on Medium, so
	// choosing a difficulty is not a step repeated every visit.
	let difficulty = $state(settings.state.lastDifficulty ?? Difficulty.MEDIUM);

	/** @param {number} value */
	function selectDifficulty(value) {
		settings.setLastDifficulty(value);
	}

	function playBot() {
		goto(`/play?difficulty=${difficulty}`);
	}
</script>

<svelte:head>
	<title>{t.titleHome}</title>
</svelte:head>

<section class="home">
	<!-- The screen's heading, so the document does not start at h2 and a
	     screen reader has something to land on. -->
	<h1 class="tagline">{t.tagline}</h1>

	<!-- The rule itself, before the nickname or the ladder: a newcomer's first
	     read of it used to be a rejection message under a running clock. -->
	<p class="how">
		{fill(t.howToPlay, { example: t.howToPlayExample })}
	</p>

	<NicknameInput />
	<DifficultyPicker bind:value={difficulty} onselect={selectDifficulty} />

	<div class="actions">
		<button type="button" class="primary" onclick={playBot}>{t.playBot}</button>
		<a class="secondary" href="/online">{t.playOnline}</a>
	</div>

	<a class="rules-link" href="/rules">{t.rulesLink}</a>
</section>

<style>
	.home {
		display: flex;
		flex-direction: column;
		gap: var(--space-5);
		padding-top: var(--space-3);
	}

	/* A heading by role, a tagline by weight: it introduces the game rather
	   than titling a document the header already names. */
	.tagline {
		margin: 0;
		color: var(--text-muted);
		font-size: var(--text-3);
		font-weight: 400;
	}

	.actions {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}

	.actions > * {
		min-height: 44px;
		padding: var(--space-4);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface);
		color: inherit;
		font-weight: 600;
		text-align: center;
		text-decoration: none;
	}

	.actions .primary {
		border-color: transparent;
		background: var(--accent);
		color: var(--accent-text);
		transition: background-color 150ms ease-out;
	}

	.actions .primary:hover {
		background: var(--accent-hover);
	}

	.actions .primary:active {
		background: var(--accent-pressed);
	}

	/* Its own line under the two big taps, not one more of them: this is a
	   thing to read before playing, not a third way to start a game. */
	.rules-link {
		align-self: center;
	}
</style>
