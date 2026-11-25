# Educational Dialog and abuse intake

MENT-27 uses the existing Dialog owner. An Assignment reuses its ordinary
personal pair; Offer negotiation remains ordinary personal history. A Cohort
uses one existing Group with a typed Mentor binding. The group persists its
Mentor owner only; Student membership is a request-local projection of the
current Mentor enrollment, so historical leavers cannot exhaust a shadow
Dialog roster. No Teacher binding is created or reused.

## Current context and messages

`POST /api/v1/dialogs/mentor-context`, no query, exact JSON `{kind,id}` (1024
bytes), accepts `ASSIGNMENT` or `COHORT` and a canonical source UUID. It returns
`{schema:"mentor-dialog.context.v1",dialog_id,mentor_binding_id,kind,source_id}`.
The caller identity is the existing trusted Gateway actor. The response is a
reference, never a grant. Creating a binding does not send a message.

Existing message, dialog get, read and attachment HTTP routes accept one
canonical UUID `X-Mentor-Dialog-Context`. Without it, educational messages and
protected groups are excluded. With it, the binding must match the requested
Dialog, and the Mentor current-access endpoint checks the actor and requested
message authors before and after reads or mutations. Assignment consent,
Mentor suspension, enrollment leave/rejoin, group cancellation, actual content
membership and pair blocks remain authoritative on every operation.

Messages persist a nullable binding, observed owner revision and author
membership UUID. The current enrollment's joined revision is the history floor;
current author membership must still equal the stored author pin. Rejoin does
not expose earlier enrollment history. Assignment consent uses its consent
revision and current command ID. Revocation hides educational payloads without
deleting personal history, binding rows, messages or receipts. Read cursors are
separate for each binding, actor and membership.

The narrow owner contract is documented in Mentor's `docs/dialog-access-v1.md`.
Dialog's HTTPS client uses `POST /internal/dialog/v1/context-access` and the
distinct `X-Mentor-Dialog-Token`; it follows no redirects and performs no retry
or fallback. Missing/changed authority fails closed. Owner observations cannot
create a cross-service transaction; a committed operation can still return a
denial after its final read. Preserve the original idempotency key after an
unknown acknowledgement.

## Projections, paging and delivery

Ordinary list, last-message, count, unread, activity ordering and ready frames
derive from unbound messages only. Protected Cohorts never enter generic lists
or management commands. Educational dialog metadata exposes only self and the
Mentor; aggregate message/unread counts and last-message previews are omitted
as neutral values instead of deriving them from a historical roster.

Educational HTTP window/list/changes are bounded to 30 rows (31 with a page
probe). Their next cursor advances across scanned rows even when current
author checks remove the entire visible page. Context cursors contain the
already-known binding ID, not a hidden message UUID. Current access and history
floors are rechecked on each page; a cursor is not permission. Reply references
whose parent is outside the bounded current-author proof are omitted. Read
through requires an actually visible message; read-all covers the latest
authorized window. No full-history unread total is claimed.

Generic WebSocket fanout performs a durable binding lookup and suppresses all
educational payloads, including queued attachment or moderation events.
Educational consumers poll current-owner HTTP projections and replace cached
windows after revoke/leave. Pending files are typed to the same binding, and
signed URL issuance checks current access again after FileStorage responds.
Previously issued URL bytes retain FileStorage's existing short expiry; this
change does not revoke a downloaded copy or an already issued URL.

## Mentor MCP reads

Mentor's HTTPS client uses `POST /internal/v1/mentor-mcp/dialogs` and
`POST /internal/v1/mentor-mcp/dialog-messages` with one
`X-Mentor-Dialog-MCP-Read-Token`. `DIALOG_MENTOR_MCP_READ_TOKEN` enables both
routes when educational access is enabled. It is distinct from Dialog's other
purpose tokens. Mentor supplies `mentor_id` from the verified MCP delegation
and rechecks that Mentor is active after Dialog responds. Dialog uses the
existing current-access callback for consent, blocks, membership, and history
floors. Requests do not accept browser credentials or actor headers.

