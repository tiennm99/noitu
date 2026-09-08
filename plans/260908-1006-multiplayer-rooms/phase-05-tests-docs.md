# Phase 5 — Tests and docs

## Go

- `game/engine_test.go` — the phase 1 invariants: four-player elimination order,
  dead-end cascade, out-of-turn resign, standings ranks, and every existing
  two-player case unchanged.
- `wsapi/wsapi_test.go` — a four-player room: join to full, fifth refused; start
  refused until all ready; kick by id; a full game with two eliminations; an
  eliminated player still receiving turn updates and chat.
- `wsapi/regression_test.go` — reconnect inside and outside the window with more
  than two players seated.
- `wsapi/wire_test.go` — fixtures for the new and changed messages;
  `go test ./internal/wsapi -update` rewrites `proto/testdata/`.
- `wsapi/convert_test.go` — unchanged mappings still exhaustive.

## JavaScript

- `tests/game-store.test.js` — the new projection, including a `turnUpdate` with
  no `played`.
- `tests/game-wire.test.js` — decodes the regenerated fixtures.
- `tests/error-codes.test.js` — every server code has Vietnamese copy.
- `e2e/pvp-game.spec.js` — a three-player game through the browser.
- `e2e/reconnect.spec.js` — unchanged 1v1 path still passes.

## Docs

- `README.md`: "online 1v1 by room code" becomes rooms of two to four, and the
  elimination rule joins the opening description.
- `docs/deployment.md` only if a knob changed — it did not.
