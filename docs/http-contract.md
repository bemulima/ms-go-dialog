# HTTP contract

Route groups follow the platform convention:

- `/healthz`, `/readyz`, `/metrics`: private operational endpoints without actor authentication;
- `/api/v1`: authenticated user operations;
- `/admin/v1`: explicit role-protected administration and moderation;
- `/internal/v1`: exact shared-token service calls when introduced.

Bodies reject unknown fields and acting-user fields. Pagination cursors are opaque, base64url encoded, and bound to their dialog/direction.

## Operational API

`GET /healthz` is liveness only and returns `200` while the process can serve HTTP. `GET /readyz` returns `200` only when all dependencies required by the configured runtime mode are usable, otherwise `503`. It reports only `ok`/`failed` component states and never exposes connection strings or dependency error messages. `GET /metrics` returns Prometheus text format with route-template HTTP labels and no user, dialog, message, attachment, or request identifiers.

These routes are served on the private service port. The gateway must not expose them under `/api/dialog/v1`.

## User API

```http
PUT    /api/v1/dialog/personal/ensure
POST   /api/v1/dialog/group/create
GET    /api/v1/dialog/list
GET    /api/v1/dialog/get/{dialogID}
PUT    /api/v1/dialog/update/{dialogID}
PUT    /api/v1/dialog/read/{dialogID}
PUT    /api/v1/dialog/read-all/{dialogID}
POST   /api/v1/dialog/leave/{dialogID}
POST   /api/v1/dialog-member/add/{dialogID}
DELETE /api/v1/dialog-member/remove/{dialogID}/{userID}
PUT    /api/v1/dialog-member/role/{dialogID}/{userID}
PUT    /api/v1/user-block/{userID}
DELETE /api/v1/user-block/{userID}
GET    /api/v1/message/window
GET    /api/v1/message/list
GET    /api/v1/message/get/{messageID}
GET    /api/v1/message/changes
POST   /api/v1/message/create
PUT    /api/v1/message/update/{messageID}
DELETE /api/v1/message/delete/{messageID}
POST   /api/v1/message-attachment/upload
GET    /api/v1/message-attachment/signed-url/{attachmentID}
DELETE /api/v1/message-attachment/delete/{attachmentID}
POST   /api/v1/realtime/ticket
GET    /api/v1/ws
```

### Initial message window

`GET /message/window?dialog_id=<uuid>&before=10&after=20` returns ascending messages around the actor's first unread boundary, current per-member read state, and both dialog high-water marks. With no unread messages it spends the full `before + after` budget on the latest messages. `older_cursor` and `newer_cursor` are non-null only when at least one visible message exists in that direction.

### Read

`PUT /dialog/read/{dialogID}` accepts only `{"through_message_sequence": 123}`. The service updates the authenticated member with `GREATEST(current, requested)` and returns authoritative read state. `PUT /dialog/read-all/{dialogID}` accepts no body and atomically advances only that member to the current maximum message sequence.

## Admin API

```http
POST /admin/v1/space/create
GET  /admin/v1/space/list
PUT  /admin/v1/space/update/{spaceID}
GET  /admin/v1/dialog/list
PUT  /admin/v1/dialog/close/{dialogID}
PUT  /admin/v1/dialog/reopen/{dialogID}
PUT  /admin/v1/message/hide/{messageID}
PUT  /admin/v1/message/restore/{messageID}
```

Administrative access to private message bodies is denied unless a future explicit report-review contract authorizes a bounded target.