Both routes require exact JSON within 1024 bytes, no query or content encoding,
and a `limit` of 1–20. Responses are private and use `Cache-Control: no-store`.

- Dialog list accepts `mentor_id`, `cursor`, and `limit`. It returns
  `{schema:"dialog.mentor-mcp-dialogs.v1",mentor_id,items,next_cursor?}`.
  Items contain `dialog_id`, `binding_id`, `kind`, and `source_id` for
  existing Assignment or Cohort bindings owned by the Mentor. Pass an empty
  cursor initially. A returned cursor is opaque, authenticated, and bound to
  the Mentor. Follow it even when all scanned items were inaccessible.
- Message history accepts `mentor_id`, `dialog_id`, `binding_id`,
  `before_sequence`, and `limit`. It returns
  `{schema:"dialog.mentor-mcp-messages.v1",mentor_id,dialog_id,binding_id,items,next_before_sequence?}`.
  Start with an empty string for the newest page. Follow the returned decimal
  string to read older pages. Each item contains `message_id`, `sender_id`,
  `body`, `body_truncated`, and `sent_at`. The body is plain text capped at
  800 Unicode code points. Current access and message status govern visibility
  throughout paging.

These reads use existing bindings and history and perform no provisioning,
membership updates, or read-state mutations.

## Abuse reports and moderation

`POST /api/v1/message-report` accepts exact `{request_id,message_id,category,details}`,
where category is `SPAM`, `HARASSMENT` or `OTHER`; details are nonblank, at most
1000 Unicode code points. The caller must currently read the exact message for
a new report. `Idempotency-Key` must occur exactly once and equal the canonical
request UUID. Maximum body is 4096 bytes, no query or encoding.

The immutable receipt is `{schema:"dialog.report-receipt.v1",report_id,request_id,message_id,revision:1,status:"OPEN"}`.
An exact own-command replay can recover that receipt after access changes. It
does not disclose message content or grant access. Different bytes or binding
with the same key conflict.

ADMIN/MODERATOR can read `GET /admin/v1/message-report/list?before_id=<UUID>&limit=1..50`.
The exact envelope is `{schema:"dialog.report-list.v1",items,next_before_id}`.
Each item contains `id,request_id,reporter_id,message_id,dialog_id,message_version,category,details,status,revision,created_at,resolution`.
It contains reporter-provided details, not message body or attachments. Queue
pagination includes resolved history and orders by creation time and ID.

`POST /admin/v1/message-report/{id}/resolve` accepts exact
`{request_id,expected_revision:1,action,reason}`, the same required key header and
4096 byte limit. Action is `HIDE` or `DISMISS`; reason is nonblank and at most
500 Unicode code points. HIDE requires the current message version to equal
the reported version. The message transition and immutable resolution commit
in one Dialog transaction. A changed message returns 409; DISMISS remains
available. Exact retries return the existing resolution, other commands
conflict. Response is `{schema:"dialog.report-resolution.v1",report}`; the
resolved item has revision 2 and resolution `{request_id,actor_id,action,reason,resolved_at}`.

All new replies are private/no-store. Existing Gateway verification and
moderator role gates apply; browser actor headers are not authority. No email
or external moderation message is emitted.

## Configuration and source qualification

`DIALOG_MENTOR_EDUCATION_ENABLED=false` and `DIALOG_ABUSE_REPORT_ENABLED=false`
are defaults. Education additionally requires an active
`DIALOG_MENTOR_EDUCATION_SPACE_KEY`, HTTPS `DIALOG_MENTOR_ACCESS_ORIGIN`, distinct
`DIALOG_MENTOR_ACCESS_TOKEN`, and optional trusted CA file
`DIALOG_MENTOR_ACCESS_CA_FILE`. Mentor must separately enable its owner endpoint.

Additive migrations 013/014 are source only. They must precede deployment of
the new projections even while feature flags are off. Down scripts refuse to
discard existing conversation or report history. No migration or SQL was
executed. Targeted usecase, HTTP recorder, bounded owner client, WebSocket and
SQL-query-shape tests exercise source logic without live services. Gherkin,
CI, full-suite and runtime qualification remain unrun.
