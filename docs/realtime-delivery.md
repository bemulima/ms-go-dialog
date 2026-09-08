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

At-least-once delivery requires event-ID deduplication. Per-dialog event sequence detects gaps; REST snapshots and `/message/changes` recover state.
