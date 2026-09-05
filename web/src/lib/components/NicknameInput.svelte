<script>
	import { t } from '$lib/i18n/vi.js';
	import { MAX_NICKNAME_LENGTH, settings } from '$lib/stores/settings.svelte.js';

	const id = 'nickname-input';
</script>

<div class="field">
	<label for={id}>{t.nicknameLabel}</label>
	<!--
		The value is never written back while the player types. A Telex or VNI
		IME composes a diacritic across several keystrokes, and reassigning the
		field mid-composition drops the accent being formed. The cap is applied
		in the store, which is also where the same rule is unit-tested.
	-->
	<input
		{id}
		type="text"
		autocomplete="nickname"
		maxlength={MAX_NICKNAME_LENGTH}
		placeholder={t.nicknamePlaceholder}
		value={settings.state.nickname}
		oninput={(event) => settings.setNickname(event.currentTarget.value)}
	/>
	<p class="hint">{t.nicknameHint}</p>
</div>

<style>
	.field {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}

	label {
		font-weight: 600;
		font-size: 0.9rem;
	}

	input {
		padding: 12px 14px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		font-size: 1rem;
	}

	input:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}

	.hint {
		margin: 0;
		color: var(--text-muted);
		font-size: 0.8rem;
	}
</style>
