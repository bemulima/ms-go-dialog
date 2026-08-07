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
- `docs` and `.ai`: human and machine-readable contract sources.

## Local commands

```sh
make deps
make test
make validate-contracts
make up
make migrate
```

The external gateway namespace is `/api/dialog/v1/*`; the service owns `/api/v1/*`. `X-User-ID` and `X-User-Role` are trusted only from `ms-gateway`. WebSocket authentication uses a short-lived single-use ticket in `Sec-WebSocket-Protocol`, never a bearer token in the URL.
