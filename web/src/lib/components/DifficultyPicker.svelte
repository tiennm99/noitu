<script>
	import { difficultyLabels, difficultyOrder, t } from '$lib/i18n/vi.js';
	import { settings } from '$lib/stores/settings.svelte.js';

	/** @type {{ value: number, onselect?: (difficulty: number) => void }} */
	let { value = $bindable(), onselect } = $props();
</script>

<!--
	Real radio inputs rather than buttons wearing role="radio". Arrow-key
	movement, the single tab stop and the grouping all come from the platform,
	which is a lot of behaviour to reimplement and get subtly wrong.
-->
<fieldset class="picker">
	<legend>{t.difficultyLabel}</legend>
	<div class="options">
		{#each difficultyOrder as difficulty (difficulty)}
			{@const best = settings.bestScore(difficulty)}
			<label class="option" class:selected={value === difficulty}>
				<input
					type="radio"
					name="difficulty"
					class="sr-only"
					checked={value === difficulty}
					onchange={() => {
						value = difficulty;
						onselect?.(difficulty);
					}}
				/>
				<span class="name">{difficultyLabels[difficulty]}</span>
				<span class="best">{t.bestScore}: {best > 0 ? best : t.noBestScore}</span>
			</label>
		{/each}
	</div>
</fieldset>

<style>
	.picker {
		border: 0;
		margin: 0;
		padding: 0;
	}

	legend {
		padding: 0 0 6px;
		font-weight: 600;
		font-size: 0.9rem;
	}

	.options {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 8px;
	}

	.option {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 12px 8px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		cursor: pointer;
		text-align: center;
	}

	.option.selected {
		border-color: var(--accent);
		background: var(--accent-soft);
	}

	/* The input is visually hidden, so its focus ring has to land on the label
	   the player can actually see. */
	.option:focus-within {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}

	.name {
		font-weight: 600;
	}

	.best {
		color: var(--text-muted);
		font-size: 0.75rem;
	}
</style>
