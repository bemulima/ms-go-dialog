# Attachment lifecycle

Dialog owns authorization and binding metadata; `ms-go-filestorage` owns bytes.

1. Active member uploads to an existing dialog.
2. Dialog applies effective policy, validates size/signature/MIME, and uploads a temporary object using the attachment UUID as owner.
3. Pending attachment metadata is stored for uploader and dialog.
4. Message creation binds only same-dialog pending attachments owned by the sender.
5. A worker performs any required scan, activates FileStorage idempotently, and transitions to ready or failed with event evidence.
6. Signed download URLs require current active membership and a ready attachment bound to a visible message.
7. Deletion is local-first and physical FileStorage removal is retried asynchronously.

Images support JPEG, PNG, and WebP. Generic files require an explicit MIME allowlist and scan result; executable, HTML, SVG, and macro-bearing content are disabled by default.
