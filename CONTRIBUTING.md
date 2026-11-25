# Contributing to Homing

Start with a focused issue or pull request describing the user-visible problem, the expected behavior and the platforms affected. For security issues, follow [SECURITY.md](SECURITY.md) instead of posting secrets or exploit details publicly.

## Development

Read [AGENTS.md](AGENTS.md) and [the architecture guide](docs/go-architecture.md) before editing this shared repository. Coordinate changes across owned directories. Preserve unrelated work and stage only the files belonging to your change.

Requirements: Go 1.26, JDK 17 and Android SDK 35 for Android, and Node.js 22 for protocol / legacy relay tests or console development. Android builds use the committed Gradle wrapper.

Run an isolated local service with a new data directory and an unused port:

```sh
go run -tags headless ./cmd/homing serve --addr 127.0.0.1:18788 --data-dir /tmp/homing-demo-data
```

Use synthetic `demo-*` fixtures. Real Agent tests must create new sessions in a temporary project; never inject test prompts into somebody's existing conversation. Keep pairing tokens, credentials, transcripts, screenshots and local databases out of commits.

## Validation

Run checks relevant to the change:

```sh
go build ./...
go vet -tags headless ./...
go test -tags headless ./...
go test -race -tags headless ./internal/cloud ./internal/api
```

On Linux without desktop GUI dependencies, use `go build -tags headless ./...`. For Android:

```sh
cd android
./gradlew :app:testDebugUnitTest :app:assembleDebug :app:lintDebug
```

Protocol changes must retain compatibility with the shared vectors:

```sh
cd packages/protocol-js
npm ci
npm test
```

Run `gitleaks git .` before submitting. Inspect the staged diff as well: a secret detector does not reliably identify private conversation text. New tests should prove the reported behavior, including failure before a bug fix when practical.

## Pull requests

Explain the problem and resulting behavior, include validation results, and distinguish automated checks from physical-device evidence. State any untested platform or runtime prerequisite. Avoid reformatting unrelated files or committing generated credentials and local build outputs.

Do not change a running user's service or install an APK with a different signing identity as part of ordinary tests. Desktop service cutovers need an agreed maintenance window, consistent data backups and a tested rollback path.

Contributions are accepted under the project's Apache-2.0 license. Preserve third-party notices when adding or redistributing assets and dependencies.
