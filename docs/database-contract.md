# Database contract

PostgreSQL is owned exclusively by `ms-go-dialog`. Migrations are ordered reversible `.up.sql`/`.down.sql` pairs and do not use cross-service foreign keys.

## Tables

- `dialog_space`: integration key, Origin allowlist, personal/group and content policies.
- `dialog`: type, lifecycle, personal pair hash, counters, message/event high-water marks, last activity.
- `dialog_member`: role, lifecycle, history boundary, independent read cursor/unread count, mute/archive state.
- `dialog_message`: sender, optional same-dialog reply, content, immutable message order, latest event sequence, version and tombstone.
- `dialog_attachment`: service authorization and lifecycle metadata while FileStorage owns bytes.
- `dialog_outbox`: versioned lifecycle payload, finite claim lease, retries and publication evidence.
- `dialog_ws_ticket`: SHA-256 ticket hash, user/space binding and short expiry.
- `dialog_user_block`: directed user block relationship.

## Transaction boundaries

- Personal dialog and both memberships commit together.
- Group and initial membership set commit together.
- Message, dialog counters/sequences, recipient unread increments, attachment binding, and outbox insert commit together.
- Read cursor, unread decrement, event sequence, and outbox insert commit together for one member only.
- Attachment ready/failed transitions and their message/dialog event evidence commit together after idempotent FileStorage work for active messages. Hidden messages finish the attachment transition without public event evidence; a later moderation restore carries the authoritative attachment snapshot.

Attachment activation and deletion batches are claimed atomically with `FOR UPDATE SKIP LOCKED` by moving the corresponding next-attempt timestamp to a finite lease deadline. This permits multiple worker replicas without concurrent normal processing; an abandoned claim becomes eligible again after the lease.

## Sequence distinction

`dialog.max_message_sequence` increments only for message creation. `dialog.max_event_sequence` increments for every durable change. `dialog_message.message_sequence` never changes; `last_event_sequence` advances on edits, deletes, moderation, or attachment projection changes.
