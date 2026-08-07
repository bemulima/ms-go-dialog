# Dialog business rules

## Identity and access

- Every user API requires a non-guest UUID identity supplied by the trusted gateway.
- Request bodies never select the acting user or sender.
- A user may read or mutate a dialog only while its membership is active.
- Personal dialogs contain exactly two immutable participant identities and are unique per unordered pair inside one space.
- Group owners and admins manage membership. A group must retain an active owner.
- Blocking prevents new personal-dialog creation and new messages between the blocked pair.

## Messages

- A message belongs to exactly one dialog and has immutable `message_sequence`.
- An optional reply target must belong to the same dialog. Replies do not form a storage tree.
- Content requires non-blank text or at least one attachment.
- Raw HTML is rejected. Only absolute HTTP(S) links are accepted when links are enabled.
- Create is idempotent per sender UUID key. Update/delete use expected integer version.
- Delete keeps a tombstone, identity, order, reply references, and sequence.

## Read state

- Every member owns `last_read_message_sequence`, `unread_count`, and `last_read_at`.
- Reading by one member never changes another member's row.
- A read cursor is monotonic and cannot exceed the dialog's current message sequence.
- New messages increment unread count for active recipients only, never for the sender.
- `read-all` takes the current maximum message sequence inside its transaction and sets only the actor's unread count to zero.
- A new group member defaults to history beginning after the current last message. That boundary is enforced by window/list/changes/get/reply and attachment download authorization, not only stored as metadata.

## Delivery

- Durable mutations allocate event sequence and insert outbox evidence in the same transaction.
- FileStorage and NATS calls never run inside the domain transaction.
- REST is the command source of truth. WebSocket is a bounded low-latency projection.
- Clients deduplicate by event ID and reconcile event-sequence gaps through REST.
