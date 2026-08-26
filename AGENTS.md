# Repository Guidelines

## Project Structure & Module Organization

- `src/` contains the Vue 3/TypeScript UI. Screens live in `pages/`, shared UI in `components/`, Pinia state in `stores/`, and reusable behavior in `composables/` and `utils/`.
- `src-tauri/` contains the Rust/Tauri desktop layer. Input capture, multiplayer transport, and skin hashing are under `src-tauri/src/core/`; bundled Live2D models are in `src-tauri/assets/models/`.
- `server/` is an independent Go 1.25 module implementing the in-memory WebSocket room service. Protocol types are in `server/internal/protocol/` and room logic in `server/internal/server/`.
- Frontend tests use `*.test.ts`; Go and Rust tests stay beside the code they cover.

## Build, Test, and Development Commands

Run frontend commands from the repository root:

- `pnpm install` installs dependencies; pnpm is required.
- `pnpm dev` starts Vite, while `pnpm tauri dev` runs the desktop app.
- `pnpm test` runs Vitest; `pnpm lint` applies ESLint fixes.
- `pnpm build` builds frontend assets; `pnpm tauri build` creates installers.
- `cargo test --manifest-path src-tauri/Cargo.toml` tests the desktop backend.
- `cd server && go test ./...` tests the room service; `go run ./cmd/bongocat-server` starts it on `:8080`.

## Coding Style & Naming Conventions

Use two-space indentation, single quotes, and no semicolons in TypeScript/Vue, following `eslint.config.ts`. Name Vue components in PascalCase, composables with `useX`, and stores/utilities descriptively. Format Rust with `cargo fmt` and Go with `gofmt`. Keep protocol JSON fields camelCase and message types snake_case, such as `member_joined`.

## Testing Guidelines

Add Vitest coverage for pure frontend state and protocol helpers, Rust unit tests for native state machines, and Go tests for room limits, recovery, rate limiting, and WebSocket relaying. Before submission, run all three test suites plus `pnpm build`. UI changes should be manually checked with 1, 5, and 8 players and a missing remote skin.

## Commit & Pull Request Guidelines

Use Conventional Commits (`feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`). PRs should explain user-visible and platform-specific effects, link relevant issues, list verification performed, and include screenshots or video for UI work. Update all five locale files for new text. Never commit server secrets, local endpoints, or generated build artifacts.
