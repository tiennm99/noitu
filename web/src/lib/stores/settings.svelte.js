/**
 * Client-owned state: the three things the server has no opinion about.
 *
 * Every read and write goes through the guards below because localStorage is
 * not always there. A private window, a browser configured to block site data,
 * or a storage quota that is already full each fail differently, and some fail
 * on the property access itself rather than on the call. A player in that
 * state should lose their preferences, not the game.
 */

const NICKNAME_KEY = 'noitu.nickname';
const THEME_KEY = 'noitu.theme';
const BEST_KEY = 'noitu.bestScores';

/** Mirrors the server's own cap so the input cannot promise a name it will lose. */
export const MAX_NICKNAME_LENGTH = 20;

/** @returns {Storage | null} */
function safeStorage() {
	try {
		return globalThis.localStorage ?? null;
	} catch {
		return null;
	}
}

/**
 * @param {string} key
 * @param {string} fallback
 * @returns {string}
 */
function read(key, fallback) {
	try {
		return safeStorage()?.getItem(key) ?? fallback;
	} catch {
		return fallback;
	}
}

/**
 * @param {string} key
 * @param {string} value
 */
function write(key, value) {
	try {
		safeStorage()?.setItem(key, value);
	} catch {
		// In-memory state stays correct for this session; only persistence is
		// lost, and there is no useful recovery from a storage refusal.
	}
}

/**
 * Best scores are stored as one JSON object keyed by difficulty. Corrupt or
 * hand-edited JSON degrades to "no records yet" rather than throwing on load.
 *
 * @returns {Record<string, number>}
 */
function readBestScores() {
	const raw = read(BEST_KEY, '');
	if (!raw) return {};
	try {
		const parsed = JSON.parse(raw);
		if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return {};
		/** @type {Record<string, number>} */
		const clean = {};
		for (const [key, value] of Object.entries(parsed)) {
			if (typeof value === 'number' && Number.isFinite(value) && value >= 0) {
				clean[key] = value;
			}
		}
		return clean;
	} catch {
		return {};
	}
}

/**
 * @param {string} value
 * @returns {'light' | 'dark'}
 */
function normalizeTheme(value) {
	return value === 'dark' ? 'dark' : 'light';
}

/** Reads the theme the inline script in app.html already applied, if any. */
function initialTheme() {
	const saved = read(THEME_KEY, '');
	if (saved === 'dark' || saved === 'light') return saved;
	try {
		return globalThis.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
	} catch {
		return 'light';
	}
}

export function createSettingsStore() {
	const state = $state({
		nickname: read(NICKNAME_KEY, ''),
		/** @type {'light' | 'dark'} */
		theme: initialTheme(),
		/** @type {Record<string, number>} */
		bestScores: readBestScores()
	});

	return {
		state,

		/** @param {string} value */
		setNickname(value) {
			const trimmed = [...value].slice(0, MAX_NICKNAME_LENGTH).join('');
			state.nickname = trimmed;
			write(NICKNAME_KEY, trimmed);
		},

		/** @param {'light' | 'dark'} value */
		setTheme(value) {
			const theme = normalizeTheme(value);
			state.theme = theme;
			write(THEME_KEY, theme);
			try {
				globalThis.document?.documentElement?.setAttribute('data-theme', theme);
			} catch {
				// No document under test; the state above is what matters.
			}
		},

		toggleTheme() {
			this.setTheme(state.theme === 'dark' ? 'light' : 'dark');
		},

		/**
		 * @param {number|string} difficulty - a Difficulty enum value
		 * @returns {number}
		 */
		bestScore(difficulty) {
			return state.bestScores[String(difficulty)] ?? 0;
		},

		/**
		 * Records a score and reports whether it beat the previous best, which
		 * is what the game-over screen needs to show the record marker. Ties do
		 * not count: matching your own record is not setting a new one.
		 *
		 * @param {number|string} difficulty
		 * @param {number} score
		 * @returns {boolean}
		 */
		recordScore(difficulty, score) {
			const key = String(difficulty);
			const previous = state.bestScores[key] ?? 0;
			if (score <= previous) return false;
			state.bestScores = { ...state.bestScores, [key]: score };
			write(BEST_KEY, JSON.stringify(state.bestScores));
			return true;
		}
	};
}

/** The store the routes share. */
export const settings = createSettingsStore();
