import { RejectReason, GameEndReason, Difficulty } from '$lib/proto/noitu/v1/game_pb.js';

/**
 * Every user-facing string in the app. Nothing outside this file writes
 * Vietnamese prose, which is what lets the server send UI keys such as
 * "room_not_found" instead of sentences.
 */
export const t = {
	appName: 'Nối Từ',
	tagline: 'Trò chơi nối từ tiếng Việt',
	skipToContent: 'Tới nội dung chính',

	// Document titles. One per screen, so the tab strip, the browser history
	// and a screen reader's page announcement say which screen this is.
	titleHome: 'Nối Từ — trò chơi nối từ tiếng Việt',
	titlePlay: 'Chơi với máy · Nối Từ',
	titleOnline: 'Đấu trực tuyến · Nối Từ',
	titleRoom: 'Phòng {code} · Nối Từ',

	nicknameLabel: 'Tên của bạn',
	nicknamePlaceholder: 'Nhập tên hiển thị',
	nicknameHint:
		'Tối đa 20 ký tự. Máy chủ có thể rút gọn tên của bạn. Để trống sẽ được gọi là “Người chơi”.',
	nicknameNeeded: 'Nhập tên của bạn trước khi vào phòng.',

	playBot: 'Chơi với máy',
	playOnline: 'Đấu trực tuyến',
	difficultyLabel: 'Độ khó',
	back: 'Quay lại',
	dismiss: 'Bỏ qua',
	home: 'Về trang chủ',
	rematch: 'Chơi lại',
	resign: 'Đầu hàng',
	// Short enough to sit inside the button it arms, and it still contains the
	// word the first press was labelled with, so the control reads as the same
	// control asking again rather than as a different one.
	resignSure: 'Chắc chắn đầu hàng?',
	retry: 'Thử lại',

	yourTurn: 'Đến lượt bạn',
	opponentTurn: 'Đối thủ đang suy nghĩ…',
	playerTurn: 'Đến lượt {name}…',
	currentSyllable: 'Nối tiếp tiếng',
	wordInputPlaceholder: 'Nhập từ của bạn',
	// The field's own off-turn text. Short on purpose: it shares a row with
	// the send button, and the turn line above it is where the name goes.
	wordInputWaiting: 'Chưa đến lượt bạn',
	wordInputOffline: 'Mất kết nối…',
	submit: 'Gửi',
	chainTitle: 'Chuỗi từ',
	chainEmpty: 'Chưa có từ nào.',
	meaningShow: 'Xem nghĩa của {word}',
	meaningHide: 'Ẩn nghĩa của {word}',
	meaningNone: 'Chưa có nghĩa',
	you: 'Bạn',
	opponent: 'Đối thủ',
	someone: 'Người chơi',
	syllableUnit: 'tiếng',
	correctedFrom: 'Bạn gõ “{typed}”, từ đúng là “{word}”.',

	secondsLeft: '{n} giây',
	yourTimeLeft: 'Còn {n} giây cho lượt của bạn',
	connecting: 'Đang kết nối…',
	connected: 'Đã kết nối',
	reconnecting: 'Mất kết nối, đang thử lại…',
	noConnection: 'Không có kết nối',
	connectStalled: 'Chưa kết nối được máy chủ. Kiểm tra mạng rồi thử lại.',

	won: 'Bạn thắng!',
	lost: 'Bạn thua.',
	finalScore: 'Điểm cuối cùng',
	chainLength: 'Số từ trong chuỗi',
	standingsTitle: 'Kết quả',
	winnerBadge: 'Vô địch',
	pointsUnit: 'điểm',
	newRecord: 'Kỷ lục mới!',
	suggestionsTitle: 'Bạn có thể nối',
	noSuggestions: 'Không còn từ nào bắt đầu bằng tiếng “{syllable}”. Ai gặp thế này cũng chịu thôi!',
	exportHistory: 'Tải chuỗi từ',
	exportOpening: 'từ mở đầu',
	bestScore: 'Kỷ lục',
	noBestScore: 'Chưa có',

	theme: 'Giao diện',
	themeLight: 'Sáng',
	themeDark: 'Tối',

	onlineTitle: 'Đấu trực tuyến',
	onlineIntro: 'Tạo phòng rồi gửi mã cho bạn bè, hoặc nhập mã bạn được mời.',
	createRoom: 'Tạo phòng',
	joinRoom: 'Vào phòng',
	roomCodeLabel: 'Mã phòng',
	roomCodePlaceholder: 'VD: K7M2QP',
	roomCodeHint: 'Sáu ký tự. Không phân biệt hoa thường.',
	roomCodeInvalid: 'Mã phòng gồm sáu ký tự.',
	copyCode: 'Sao chép mã',
	copyLink: 'Sao chép liên kết mời',
	shareLink: 'Chia sẻ',
	copied: 'Đã sao chép',
	copyFailed: 'Không sao chép được. Hãy chọn và sao chép thủ công.',
	inviteLinkLabel: 'Liên kết mời',
	leaveRoom: 'Rời phòng',
	shareInvite: 'Vào chơi nối từ với tôi',

	chatTitle: 'Trò chuyện',
	chatPlaceholder: 'Nhắn tin…',
	chatEmpty: 'Chưa có tin nhắn nào.',
	chatUnread: '{n} tin mới',
	chatAuthorLeft: 'Đã rời phòng',

	// The series score: how many games each player has won since the room
	// opened, which is a room fact rather than a game one.
	winsLabel: 'Tỉ số',

	lobbyTitle: 'Phòng chờ',
	owner: 'Chủ phòng',
	guest: 'Khách',
	playerCount: '{n}/{max} người chơi',
	ready: 'Sẵn sàng',
	unready: 'Bỏ sẵn sàng',
	isReady: 'Đã sẵn sàng',
	notReady: 'Chưa sẵn sàng',
	startGame: 'Bắt đầu',
	kickPlayer: 'Mời ra khỏi phòng',
	// Contains the label the unarmed button carries, for the same reason
	// resignSure does.
	kickSure: 'Chắc chắn mời ra khỏi phòng?',
	emptySeat: 'Còn trống',
	offline: 'Mất kết nối',
	ownerStartsHint: 'Bạn là chủ phòng. Bắt đầu khi mọi người đã sẵn sàng.',
	ownerNeedsMore: 'Cần ít nhất {n} người mới bắt đầu được. Gửi mã phòng cho bạn bè!',
	guestReadyHint: 'Bấm sẵn sàng rồi chờ chủ phòng bắt đầu.',
	waitingForStart: 'Đang chờ chủ phòng bắt đầu…',
	ownerAway: 'Chủ phòng đang mất kết nối. Chờ một chút hoặc rời phòng.',
	unreadyToLeave: 'Bỏ sẵn sàng trước khi rời phòng.',

	playerDisconnected: '{name} mất kết nối…',
	playerDisconnectedIn: '{name} mất kết nối… ({n}s)',
	youAreOut: 'Bạn đã bị loại. Ván đấu vẫn đang tiếp tục.',
	spectating: 'Bạn đang xem ván đấu.',
	playerOut: '{name} đã bị loại.',
	// Left rather than knocked out: the seat is gone from the room, not just
	// out of the game, so the others are not waiting for anybody.
	playerLeft: '{name} đã rời phòng.',
	eliminated: 'Đã bị loại',

	attributionIntro: 'Từ điển dựa trên',
	attributionSource: 'Wiktionary tiếng Việt',
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
	[GameEndReason.OPPONENT_LEFT]: 'Có người đã rời trận.',
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
	busy: 'Máy chủ đang bận. Hãy thử lại.',
	cannot_join_own_room: 'Bạn không thể vào phòng của chính mình.',
	game_already_over: 'Ván đấu đã kết thúc.',
	game_in_progress: 'Ván đấu đang diễn ra.',
	game_not_started: 'Ván đấu chưa bắt đầu.',
	game_start_failed: 'Không thể bắt đầu ván đấu. Hãy thử lại.',
	handshake_required: 'Phiên chơi chưa sẵn sàng. Hãy tải lại trang.',
	kicked: 'Bạn đã bị mời ra khỏi phòng.',
	cannot_kick_self: 'Bạn không thể tự mời mình ra khỏi phòng.',
	must_unready_first: 'Hãy bỏ sẵn sàng trước khi rời phòng.',
	need_more_players: 'Cần ít nhất hai người chơi mới bắt đầu được.',
	not_everyone_ready: 'Vẫn còn người chưa sẵn sàng.',
	not_in_a_game: 'Bạn không ở trong ván đấu nào.',
	not_in_a_room: 'Bạn không ở trong phòng nào.',
	not_the_owner: 'Chỉ chủ phòng làm được việc này.',
	not_your_seat: 'Bạn không phải người chơi trong ván này.',
	not_your_turn: 'Chỉ đầu hàng được trong lượt của bạn.',
	no_one_to_kick: 'Chưa có ai trong phòng để mời ra.',
	player_offline: 'Vẫn còn người đang mất kết nối.',
	owner_needs_no_ready: 'Chủ phòng không cần bấm sẵn sàng.',
	player_is_ready: 'Không thể mời một người đã sẵn sàng ra khỏi phòng.',
	protocol_version_mismatch: 'Phiên bản đã cũ. Hãy tải lại trang.',
	room_full: 'Phòng đã đủ người.',
	room_idle_closed: 'Phòng đã đóng vì không có ván nào được bắt đầu.',
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
