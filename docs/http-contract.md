# HTTP contract

Route groups follow the platform convention:

- `/healthz`, `/readyz`, `/metrics`: private operational endpoints without actor authentication;
- `/api/v1`: authenticated user operations;
- `/admin/v1`: explicit role-protected administration and moderation;
- `/internal/v1`: exact shared-token service calls when introduced.

Bodies reject unknown fields and acting-user fields. Commands documented without a request body accept an absent/whitespace body or one empty `{}` object up to 1 KiB; additional JSON values, fields, and oversized bodies are rejected. Pagination cursors are opaque, base64url encoded, and bound to their dialog/direction.

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

### Teacher dialogs and message authors

Internally provisioned teacher dialogs appear in the ordinary list/get/history APIs because the real student is their only member. Their response includes `student_id`, `personal_teacher_id`, `context_type`, and optional `context_id`.

Message responses include `author_type` and `channel`. Existing user messages keep `sender_id`; PersonalTeacher messages return `sender_id: null` and `personal_teacher_id`. Browser-created messages use `web`; a teacher response inherits its source channel. `POST /message/create` accepts optional `learning_action_id`: it is required for student messages in `lesson_task`, `practice_task`, and `project` teacher dialogs and rejected everywhere else.

For a `lesson` teacher dialog, `POST /message/create` instead requires:

```json
{
  "lesson_context": {
    "content_revision": "2026-08-31T12:00:00.123Z",
    "selected_text": "the exact text selected in the lesson"
  }
}
```

The revision must be RFC3339Nano and the selected text is preserved byte for
byte, non-blank, and limited to 12,000 Unicode code points. Message responses
return the same `lesson_context`. Every non-lesson context rejects this object;
Dialog does not claim the revision or selection is canonical.

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

## Internal API

The internal contour requires an exact `X-Internal-Token` and is never exposed through the gateway.

```http
PUT  /internal/v1/teacher-dialog/ensure
GET  /internal/v1/teacher-dialog/{dialogID}/request/{sourceMessageID}?personal_teacher_id=<uuid>&before=20
POST /internal/v1/teacher-dialog/{dialogID}/message
POST /internal/v1/teacher-dialog/{dialogID}/proactive-message
POST /internal/v1/teacher-dialog/{dialogID}/student-channel-message
```

Ensure accepts `space_key`, `student_id`, `personal_teacher_id`, `context_type`, and optional `context_id`; it is idempotent for that binding.

Proactive append accepts `personal_teacher_id`, UUID `idempotency_key`, and
`body`. It is valid only for the bound active `general_teacher` dialog and
returns `201` for the first commit or `200` for an identical replay. The
created message has no user source, reply, LearningAction, or lesson context.

The request query returns the exact active student source message and no more than 49 preceding visible messages. It verifies the requested PersonalTeacher binding and never returns unrestricted history. For `lesson`, the source includes its revision/selection anchor so Teacher can verify it against Course; the integration event remains body- and context-free.

Student-channel append accepts the bound `student_id`, `personal_teacher_id`, a
UUID `idempotency_key`, literal `telegram` channel, and body. It is restricted
to the active `general_teacher` dialog and returns the ordinary message
contract. It accepts no sender override, attachments, reply, LearningAction, or
lesson context.

Append accepts `personal_teacher_id`, `source_message_id`, `idempotency_key`, and `body`. It accepts no sender, targets, mastery, attachments, or LearningAction override. The response is authored by the bound PersonalTeacher, replies to the source, copies its LearningAction reference, and is idempotent per PersonalTeacher and key.
