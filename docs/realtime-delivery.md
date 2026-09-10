# Realtime delivery

Every durable mutation stores a versioned outbox event in the same PostgreSQL transaction. A worker claims eligible rows with `FOR UPDATE SKIP LOCKED`, commits a finite lease before the network call, publishes to NATS JetStream with `event_id` deduplication, and records success or bounded retry evidence.

Lifecycle subjects use the `dialog.*` namespace. Realtime instances subscribe without a shared queue group so every instance can deliver to its own local connections. Ephemeral typing uses Core NATS subject `dialog.realtime.typing.<dialog_uuid>` and is never stored in the outbox.

`dialog.teacher.requested` is durable in the same stream but is not a WebSocket lifecycle event. It is consumed by Teacher only. The normal `dialog.message.created` event provides browser realtime for both the source student message and the later teacher response.

`dialog.teacher.context-mutated` schema v1 is also durable and Teacher-only,
never a WebSocket lifecycle projection. A successful update/delete of an active
student-authored message in an active teacher dialog writes it beside the
ordinary lifecycle event in one transaction and with the same event sequence.
Its strict payload is body-free: common envelope, exact teacher binding,
message ID/immutable sequence, resulting message version/event sequence, and
mutation kind only. A retry of an already committed version-fenced mutation
cannot create a second event; delivery remains at-least-once and consumers
deduplicate by `event_id`.

`dialog.message.created` remains schema version `1`. For a structured
PersonalTeacher message it additively includes optional `assistant_ui` with its
own `assistant-ui.v1` envelope. An ordinary lifecycle event for an anchored
student lesson message additively includes optional `lesson_context`; updates
retain it and deletion omits it after clearing the anchor. Existing body-only
v1 payloads omit both fields. These ordinary events and their WebSocket frames
follow normal dialog membership/history access. Mandatory `body` remains the
fallback for structured Teacher UI, and the projection does not interpret
either extension.

Both events carry the bounded message channel. Channel is routing/provenance
metadata only; the normal Dialog stream remains canonical for every transport.

One student teacher-message mutation produces `dialog.message.created` and `dialog.teacher.requested` with distinct event IDs and subjects but the same dialog event sequence. This is one state transition, not a reconnect gap. The ordinary message event may contain body and lesson anchor; the Teacher-only trigger contains neither. Teacher deduplicates its trigger by event ID and fetches the bounded source; WebSocket clients never receive the integration trigger.

`DIALOG_TEACHER_ORDERING_V2_ENABLED=false` is the sole Dialog producer switch.
When false, `dialog.teacher.requested` stays byte-compatible schema V1 and no
teacher turn is allocated. When true, each newly committed qualifying
Student-authored source gets the next dense private per-dialog
`teacher_turn_sequence` under the existing locked Dialog row, and Dialog emits
exactly one schema V2 trigger; it never dual-emits V1 and V2. Idempotency replay
returns the prior message without allocation or trigger; legacy rows and
PersonalTeacher replies/proactive messages remain unsequenced. Every producer
replica must cross this flag only after Teacher supports V2 and after a
per-dialog legacy drain/cutover barrier; a mixed producer fleet is forbidden.

V2 remains body-free and adds `canonical_student_message_id`,
`teacher_turn_sequence`, `correlation_id`, `causation_id`, optional
`source_message_version`, and optional nullable `reply_to_message_id`; every
new Dialog V2 emission carries a positive source version and an explicit reply
UUID or `null`, while queued pre-extension V2 records may omit either field.
Trusted canonical action materialization additionally sets optional
`action_receipt_id` equal to correlation. For existing
direct Student text, source and canonical IDs are the stored message ID, and
both correlation and causation equal that message ID. The S2 exact-token
command is separately default-disabled and requires the V2 flag; it retains the
canonical message as causation and uses the action receipt as correlation. The
complete action causal ledger is source
prompt -> receipt -> canonical Student message -> source event -> Teacher job
-> PersonalTeacher response: Teacher records the trigger's existing UUID
`event_id` as `source_event_id`, then creates a Teacher job caused by that
event and records its UUID `teacher_job_id`; response append is caused by that
job and records UUID `teacher_response_message_id`. Terminal failure evidence
reserves UUID `no_effect_proof_id`, UUID `recoverable_outcome_id`, and bounded
`failure_code` in structured logs/durable owner records only, never as metric
labels. V2 does not contain private interaction metadata, browser action data,
block/action IDs, source UI digest, `student_command_id`, assistant UI, message
body, lesson anchor, mastery, or history. The lifecycle
event and WebSocket remain ordinary message evidence, never a causal trigger or
ordering authority.

At-least-once delivery requires event-ID deduplication. Per-dialog event sequence detects gaps; REST snapshots and `/message/changes` recover state.
