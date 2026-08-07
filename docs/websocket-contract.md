# WebSocket contract

WebSocket is a realtime projection; durable message commands remain REST operations.

## Handshake

1. An authenticated user mints `POST /api/v1/realtime/ticket` for one active space.
2. The server returns a random opaque ticket with at most 30 seconds lifetime.
3. The browser connects to `/api/v1/ws` with `dialog.v1` and `ticket.<opaque>` subprotocols.
4. The service validates exact Origin, consumes the hashed ticket once, and binds the connection to the user and space.

Tokens and tickets in query strings are forbidden.

## Envelope

```json
{
  "v": 1,
  "type": "message.created",
  "event_id": "uuid",
  "dialog_id": "uuid",
  "event_sequence": 43,
  "occurred_at": "2026-08-08T10:00:00Z",
  "data": {}
}
```

Durable events include dialog/member/message/read/attachment lifecycle types. Ephemeral events are `connection.ready`, `typing.started`, `typing.stopped`, `resync_required`, `error`, and `ping`. Client frames are `typing.start`, `typing.stop`, and `pong`.

The hub indexes connections by user and active dialog memberships. Membership removal revokes the subscription. A bounded queue disconnects slow consumers. Reconnect reloads dialog snapshots, compares each `max_event_sequence`, and requests changes for gaps.
