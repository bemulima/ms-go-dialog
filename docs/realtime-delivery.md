# Realtime delivery

Every durable mutation stores a versioned outbox event in the same PostgreSQL transaction. A worker claims eligible rows with `FOR UPDATE SKIP LOCKED`, commits a finite lease before the network call, publishes to NATS JetStream with `event_id` deduplication, and records success or bounded retry evidence.

Lifecycle subjects use the `dialog.*` namespace. Realtime instances subscribe without a shared queue group so every instance can deliver to its own local connections. Ephemeral typing uses Core NATS subject `dialog.realtime.typing.<dialog_uuid>` and is never stored in the outbox.

`dialog.teacher.requested` is durable in the same stream but is not a WebSocket lifecycle event. It is consumed by Teacher only. The normal `dialog.message.created` event provides browser realtime for both the source student message and the later teacher response.

Both events carry the bounded message channel. Channel is routing/provenance
metadata only; the normal Dialog stream remains canonical for every transport.

One student teacher-message mutation produces `dialog.message.created` and `dialog.teacher.requested` with distinct event IDs and subjects but the same dialog event sequence. This is one state transition, not a reconnect gap. Teacher deduplicates its trigger by event ID; WebSocket clients never receive the integration trigger.

At-least-once delivery requires event-ID deduplication. Per-dialog event sequence detects gaps; REST snapshots and `/message/changes` recover state.
