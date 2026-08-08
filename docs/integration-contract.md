# Integration contract

## Gateway

The public namespace is `/api/dialog/v1/*`; the gateway strips `/api/dialog` and forwards `/api/v1/*` to this service. It must overwrite (not append or preserve client values) `X-User-ID`, `X-User-Role`, and `X-Request-ID`, reject guest access, forward WebSocket Upgrade headers without buffering, and keep the service port private. The service preserves a valid gateway UUID request ID for end-to-end error correlation and replaces a missing or malformed value. Direct browser access to the service is not a supported trust boundary.

The gateway must not forward `/healthz`, `/readyz`, or `/metrics`. Orchestrators and Prometheus access those endpoints directly on the private service network.

The WebSocket flow is: authenticated REST request to `POST /api/v1/realtime/ticket`, then `GET /api/v1/ws` with subprotocols `dialog.v1` and `ticket.<opaque>`. The browser never sends a bearer token or ticket in a URL.

## User service

Dialog stores user UUIDs only. Profile names and avatars are hydrated from `ms-go-user` by the consuming application; they are not copied into dialog tables or events. The gateway authenticates the acting user. Participant UUID existence validation is exposed as the `ParticipantResolver` port; it remains optional until `ms-go-user` provides a bounded batch internal lookup. This avoids N sequential cross-service calls during large-group creation. A production rollout must either bind that port to the batch contract or explicitly accept UUID-only eventual consistency.

## FileStorage and scanner

Dialog authorizes and binds attachment metadata. `ms-go-filestorage` owns bytes and temporary/active object lifecycle. Images are inspected locally; generic files fail closed unless ClamAV is reachable. Signed download URLs are requested only after current dialog membership and ready status are verified.

## NATS

JetStream `DIALOG_EVENTS` carries durable `dialog.*` subjects from the PostgreSQL outbox. Consumers deduplicate by `event_id`. Core NATS `dialog.realtime.typing.<dialog_id>` is ephemeral and may be lost. Realtime instances use independent subscriptions so every instance can serve its local WebSocket connections.
