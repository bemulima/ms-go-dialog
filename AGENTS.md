# Repository Guidelines

## Agent bootstrap

Before planning or changing files, read these sources in order:

1. `.ai/rules/common.md` for shared delivery policy.
2. `.ai/service.yaml` for this service's capabilities, contracts, commands, and workflows.
3. `docs/README.md` and only the linked documents relevant to the task.
4. `.ai/manifest.yaml` when adding or moving agent-facing documentation.

The issue and its linked pull request are the durable task record. Do not create a parallel task journal. Repository-local code, migrations, contracts, and documentation are the source of truth for behavior.

## Project structure and boundaries

- `cmd/ms-dialog-service`: composition root for REST, realtime, and worker modes.
- `internal/domain`: dialog, membership, message, attachment, ticket, outbox, and stable error contracts.
- `internal/usecase`: transport-independent business processes and transaction boundaries.
- `internal/transport/http`: inbound chi transport split into `api/v1`, `admin/v1`, `private`, and `common`; it maps requests and trusted identity only.
- `internal/transport/websocket`: ticket-authenticated realtime projection; durable commands stay in use cases.
- `internal/transport/message`: inbound NATS realtime subscription and WebSocket fan-out boundary.
- `internal/infrastructure/persistence/postgres`: pgx repositories; keep SQL and reversible migrations aligned.
- `internal/infrastructure/messaging/nats`: outbox-backed lifecycle delivery and ephemeral typing fan-out.
- `internal/infrastructure/filestorage`, `filescan`, and `http/user`: outbound FileStorage, ClamAV, and participant-resolution integrations.
- `internal/infrastructure/health` and `observability`: outbound operational dependencies and bounded-cardinality metrics.
- `db/migrations`: ordered reversible schema contracts owned only by this service.
- `docs` and `.ai/contracts`: human and machine-readable contract sources; update both when behavior changes.

## Commands and testing

- Use only commands registered in `.ai/commands.yaml`.
- Run `make agent-policy` after changing agent instructions or documentation links.
- Run `make validate-contracts` after changing service contracts.
- Tests live next to Go code and under `test/`; prefer table-driven cases and regression coverage.
- Cover personal-pair concurrency, group roles, idempotency, optimistic locking, per-member read state, attachments, and reconnect gaps where relevant.

## Coding and security invariants

- Keep handlers thin; business logic belongs in `internal/usecase`, persistence in `internal/infrastructure/persistence/postgres`.
- `internal/domain` and `internal/usecase` must not import transport, infrastructure, or framework implementations. Dependency construction and process lifecycle belong only in `cmd/ms-dialog-service`.
- Use context-aware functions and `Err*` names for sentinel errors; run `gofmt`/`goimports` on changed Go files.
- Trust `X-User-ID` and `X-User-Role` only behind the configured gateway.
- Never accept raw HTML; treat message bodies, links, filenames, and attachment metadata as untrusted input.
- Keep migrations ordered, reversible, and synchronized with database documentation.

## Delivery

- Follow `.ai/rules/common.md` and the task-specific workflow under `.ai/workflows/`.
- Use focused conventional-style commits when a commit is requested.
- A PR must state verification and impacts on migrations, HTTP, WebSocket, NATS, and owned documentation.
- Do not push, open or merge a PR, deploy, or mutate an issue without explicit authorization.
