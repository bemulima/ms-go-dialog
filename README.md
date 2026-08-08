# ms-go-dialog

`ms-go-dialog` is an independent Go domain service for authenticated personal and group messaging. It owns dialogs, membership, messages, per-member read state, attachment authorization, realtime sequencing, moderation state, and transactional event delivery. It does not own user profiles or file bytes.

## Architecture

- `cmd/ms-dialog-service`: process composition and graceful lifecycle.
- `internal/domain`: entities, invariants, stable errors, repository ports.
- `internal/usecase`: transaction-scoped business processes.
- `internal/adapters/http`: Chi REST adapters and trusted gateway boundary.
- `internal/adapters/websocket`: single-use-ticket realtime projection.
- `internal/adapters/postgres`: pgx persistence and transaction manager.
- `internal/adapters/nats`: JetStream outbox delivery and Core NATS typing.
- `internal/adapters/filestorage`: staged file lifecycle.
- `internal/adapters/health` and `observability`: dependency readiness and bounded-cardinality Prometheus metrics.
- `docs` and `.ai`: human and machine-readable contract sources.

## Local commands

```sh
make deps
make test
make validate-contracts
make up
make migrate
```

PostgreSQL adapter smoke tests are opt-in and roll their fixtures back:

```sh
DIALOG_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5444/ms_dialog?sslmode=disable' go test ./internal/adapters/postgres
```

The external gateway namespace is `/api/dialog/v1/*`; the service owns `/api/v1/*`. `X-User-ID` and `X-User-Role` are trusted only from `ms-gateway`. WebSocket authentication uses a short-lived single-use ticket in `Sec-WebSocket-Protocol`, never a bearer token in the URL.

Runtime modes are `api`, `realtime`, `worker`, and `all`. PostgreSQL is always required; realtime/worker modes require NATS JetStream, attachment operations require FileStorage, and generic-file uploads require ClamAV. See [integration contract](docs/integration-contract.md) and [frontend/backend map](docs/frontend-backend-contract-map.md).

Operational endpoints are `GET /healthz` (process liveness), `GET /readyz` (mode-aware PostgreSQL/NATS readiness), and `GET /metrics` (Prometheus text format). They belong on the private service port and must not be routed through the public dialog namespace. See [operations](docs/operations.md).
