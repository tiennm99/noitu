package wsapi

// errCode is a ServerError.code: a key the client turns into a message, not
// text shown as is. Every code the transport can send is declared here and
// nowhere else, so the vocabulary the web client has to translate is one list
// — web/tests/error-codes.test.js reads it from this file.
type errCode string

const (
	codeAlreadyGreeted          errCode = "already_greeted"
	codeAlreadyInAGame          errCode = "already_in_a_game"
	codeAlreadyInARoom          errCode = "already_in_a_room"
	codeAlreadyQueued           errCode = "already_queued"
	codeBadFrame                errCode = "bad_frame"
	codeBusy                    errCode = "busy"
	codeCannotJoinOwnRoom       errCode = "cannot_join_own_room"
	codeCannotKickSelf          errCode = "cannot_kick_self"
	codeGameAlreadyOver         errCode = "game_already_over"
	codeGameInProgress          errCode = "game_in_progress"
	codeGameNotStarted          errCode = "game_not_started"
	codeGameStartFailed         errCode = "game_start_failed"
	codeHandshakeRequired       errCode = "handshake_required"
	codeKicked                  errCode = "kicked"
	codeMustUnreadyFirst        errCode = "must_unready_first"
	codeNeedMorePlayers         errCode = "need_more_players"
	codeNoOneToKick             errCode = "no_one_to_kick"
	codeNotADeadEnd             errCode = "not_a_dead_end"
	codeNotEveryoneReady        errCode = "not_everyone_ready"
	codeNotInAGame              errCode = "not_in_a_game"
	codeNotInARoom              errCode = "not_in_a_room"
	codeNotTheOwner             errCode = "not_the_owner"
	codeNotYourSeat             errCode = "not_your_seat"
	codeNotYourTurn             errCode = "not_your_turn"
	codeOwnerNeedsNoReady       errCode = "owner_needs_no_ready"
	codePlayerIsReady           errCode = "player_is_ready"
	codePlayerOffline           errCode = "player_offline"
	codeProtocolVersionMismatch errCode = "protocol_version_mismatch"
	codeRoomFull                errCode = "room_full"
	codeRoomIdleClosed          errCode = "room_idle_closed"
	codeRoomNotFound            errCode = "room_not_found"
	codeRoomStartFailed         errCode = "room_start_failed"
	codeServerFull              errCode = "server_full"
	codeServerRestarting        errCode = "server_restarting"
	codeSessionNotResumable     errCode = "session_not_resumable"
	codeTooFast                 errCode = "too_fast"
	codeTooManyAttempts         errCode = "too_many_attempts"
	codeTooManyRooms            errCode = "too_many_rooms"
	codeUnknownMessage          errCode = "unknown_message"
	codeUnknownDifficulty       errCode = "unknown_difficulty"
	codeWordReportLimit         errCode = "word_report_limit"
	codeWordReportRefused       errCode = "word_report_refused"
)
