---
title: "Codebase review: stale and legacy cleanup"
date: 2026-09-08
mode: codebase
verdict: clean, behaviour unchanged
---

# Codebase review: stale and legacy cleanup

Scope: whole repo at `f00d0ef`. Tools: `deadcode` (with and without tests), `staticcheck`,
`go vet`, a script over `web/src` for unused i18n keys, unused `lib` exports, unimported
components and unread CSS variables, a grep for plan-phase citations and legacy names.

## Findings and what was done

| # | Finding | Evidence | Action |
|---|---|---|---|
| 1 | `bot.BoardFor` + `engineBoard` unreachable from `main`; used by tests only | `deadcode ./...`; callers in `simulate_test.go`, `realcorpus_test.go` | moved to `internal/bot/board_test.go`; `game` import dropped from `bot.go` |
| 2 | `hub.roomCount` test probe in production code | `deadcode`; callers in two `_test.go` | moved to `internal/wsapi/hub_test.go` |
| 3 | `err != nil` always true in `session.serve`: `readLoop` never returns nil | staticcheck SA4023 | condition reduced to the `context.Canceled` check, comment says why |
| 4 | Invisible characters as literals in test strings (BEL, ZWSP, ZWJ, RLO) | staticcheck ST1018, `wsapi_test.go:881-882`, `wikitext_test.go:62` | escapes |
| 5 | `!b.allow(now) \|\| !b.allow(now)` reads as a typo | SA4000 | a two-iteration loop with the call number in the failure |
| 6 | Comments citing plan phases ("from phase 5 on", "until phase 6", "phase-7 screens") | grep | reworded to describe the code |
| 7 | `.dockerignore` lets the 63 MB dump into the build context | file | `data/*.bz2`, `data/*.bz2.part`, `data/*.xml` ignored |

## Checked and left alone

- i18n: 101 keys, all read. Lib exports: all imported. Components: all imported.
  `--player-1..4`: read dynamically in `ChatPanel.svelte`. `jsdom`: two tests declare it.
- No `TODO`/`FIXME`. No kaikki/wiktextract/jsonl outside `plans/`. Root tracked files all live.
- `gofmt -l` lists every Go file on this checkout because `core.autocrlf=true` checks them out
  with CRLF; the index is LF (`git ls-files --eol`). Not a code issue.
- "wordlist" in README/CI/Dockerfile comments: still the right word for the licence and
  leak-guard sentences; not renamed.

## Verification

`go vet`, `deadcode` (empty), `staticcheck` (clean), `go test ./... -race`, `npm run check`,
`npm test` (183): all green after the change. No behaviour changed: every edit is a move of
test-only code, a comment, a test literal, a dead condition, or a build-context ignore.
