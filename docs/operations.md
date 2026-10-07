# Operations

## Runtime topology

The same binary supports four explicit modes:

| Mode | REST API | WebSocket | Workers | Readiness dependencies |
| --- | --- | --- | --- | --- |
| `api` | yes | no | no | PostgreSQL |
| `realtime` | no | yes | no | PostgreSQL, NATS |
| `worker` | no | no | yes | PostgreSQL, NATS |
| `all` | yes | yes | yes | PostgreSQL, NATS |

For production, `api`, `realtime`, and `worker` can be deployed and scaled independently. All modes expose the private operational HTTP surface. FileStorage, ClamAV, and `ms-go-user` participant resolution are feature dependencies: an outage blocks the operations that need them but does not make unrelated message/dialog operations globally unready. Participant mutations use `USER_SERVICE_BASE_URL`, the outbound `USER_SERVICE_INTERNAL_TOKEN`, and `USER_SERVICE_TIMEOUT_SECONDS` (default `3`, maximum `30`) and fail with retryable `503 dependency_unavailable` when a trustworthy user lookup is unavailable. Dialog's inbound `INTERNAL_API_TOKEN` is a separate boundary and is not reused for User calls.

Worker and `all` modes require the existing infrastructure-provisioned
`DIALOG_EVENTS` stream. Provision the shared NATS manifests through
`learning-platform-infrastructure` before starting Dialog. A missing or
incompatible stream fails startup with an infrastructure validation error;
restarting Dialog never creates or repairs shared streams. See
[Realtime delivery](realtime-delivery.md) for publisher compatibility checks.

The development Compose stack exposes the `all` mode on `http://localhost:8095` by default. `DIALOG_PORT` changes only that host binding; Gateway and other containers reach the stable internal address `ms-dialog-service:8080` on `ms-net`.

`DIALOG_TEACHER_ORDERING_V2_ENABLED` defaults to `false` and is the only
Dialog-side switch for dense teacher-turn allocation and V2 Teacher-request
emission. Do not enable it on only part of the API fleet. Enable it only after
the migration is applied, Teacher consumes V2, and the cross-service legacy
cutover barrier is complete. Disabling it after any V2 turns exist requires the
same barrier; the migration down path deliberately refuses to erase that data.

`DIALOG_CANONICAL_STUDENT_TURN_ENABLED` also defaults to `false` and is valid
only when the V2 ordering flag is true. It admits new trusted action receipts
to Dialog's exact-token internal endpoint; it does not add a browser route or
second trigger. Disable it first for an action rollout rollback: new receipts
fail without effect while exact committed receipt replay stays available. Do
not apply migration `011` down after any receipt evidence exists; it fails
closed deliberately.

After Dialog and Gateway are running, the opt-in integration probe creates an isolated space and two temporary authenticated users, then verifies a three-member group, per-member unread cursors, read-all, image/file activation, signed URLs, WebSocket typing, single-use tickets, and reconnect:

```sh
DIALOG_E2E_ISOLATED_FIXTURE=true \
DIALOG_E2E_VERIFICATION_CODE_COMMAND='/absolute/path/to/isolated-auth-code-fixture' \
DIALOG_E2E_ADMIN_TOKEN='<current ADMIN access token>' make runtime-e2e
```

The probe uses the public Gateway on `http://localhost:7070`, the admin Gateway
on `http://localhost:9090`, and an explicitly supplied Auth verification fixture
executable. The executable
receives one synthetic `dialog-e2e-...@example.test` email argument and must
return one four-digit decimal code on stdout, or fail with nonzero status. It
reads only the owned
signup from the coordinator's disposable loopback Auth store using a separate
fixture principal; production runtime credentials do not grant broad reads.
The coordinator must explicitly declare `DIALOG_E2E_ISOLATED_FIXTURE=true` and
provide the absolute executable `DIALOG_E2E_VERIFICATION_CODE_COMMAND` path.
There is no public code-extraction route. Override `DIALOG_E2E_GATEWAY_URL`,
`DIALOG_E2E_ADMIN_GATEWAY_URL`, or `DIALOG_E2E_ORIGIN` when the local topology
differs. It creates integration data intentionally and never prints access tokens.

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

`dialog_teacher_requested_v2_total` is an exposed zero-label counter incremented
only after the V2 source message, sequence, and outbox event commit.
`dialog_canonical_student_turn_materialized_total` is an exposed
zero-label counter incremented after a canonical Student turn, its private
ledger, lifecycle event, and V2 Teacher request commit. The canonical Teacher
response counter remains reserved for a later slice. V2 structured logs use
`dialog_id`, `canonical_student_message_id`, `teacher_turn_sequence`,
`correlation_id`, `causation_id`, and `source_event_id`; IDs link records but
are never metric labels.
No metric label contains user IDs, dialog IDs, message IDs, attachment IDs,
URLs, request IDs, receipt IDs, source-event IDs, job IDs, response IDs, or
terminal-failure proof/recovery IDs. Recommended alerts are sustained readiness
failure, growth in outbox failures/errors, attachment errors, and WebSocket
connection count approaching configured instance capacity.

## Delivery and recovery

The API commits durable mutations and outbox evidence in one PostgreSQL transaction. Worker publication is at least once; consumers deduplicate by `event_id`. Realtime clients compare `event_sequence`, recover gaps with REST, and must reconnect after a slow-consumer disconnect. Typing notifications are intentionally ephemeral.

On shutdown the HTTP server stops accepting work, subscriptions and the hub close, workers observe cancellation, NATS drains, and PostgreSQL closes. `SHUTDOWN_TIMEOUT_SECONDS` must be smaller than the orchestrator termination grace period.
