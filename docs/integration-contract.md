# Integration contract

## Gateway

The public namespace is `/api/dialog/v1/*`; the gateway strips `/api/dialog` and forwards `/api/v1/*` to this service. It must overwrite (not append or preserve client values) `X-User-ID`, `X-User-Role`, and `X-Request-ID`, reject guest access, forward WebSocket Upgrade headers without buffering, and keep the service port private. The service preserves a valid gateway UUID request ID for end-to-end error correlation and replaces a missing or malformed value. Direct browser access to the service is not a supported trust boundary.

The gateway must not forward `/healthz`, `/readyz`, or `/metrics`. Orchestrators and Prometheus access those endpoints directly on the private service network.

The WebSocket flow is: authenticated REST request to `POST /api/v1/realtime/ticket`, then `GET /api/v1/ws` with subprotocols `dialog.v1` and `ticket.<opaque>`. The browser never sends a bearer token or ticket in a URL.

## User service

Dialog stores user UUIDs only. Profile names and avatars are hydrated from `ms-go-user` by the consuming application; they are not copied into dialog tables or events. The gateway authenticates the acting user.

Before creating a personal dialog, creating a group, or adding a group member, Dialog resolves the relevant participant UUIDs with one bounded request to `POST /internal/v1/users/active/resolve`. The request uses `X-Internal-Token` populated from the outbound `USER_SERVICE_INTERNAL_TOKEN` (not Dialog's inbound token), contains `user_ids`, and is limited to 1000 unique non-zero UUIDs. `ms-go-user` returns the same UUID set partitioned into `data.active_user_ids` and `data.unavailable_user_ids`; active means the user is enabled and has status `ACTIVE` or `NEW_USER`.

Dialog validates the response as an exact partition. Any unavailable participant produces `422 participant_unavailable`; transport failure, non-200 response, timeout, oversized or malformed payload, or inconsistent partition produces `503 dependency_unavailable`. These mutations fail closed before their database transaction. The lookup remains a feature dependency and is not part of global readiness. The actor is included in group-creation validation; personal-dialog creation and member addition validate the target participant. Manager authorization precedes member lookup so the endpoint does not expose whether an arbitrary UUID exists.

## FileStorage and scanner

Dialog authorizes and binds attachment metadata. `ms-go-filestorage` owns bytes and temporary/active object lifecycle. Images are inspected locally; generic files fail closed unless ClamAV is reachable. Signed download URLs are requested only after current dialog membership and ready status are verified.

## NATS

JetStream `DIALOG_EVENTS` carries durable `dialog.*` subjects from the PostgreSQL outbox. Consumers deduplicate by `event_id`. Core NATS `dialog.realtime.typing.<dialog_id>` is ephemeral and may be lost. Realtime instances use independent subscriptions so every instance can serve its local WebSocket connections.

`dialog.teacher.requested` is a dedicated at-least-once integration trigger for `ms-go-teacher-agent`. It is emitted only after a student-authored teacher-dialog message commits and contains the source IDs, student, bound PersonalTeacher, educational context, and optional LearningAction reference. It deliberately excludes the message body, mastery, and history. Teacher deduplicates by `event_id`, then uses the bounded internal read contract.

The trigger also contains `channel` (`web` or `telegram`). Teacher persists this
bounded routing fact with its durable job. It does not create channel-specific
pedagogy or history.

For `lesson`, that bounded read includes the source message's complete immutable
`lesson-message-context.v1` anchor (or a historical legacy revision/selection
anchor). The ordinary `dialog.message.*` outbox/REST/WebSocket projection may
carry that anchor under the same message membership ACL. The dedicated
`dialog.teacher.requested` trigger remains body- and anchor-free. Teacher uses
its source IDs to fetch the bounded request, then verifies Course/lesson,
revision, and any exact substring; it must not treat Dialog as canonical Course
content.

Rollout is Dialog migration/application, then the v1 lesson-message producer,
then any Teacher release that requires v1 on new source messages. During the
transition Dialog still reads, replays, and accepts legacy
`{content_revision,selected_text}`. Teacher must tolerate legacy history; no
service synthesizes missing Course/lesson IDs.

## Teacher Agent

`ms-go-teacher-agent` owns logical PersonalTeacher identity and pedagogy. It provisions teacher bindings through `PUT /internal/v1/teacher-dialog/ensure`, reads the exact source plus a bounded prior window through `GET /internal/v1/teacher-dialog/{dialogID}/request/{sourceMessageID}`, and appends the eventual response through `POST /internal/v1/teacher-dialog/{dialogID}/message`. A deterministic proactive intervention may use `POST /internal/v1/teacher-dialog/{dialogID}/proactive-message`, but only for the same student's general teacher dialog; it has no synthetic student source and is idempotent by the analytics event ID. All calls require the exact `X-Internal-Token`; the gateway and browser must never route these endpoints.

After its own one-time account linking, Teacher's Telegram adapter may append a
private text update through
`POST /internal/v1/teacher-dialog/{dialogID}/student-channel-message`. Dialog
requires the exact bound student and PersonalTeacher, `telegram` channel, an
idempotency UUID derived from the provider update, and an active general
teacher context. The command commits an ordinary student message and ordinary
teacher request; Dialog remains the only history owner.

Dialog checks binding, context, source authorship, content policy, and append idempotency. Teacher verifies the opaque `learning_action_id` against `ms-go-student`, applies deterministic pedagogy before any model invocation, and does not persist a competing conversation history.

When an active student message is edited or deleted in an active teacher
dialog, the same database transaction stores the ordinary lifecycle event and
`dialog.teacher.context-mutated` schema v1. The Teacher-only payload contains
only dialog/student/PersonalTeacher/context binding, message ID, immutable
message sequence, resulting message version/event sequence, and `updated` or
`deleted`. It contains no body, links, attachments, structured UI, lesson
anchor, LearningAction, mastery, or code. Teacher deduplicates by `event_id` and
invalidates any provisional summary whose covered range includes that message
sequence.

Teacher may resolve a stored structured response through the exact-token-only
`POST /internal/v1/teacher-dialog/{dialogID}/assistant-ui-source` contract. The
request is exact-bound to student, PersonalTeacher, and message; the response is
the body-free `dialog-assistant-ui-source.v1` allowlist. Dialog keeps blocks
opaque and does not become the action-resolution owner.

Teacher response and proactive append may include a bounded
`assistant-ui.v1` envelope alongside mandatory body text. Dialog validates and
canonicalizes only the envelope version/shape/count/byte caps, stores its block
objects opaquely, and compares the complete body plus envelope for idempotent
replay. Dialog defines no structured action protocol in this contract.
