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
replay. Dialog does not resolve structured action semantics.

S2 implements private `canonical-student-turn-identity.v1` and
`canonical-student-turn-interaction.v1` for its default-disabled exact-token
command. The only interaction kind is `teacher_action_receipt`, which contains an already validated
Teacher-owned `action_receipt_id`, server-validated source prompt ID/version,
bounded opaque `block_id`/`action_id`, and a lowercase SHA-256
`source_ui_digest` of stored canonical `assistant_ui` bytes. It contains no
browser body, action payload, assistant UI,
or Student-state data. The command appends one normal Student message replying
to the exact assistant-UI source in the same transaction as the private ledger,
dense V2 order, lifecycle evidence, and V2 Teacher request. It is never browser
routable, and browsers call only Teacher's action command rather than
dual-writing Teacher plus Dialog.

The canonical causal ledger is `source_prompt_message_id` ->
`action_receipt_id` -> `canonical_student_message_id` -> `source_event_id` ->
`teacher_job_id` -> `teacher_response_message_id`. Every ledger identifier,
including `correlation_id` and `causation_id`, is a non-zero UUID.
`correlation_id` is the receipt identity throughout the action chain. Causation
is boundary-specific rather than one global value: receipt validation is caused
by `source_prompt_message_id`; canonical Dialog materialization by
`action_receipt_id`; the `dialog.teacher.requested` event by
`canonical_student_message_id`; Teacher-job intake by `source_event_id`; and
response append by `teacher_job_id`. Dialog's internal command may retain
`canonical_message_command_id`. `source_event_id` is the existing
`dialog.teacher.requested` `event_id` when Teacher records the job. The
body-free Teacher request v2 adds `canonical_student_message_id`,
`teacher_turn_sequence`, `correlation_id`, `causation_id`, optional
`source_message_version`, and optional nullable `reply_to_message_id` while
retaining existing source/binding fields. Every newly emitted V2 event sets a
positive source-message version and explicitly sets the reply field to its UUID
or `null`; previously queued V2 records may omit either additive field and
remain strict-decodable. Neither field adds message content or trusted UI
metadata. The normal lifecycle/WS projection remains evidence, not a Teacher trigger. S1 adds the disabled-by-default sole producer
flag `DIALOG_TEACHER_ORDERING_V2_ENABLED`: it allocates the dense private turn
after idempotency replay and emits exactly one V2 request for each new
qualifying Student source. When false, the existing V1 producer remains
byte-compatible and no sequence is allocated; V1/V2 are never dual-emitted.
Existing direct text uses its message ID as canonical ID, correlation, and
causation; the S2 action materializer uses receipt correlation while its
request causation remains the canonical message. Migration `010` does not
backfill existing rows. Teacher must deploy V2 handling and establish a legacy
per-dialog cutover barrier before every Dialog producer enables the flag. S1
does not materialize actions, persist trusted interaction metadata, or add a
route. S2 adds the separate default-disabled
`DIALOG_CANONICAL_STUDENT_TURN_ENABLED` command gate (which requires S1 V2):
Dialog persists private receipt evidence and atomically creates the normal
message, reply, dense turn, lifecycle event, and exactly one V2 trigger. For an
action trigger V2 includes optional `action_receipt_id`, equal to correlation;
it also retains the canonical message's positive source version and explicit
reply-to-source-prompt UUID. It excludes body, UI, block/action/digest, and does not invent a
`student_command_id`. Exact committed replay is recoverable while either flag
is off; new receipts are rejected with no effect. The shared structured-log vocabulary also reserves
`no_effect_proof_id`, `recoverable_outcome_id`, and bounded `failure_code` for
a terminal `failed_no_effect` result; IDs must never be used as metric labels.
