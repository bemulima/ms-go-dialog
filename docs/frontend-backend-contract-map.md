# Frontend/backend contract map

## Loading and unread algorithm

1. Open a dialog with `GET /api/v1/message/window?dialog_id=<uuid>&before=10&after=20`. The backend anchors the ascending window at the first unread incoming message; when there is no unread message it returns the newest window.
2. Treat returned `read_state` as authoritative. Merely downloading a message never marks it read.
3. Render messages in ascending `message_sequence`: already-read history is above the unread boundary and unread messages are below it.
4. Observe incoming message elements with `IntersectionObserver`. After a message is materially visible (recommended threshold `0.6` for `150–250 ms`), advance a local contiguous high-water mark. Own, deleted, or hidden sequence positions may be crossed but do not contribute to `unread_count`.
5. Debounce/batch the highest contiguous value into `PUT /api/v1/dialog/read/{dialogID}` with `{"through_message_sequence":N}`. Never send one request per row. Apply the returned read state; an optimistic badge decrement is allowed but must be reconciled to the response.
6. When the user scrolls near the newer edge, request `GET /message/list` with `after_cursor`. Continue while a newer cursor exists. Older history uses `before_cursor` and never changes read state by itself.
7. The “down/read all” action calls `PUT /api/v1/dialog/read-all/{dialogID}`. The transaction snapshots the current dialog maximum, changes only the actor's `dialog_member` row, returns `unread_count=0`, and emits `read.updated` for the actor's other sessions.

For a loaded window of 20 rows, rows already above the server read boundary remain read; only newly visible contiguous incoming rows advance the boundary. The client must not infer “20 loaded means 20 read.” This keeps reloads, multiple devices, edits, deletions, and reconnects deterministic.

## Realtime reconciliation

One WebSocket covers one user and one space, not one dialog. `connection.ready.dialogs` maps every active dialog ID to its current `max_event_sequence`. Compare those values with local cursors, reload the dialog snapshot, then request `/message/changes?after_event_sequence=...` for message gaps. Deduplicate every durable frame by `event_id`.

If a new message arrives while the user is at the bottom and it becomes visible, run the same batched read-through algorithm. Otherwise keep it below the viewport and increment the badge from the durable event/snapshot. Membership removal is delivered once and then the local subscription is revoked.

## UI actions to backend contracts

| UI action | REST command/query | Realtime evidence |
| --- | --- | --- |
| Open or create personal chat | `PUT /dialog/personal/ensure` | `dialog.created` |
| Create group | `POST /dialog/group/create` | `dialog.created` |
| Rename/manage group | `PUT /dialog/update`, member routes | `dialog.updated`, `member.*` |
| Initial messages | `GET /message/window` | `connection.ready` high-water marks |
| Older/newer page | `GET /message/list` | none required |
| Send/edit/delete | message command routes | `message.created/updated/deleted` |
| Reply | `reply_to_message_id` on create | message payload contains reference |
| Upload image/file | attachment upload, then ID on message create | `attachment.ready/failed` |
| Render attachment | attachment signed-url route | ready status |
| Scroll-read | `PUT /dialog/read/{dialogID}` | `read.updated` |
| Read all/down button | `PUT /dialog/read-all/{dialogID}` | `read.updated` |
| Typing | WebSocket `typing.start/stop` with `dialog_id` | ephemeral `typing.started/stopped` |
