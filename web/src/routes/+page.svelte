<script>
	import { goto } from '$app/navigation';
	import DifficultyPicker from '$lib/components/DifficultyPicker.svelte';
	import NicknameInput from '$lib/components/NicknameInput.svelte';
	import { Difficulty } from '$lib/proto/noitu/v1/game_pb.js';
	import { t } from '$lib/i18n/vi.js';

	let difficulty = $state(Difficulty.MEDIUM);

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

	<NicknameInput />
	<DifficultyPicker bind:value={difficulty} />

	<div class="actions">
		<button type="button" class="primary" onclick={playBot}>{t.playBot}</button>
		<a class="secondary" href="/online">{t.playOnline}</a>
	</div>
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
		font-size: var(--text-6);
		font-weight: 400;
	}

	.actions {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}

	.actions > * {
		min-height: 44px;
		padding: 14px;
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
	}
</style>
