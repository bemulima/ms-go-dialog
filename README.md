# ms-go-dialog

`ms-go-dialog` is an independent Go domain service for authenticated personal, group, and contextual PersonalTeacher messaging. It owns dialogs, membership, explicit user/teacher message authorship, per-member read state, attachment authorization, realtime sequencing, moderation state, and transactional event delivery. It does not own user profiles, PersonalTeacher pedagogy, student mastery, or file bytes.

## Architecture

- `cmd/ms-dialog-service`: process composition and graceful lifecycle.
- `internal/domain`: entities, invariants, stable errors, repository ports.
- `internal/usecase`: transaction-scoped business processes.
- `internal/transport/http`: Chi REST transport split into API, admin, and private contours.
- `internal/transport/websocket` and `internal/transport/message`: single-use-ticket realtime projection and inbound NATS fan-out.
- `internal/infrastructure/persistence/postgres`: pgx persistence and transaction manager.
- `internal/infrastructure/messaging/nats`: JetStream outbox delivery and Core NATS typing.
- `internal/infrastructure/filestorage`, `filescan`, and `http/user`: staged file lifecycle, scanning, and active-participant validation.
- `internal/infrastructure/health` and `observability`: dependency readiness and bounded-cardinality Prometheus metrics.
- `docs` and `.ai`: human and machine-readable contract sources.

## Local commands

```sh
cp .env.dist .env
make deps
make test
make validate-contracts
make up
make migrate
```

The all-in-one local runtime is available at `http://localhost:8095` by default. Set `DIALOG_PORT` to override only the host port; containers on `ms-net` continue to use `ms-dialog-service:8080`.

PostgreSQL adapter smoke tests are opt-in and roll their fixtures back:

```sh
DIALOG_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5444/ms_dialog?sslmode=disable' go test ./internal/infrastructure/persistence/postgres
```

The external gateway namespace is `/api/dialog/v1/*`; the service owns `/api/v1/*`. `X-User-ID` and `X-User-Role` are trusted only from `ms-gateway`. WebSocket authentication uses a short-lived single-use ticket in `Sec-WebSocket-Protocol`, never a bearer token in the URL.

Lesson-context messages preserve a strict `lesson-message-context.v1` anchor:
Course UUID, lesson UUID, RFC3339Nano content revision, and either an overview
mode or the exact student-selected text. Other teacher contexts use their
existing LearningAction binding. Dialog owns only this immutable message
anchor; Teacher verifies it against current Course content before responding.

Runtime modes are `api`, `realtime`, `worker`, and `all`. PostgreSQL is always required; realtime/worker modes require NATS JetStream, participant membership mutations require the private `ms-go-user` batch API, attachment operations require FileStorage, and generic-file uploads require ClamAV. See [integration contract](docs/integration-contract.md) and [frontend/backend map](docs/frontend-backend-contract-map.md).

Operational endpoints are `GET /healthz` (process liveness), `GET /readyz` (mode-aware PostgreSQL/NATS readiness), and `GET /metrics` (Prometheus text format). They belong on the private service port and must not be routed through the public dialog namespace. See [operations](docs/operations.md).
