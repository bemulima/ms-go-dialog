# Dialog domain map

## Boundary

The service owns authenticated messaging. Users are referenced by UUID without cross-database foreign keys. `ms-go-user` owns profile data, `ms-go-auth` and `ms-gateway` own authentication, and `ms-go-filestorage` owns attachment bytes.

## Aggregate map

```text
DialogSpace
└── Dialog (personal or group)
    ├── DialogMember (one independent read state per user)
    ├── DialogMessage (ordered flat stream, optional reply reference)
    │   └── DialogAttachment
    ├── RealtimeTicket
    ├── max_message_sequence
    └── max_event_sequence

Database transaction
└── OutboxEvent -> NATS JetStream -> WebSocket instances
```

`max_message_sequence` changes only on message creation and orders the stream. `max_event_sequence` changes on every durable mutation and drives reconnect reconciliation. Read state is never stored on `Dialog`; it is stored independently on every `DialogMember`.

## Business processes

1. Ensure the unique personal dialog for an unordered pair of users.
2. Create a group and manage owner/admin/member roles.
3. List dialogs with the authenticated member's independent unread state.
4. Load a bidirectional message window anchored at the first unread message.
5. Create a message atomically with idempotency, ordering, recipient unread counters, and outbox evidence.
6. Edit with optimistic locking or delete to a stable tombstone.
7. Advance one member's read cursor monotonically or atomically mark all current messages read.
8. Stage, bind, scan/activate, authorize, and delete attachments.
9. Publish committed lifecycle events and fan them out through user-scoped WebSockets.
10. Recover gaps from REST using event sequence and current snapshots.
