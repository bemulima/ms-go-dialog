# Agent contract map

This map is the final navigation source for the current backend release. Until implementation is complete, machine contracts must distinguish `planned` from `implemented` behavior.

| Concern | Owner | Source |
| --- | --- | --- |
| Dialogs, membership, messages, read state | `ms-go-dialog` | domain/use cases and PostgreSQL |
| Authentication | `ms-go-auth` + `ms-gateway` | gateway-replaced actor headers |
| User profile | `ms-go-user` | UUID reference only locally |
| Attachment bytes | `ms-go-filestorage` | local binding and authorization metadata |
| Durable delivery | PostgreSQL outbox + NATS JetStream | at-least-once, event-ID deduplication |
| Browser realtime | Dialog WebSocket | ticket-authenticated projection |

Change routing:

- invariant: `internal/domain`, migrations, repository port, tests, business rules;
- process: `internal/usecase`, transaction boundary, handler, outbox payload;
- REST: HTTP adapter, `docs/http-contract.md`, `.ai/contracts/http.yaml`;
- WebSocket: websocket adapter, ticket use case, WS contract and recovery tests;
- persistence: reversible migrations, postgres adapter, database contract;
- event: domain subject, outbox, events contract and consumer rules;
- attachment: attachment use case, FileStorage adapter, lifecycle contract.
