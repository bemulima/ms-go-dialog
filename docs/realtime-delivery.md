# Realtime delivery

Every durable mutation stores a versioned outbox event in the same PostgreSQL transaction. A worker claims eligible rows with `FOR UPDATE SKIP LOCKED`, commits a finite lease before the network call, publishes to NATS JetStream with `event_id` deduplication, and records success or bounded retry evidence.

Lifecycle subjects use the `dialog.*` namespace. Realtime instances subscribe without a shared queue group so every instance can deliver to its own local connections. Ephemeral typing uses Core NATS subject `dialog.realtime.typing.<dialog_uuid>` and is never stored in the outbox.

At-least-once delivery requires event-ID deduplication. Per-dialog event sequence detects gaps; REST snapshots and `/message/changes` recover state.
