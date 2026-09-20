# Agent contract map

This map is the final navigation source for the current backend release. Until implementation is complete, machine contracts must distinguish `planned` from `implemented` behavior.

| Concern | Owner | Source |
| --- | --- | --- |
| Dialogs, membership, messages, read state | `ms-go-dialog` | domain/use cases and PostgreSQL |
| Authentication | `ms-go-auth` + `ms-gateway` | gateway-replaced actor headers |
| User profile and participant activity | `ms-go-user` | UUID reference locally; bounded internal batch lookup before membership mutations |
| Attachment bytes | `ms-go-filestorage` | local binding and authorization metadata |
| Durable delivery | PostgreSQL outbox + NATS JetStream | at-least-once, event-ID deduplication |
| Browser realtime | Dialog WebSocket | ticket-authenticated projection |
| Malware decision | ClamAV | fail-closed synchronous scan for generic files |
| Liveness, readiness, metrics | `ms-go-dialog` private HTTP port | operations contract and observability infrastructure |
| Logical PersonalTeacher identity and pedagogy | `ms-go-teacher-agent` | opaque binding/reference locally; no synthetic user |
| Teacher conversation history and delivery | `ms-go-dialog` | teacher dialog, explicit message author, outbox and WebSocket |
| LearningAction ownership/state | `ms-go-student` | opaque source-message reference; verified by Teacher |

Integration boundaries are specified in `docs/integration-contract.md`. The future frontend must follow `docs/frontend-backend-contract-map.md`; in particular, loading is not reading and only an explicit contiguous read-through or read-all command mutates one member's counters.

Change routing:

- invariant: `internal/domain`, migrations, repository port, tests, business rules;
- process: `internal/usecase`, transaction boundary, handler, outbox payload;
- REST: HTTP adapter, `docs/http-contract.md`, `.ai/contracts/http.yaml`;
- WebSocket: websocket adapter, ticket use case, WS contract and recovery tests;
- persistence: reversible migrations, postgres adapter, database contract;
- event: domain subject, outbox, events contract and consumer rules;
- attachment: attachment use case, FileStorage adapter, lifecycle contract.
- participant validation: dialog use-case port, user HTTP adapter, `ms-go-user` internal batch contract, stable `422`/`503` errors.
- frontend/read UX: frontend/backend map, message window query, per-member read transaction, `read.updated` event.
- operations: health/observability infrastructure, runtime composition, operations contract, HTTP machine contract.
- teacher dialog: Dialog domain/migration, internal ensure/read/append contour, body-free durable trigger, Teacher-owned consumer contract.
- teacher transport channel: message provenance, trusted internal student-channel append, and the same body-free teacher trigger; Teacher owns provider linking/delivery.
- structured Teacher UI: bounded opaque Dialog message storage/internal append/REST/outbox; Teacher owns pedagogical validation and no action protocol is defined here.
- lesson message anchor: strict immutable Dialog persistence/API/replay; Course owns content and Teacher owns revision/substring verification.
