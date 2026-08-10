# Integration contract

## Gateway

The public namespace is `/api/dialog/v1/*`; the gateway strips `/api/dialog` and forwards `/api/v1/*` to this service. It must overwrite (not append or preserve client values) `X-User-ID`, `X-User-Role`, and `X-Request-ID`, reject guest access, forward WebSocket Upgrade headers without buffering, and keep the service port private. The service preserves a valid gateway UUID request ID for end-to-end error correlation and replaces a missing or malformed value. Direct browser access to the service is not a supported trust boundary.

The gateway must not forward `/healthz`, `/readyz`, or `/metrics`. Orchestrators and Prometheus access those endpoints directly on the private service network.

The WebSocket flow is: authenticated REST request to `POST /api/v1/realtime/ticket`, then `GET /api/v1/ws` with subprotocols `dialog.v1` and `ticket.<opaque>`. The browser never sends a bearer token or ticket in a URL.

## User service

Dialog stores user UUIDs only. Profile names and avatars are hydrated from `ms-go-user` by the consuming application; they are not copied into dialog tables or events. The gateway authenticates the acting user.

Before creating a personal dialog, creating a group, or adding a group member, Dialog resolves the relevant participant UUIDs with one bounded request to `POST /internal/v1/users/active/resolve`. The request uses `X-Internal-Token`, contains `user_ids`, and is limited to 1000 unique non-zero UUIDs. `ms-go-user` returns the same UUID set partitioned into `data.active_user_ids` and `data.unavailable_user_ids`; active means the user is enabled and has status `ACTIVE` or `NEW_USER`.

Dialog validates the response as an exact partition. Any unavailable participant produces `422 participant_unavailable`; transport failure, non-200 response, timeout, oversized or malformed payload, or inconsistent partition produces `503 dependency_unavailable`. These mutations fail closed before their database transaction. The lookup remains a feature dependency and is not part of global readiness. The actor is included in group-creation validation; personal-dialog creation and member addition validate the target participant. Manager authorization precedes member lookup so the endpoint does not expose whether an arbitrary UUID exists.

## FileStorage and scanner

Dialog authorizes and binds attachment metadata. `ms-go-filestorage` owns bytes and temporary/active object lifecycle. Images are inspected locally; generic files fail closed unless ClamAV is reachable. Signed download URLs are requested only after current dialog membership and ready status are verified.

## NATS

JetStream `DIALOG_EVENTS` carries durable `dialog.*` subjects from the PostgreSQL outbox. Consumers deduplicate by `event_id`. Core NATS `dialog.realtime.typing.<dialog_id>` is ephemeral and may be lost. Realtime instances use independent subscriptions so every instance can serve its local WebSocket connections.
