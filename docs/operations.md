# Operations

## Runtime topology

The same binary supports four explicit modes:

| Mode | REST API | WebSocket | Workers | Readiness dependencies |
| --- | --- | --- | --- | --- |
| `api` | yes | no | no | PostgreSQL |
| `realtime` | no | yes | no | PostgreSQL, NATS |
| `worker` | no | no | yes | PostgreSQL, NATS |
| `all` | yes | yes | yes | PostgreSQL, NATS |

For production, `api`, `realtime`, and `worker` can be deployed and scaled independently. All modes expose the private operational HTTP surface. FileStorage and ClamAV are feature dependencies: an outage blocks attachment work but does not make message and dialog operations globally unready.

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
