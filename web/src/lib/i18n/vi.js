import { RejectReason, GameEndReason, Difficulty } from '$lib/proto/noitu/v1/game_pb.js';

/**
 * Every user-facing string in the app. Nothing outside this file writes
 * Vietnamese prose, which is what lets the server send UI keys such as
 * "room_not_found" instead of sentences.
 */
export const t = {
	appName: 'Nối Từ',
	tagline: 'Trò chơi nối từ tiếng Việt',

	nicknameLabel: 'Tên của bạn',
	nicknamePlaceholder: 'Nhập tên hiển thị',
	nicknameHint: 'Tối đa 20 ký tự. Máy chủ có thể rút gọn tên của bạn.',

	playBot: 'Chơi với máy',
	playOnline: 'Đấu trực tuyến',
	difficultyLabel: 'Độ khó',
	back: 'Quay lại',
	dismiss: 'Bỏ qua',
	home: 'Về trang chủ',
	rematch: 'Chơi lại',
	resign: 'Đầu hàng',
	resignConfirm: 'Bạn chắc chắn muốn đầu hàng?',

	yourTurn: 'Đến lượt bạn',
	opponentTurn: 'Đối thủ đang suy nghĩ…',
	currentSyllable: 'Nối tiếp tiếng',
	wordInputPlaceholder: 'Nhập từ của bạn',
	submit: 'Gửi',
	chainTitle: 'Chuỗi từ',
	chainEmpty: 'Chưa có từ nào.',
	you: 'Bạn',
	opponent: 'Đối thủ',
	scoreLabel: 'Điểm',
	syllableUnit: 'tiếng',
	correctedFrom: 'Bạn gõ “{typed}”, từ đúng là “{word}”.',

	secondsLeft: '{n} giây',
	waiting: 'Đang chờ…',
	connecting: 'Đang kết nối…',
	connected: 'Đã kết nối',
	reconnecting: 'Mất kết nối, đang thử lại…',
	offline: 'Không có kết nối',

	won: 'Bạn thắng!',
	lost: 'Bạn thua.',
	finalScore: 'Điểm cuối cùng',
	chainLength: 'Số từ trong chuỗi',
	newRecord: 'Kỷ lục mới!',
	bestScore: 'Kỷ lục',
	noBestScore: 'Chưa có',

	theme: 'Giao diện',
	themeLight: 'Sáng',
	themeDark: 'Tối',

	onlineTitle: 'Đấu trực tuyến',
	onlineComingSoon: 'Chế độ đấu 1v1 sẽ có ở bản cập nhật tiếp theo.',

	attributionIntro: 'Từ điển dựa trên',
	attributionSource: 'minhqnd/dictionary',
	attributionLicense: 'giấy phép CC BY-SA 4.0',
	attributionMiddle: 'phát hành theo'
};

/**
 * Bot difficulty labels, keyed by the proto enum.
 *
 * @type {Record<number, string>}
 */
export const difficultyLabels = {
	[Difficulty.EASY]: 'Dễ',
	[Difficulty.MEDIUM]: 'Trung bình',
	[Difficulty.HARD]: 'Khó'
};

/** The three difficulties offered on the home screen, in ladder order. */
export const difficultyOrder = [Difficulty.EASY, Difficulty.MEDIUM, Difficulty.HARD];

/**
 * Why a word was refused. `{syllable}` is filled from the syllable the server
 * is currently asking for — the reason alone does not say which one it was.
 *
 * @type {Record<number, string>}
 */
