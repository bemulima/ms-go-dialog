# Error contract

Errors use a stable envelope:

```json
{
  "error": "dialog_forbidden",
  "message": "dialog membership is required",
  "request_id": "uuid",
  "details": null
}
```

Stable codes include `invalid_request`, `authentication_required`, `space_not_found`, `dialog_not_found`, `dialog_forbidden`, `dialog_closed`, `member_not_found`, `member_limit_exceeded`, `last_owner_required`, `message_not_found`, `message_conflict`, `moderation_conflict`, `idempotency_conflict`, `invalid_read_sequence`, `links_disabled`, `images_disabled`, `files_disabled`, `attachment_invalid`, `attachment_not_found`, `attachment_not_ready`, `file_infected`, `file_scan_unavailable`, `rate_limited`, `dependency_unavailable`, and `internal_error`.
