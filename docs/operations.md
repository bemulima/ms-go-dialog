# Operations

## Runtime topology

The same binary supports four explicit modes:

| Mode | REST API | WebSocket | Workers | Readiness dependencies |
| --- | --- | --- | --- | --- |
| `api` | yes | no | no | PostgreSQL |
| `realtime` | no | yes | no | PostgreSQL, NATS |
| `worker` | no | no | yes | PostgreSQL, NATS |
| `all` | yes | yes | yes | PostgreSQL, NATS |

For production, `api`, `realtime`, and `worker` can be deployed and scaled independently. All modes expose the private operational HTTP surface. FileStorage, ClamAV, and `ms-go-user` participant resolution are feature dependencies: an outage blocks the operations that need them but does not make unrelated message/dialog operations globally unready. Participant mutations use `USER_SERVICE_BASE_URL`, `INTERNAL_API_TOKEN`, and `USER_SERVICE_TIMEOUT_SECONDS` (default `3`, maximum `30`) and fail with retryable `503 dependency_unavailable` when a trustworthy user lookup is unavailable.

The development Compose stack exposes the `all` mode on `http://localhost:8095` by default. `DIALOG_PORT` changes only that host binding; Gateway and other containers reach the stable internal address `ms-dialog-service:8080` on `ms-net`.

After Dialog and Gateway are running, the opt-in integration probe creates an isolated space and two temporary authenticated users, then verifies a three-member group, per-member unread cursors, read-all, image/file activation, signed URLs, WebSocket typing, single-use tickets, and reconnect:

```sh
DIALOG_E2E_ADMIN_TOKEN='<current ADMIN access token>' make runtime-e2e
```

The probe uses the public Gateway on `http://localhost:7070`, the admin Gateway on `http://localhost:9090`, and the Tarantool integration hook through Gateway. Override `DIALOG_E2E_GATEWAY_URL`, `DIALOG_E2E_ADMIN_GATEWAY_URL`, or `DIALOG_E2E_ORIGIN` when the local topology differs. It creates integration data intentionally and never prints access tokens.

Attachment worker replicas coordinate through leased `SKIP LOCKED` claims. Keep `ATTACHMENT_WORKER_LEASE_SECONDS` (default `120`) longer than a normal FileStorage activation/deletion request; a crashed worker's items become eligible after that deadline.

## Probes

- `GET /healthz`: liveness. It does not call dependencies and should drive process restart decisions.
- `GET /readyz`: traffic readiness. Each dependency check shares `READINESS_TIMEOUT_SECONDS` (default `2`, maximum `30`). A failure returns `503` and component state only.
- `GET /metrics`: Prometheus text exposition. Keep this route on the private service network.

Use a short initial delay only for process startup; let readiness control routing. Do not use liveness to detect a transient PostgreSQL or NATS outage, because restarting every replica amplifies the outage.

## Metrics

HTTP metrics use only method, Chi route template, and status labels. WebSocket connections are a gauge. Worker counters are:

- `dialog_attachment_worker_activated_total`, `dialog_attachment_worker_failed_total`, `dialog_attachment_worker_deleted_total`, `dialog_attachment_worker_errors_total`;
- `dialog_outbox_published_total`, `dialog_outbox_failed_total`, `dialog_outbox_worker_errors_total`;
- `dialog_ticket_cleanup_deleted_total`, `dialog_ticket_cleanup_errors_total`.

No metric label contains user IDs, dialog IDs, message IDs, attachment IDs, URLs, or request IDs. Recommended alerts are sustained readiness failure, growth in outbox failures/errors, attachment errors, and WebSocket connection count approaching configured instance capacity.

## Delivery and recovery

The API commits durable mutations and outbox evidence in one PostgreSQL transaction. Worker publication is at least once; consumers deduplicate by `event_id`. Realtime clients compare `event_sequence`, recover gaps with REST, and must reconnect after a slow-consumer disconnect. Typing notifications are intentionally ephemeral.

On shutdown the HTTP server stops accepting work, subscriptions and the hub close, workers observe cancellation, NATS drains, and PostgreSQL closes. `SHUTDOWN_TIMEOUT_SECONDS` must be smaller than the orchestrator termination grace period.