export const rejectMessages = {
	[RejectReason.UNSPECIFIED]: 'Từ không hợp lệ.',
	[RejectReason.TOO_FEW_SYLLABLES]: 'Từ phải có ít nhất 2 tiếng.',
	[RejectReason.WRONG_LINK]: 'Từ phải bắt đầu bằng tiếng “{syllable}”.',
	[RejectReason.NOT_IN_DICTIONARY]: 'Không tìm thấy từ này trong từ điển.',
	[RejectReason.ALREADY_USED]: 'Từ này đã được dùng rồi.',
	[RejectReason.NOT_YOUR_TURN]: 'Chưa đến lượt bạn.',
	[RejectReason.TIMEOUT]: 'Hết giờ!',
	[RejectReason.GAME_OVER]: 'Ván đấu đã kết thúc.'
};

/**
 * How a finished game ended, phrased from the losing or winning side.
 *
 * @type {Record<number, string>}
 */
export const endReasonMessages = {
	[GameEndReason.UNSPECIFIED]: '',
	[GameEndReason.TIMEOUT]: 'Hết thời gian suy nghĩ.',
	[GameEndReason.NO_LEGAL_MOVE]: 'Không còn từ nào nối được.',
	[GameEndReason.OPPONENT_LEFT]: 'Đối thủ đã rời trận.',
	[GameEndReason.RESIGNED]: 'Có người đầu hàng.'
};

/**
 * ServerError.code is a UI key, so this is where those keys become sentences.
 * An unknown code falls back to `errorFallback` rather than showing the raw
 * key: a key leaking into the UI is a bug, not a message.
 *
 * @type {Record<string, string>}
 */
export const errorMessages = {
	already_greeted: 'Phiên chơi đã được mở rồi.',
	bad_frame: 'Máy chủ không đọc được dữ liệu gửi lên.',
	busy: 'Bạn đang ở trong một ván đấu khác.',
	cannot_join_own_room: 'Bạn không thể vào phòng của chính mình.',
	game_already_over: 'Ván đấu đã kết thúc.',
	game_not_started: 'Ván đấu chưa bắt đầu.',
	game_start_failed: 'Không thể bắt đầu ván đấu. Hãy thử lại.',
	handshake_required: 'Phiên chơi chưa sẵn sàng. Hãy tải lại trang.',
	not_in_a_game: 'Bạn không ở trong ván đấu nào.',
	not_your_seat: 'Bạn không phải người chơi trong ván này.',
	protocol_version_mismatch: 'Phiên bản đã cũ. Hãy tải lại trang.',
	room_full: 'Phòng đã đủ người.',
	room_not_found: 'Không tìm thấy phòng với mã này.',
	room_start_failed: 'Không thể tạo phòng. Hãy thử lại.',
	server_restarting: 'Máy chủ đang khởi động lại. Hãy thử lại sau giây lát.',
	session_not_resumable: 'Không khôi phục được ván đấu trước.',
	too_fast: 'Bạn thao tác quá nhanh. Chậm lại một chút nhé.',
	too_many_attempts: 'Bạn thử vào phòng quá nhiều lần. Hãy đợi một lát.',
	too_many_rooms: 'Bạn tạo phòng quá nhanh. Hãy đợi một lát.',
	unknown_difficulty: 'Độ khó không hợp lệ.'
};

export const errorFallback = 'Đã có lỗi xảy ra. Hãy thử lại.';

/**
 * Fills `{name}` placeholders. Keeping interpolation here means a message can
 * gain a placeholder without every call site learning about it.
 *
 * @param {string} template
 * @param {Record<string, string | number>} [values]
 * @returns {string}
 */
export function fill(template, values = {}) {
	return template.replace(/\{(\w+)\}/g, (whole, key) =>
		key in values ? String(values[key]) : whole
	);
}

/**
 * @param {number} reason - a RejectReason enum value
 * @param {string} syllable - the syllable the server is currently asking for
 * @returns {string}
 */
export function rejectMessage(reason, syllable) {
	const template = rejectMessages[reason] ?? rejectMessages[RejectReason.UNSPECIFIED];
	return fill(template, { syllable });
}

/**
 * @param {string} code - ServerError.code
 * @returns {string}
 */
export function errorMessage(code) {
	return errorMessages[code] ?? errorFallback;
}
