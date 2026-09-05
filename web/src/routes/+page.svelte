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

<section class="home">
	<p class="tagline">{t.tagline}</p>

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
		gap: 20px;
		padding-top: 12px;
	}

	.tagline {
		margin: 0;
		color: var(--text-muted);
	}

	.actions {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}

	.actions > * {
		padding: 14px;
		border: 1px solid var(--border);
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
