# Database contract

PostgreSQL is owned exclusively by `ms-go-dialog`. Migrations are ordered reversible `.up.sql`/`.down.sql` pairs and do not use cross-service foreign keys.

Applied migrations are immutable. The initial group shape requires at least two members; migration `003_group_single_owner` explicitly permits one remaining member so an active owner can remain after others leave.

`scripts/migrate.sh` records applied versions in `dialog_schema_migration` and wraps each new migration plus its ledger insert in one transaction. Its bootstrap checks recognize databases created before the ledger was introduced, including whether the singleton-group constraint is already active.

## Tables

- `dialog_space`: integration key, Origin allowlist, personal/group and content policies.
- `dialog`: type, lifecycle, personal pair hash, optional teacher binding (`student_id`, `personal_teacher_id`, `teacher_context_type`, `context_id`), counters, message/event high-water marks, last activity.
- `dialog_member`: role, lifecycle, history boundary, independent read cursor/unread count, mute/archive state.
- `dialog_message`: mutually exclusive user/PersonalTeacher author, `web|telegram` channel, optional LearningAction, optional immutable legacy or `lesson-message-context.v1` JSON anchor, optional opaque PersonalTeacher `assistant_ui` JSONB envelope, same-dialog reply, content, immutable message order, latest event sequence, version and tombstone.
- `dialog_attachment`: service authorization and lifecycle metadata while FileStorage owns bytes.
- `dialog_outbox`: versioned lifecycle payload, finite claim lease, retries and publication evidence.
- `dialog_ws_ticket`: SHA-256 ticket hash, user/space binding and short expiry.
- `dialog_user_block`: directed user block relationship.

## Transaction boundaries

- Personal dialog and both memberships commit together.
- Group and initial membership set commit together.
- Message, dialog counters/sequences, recipient unread increments, attachment binding, and outbox insert commit together.
- A student teacher-dialog message also inserts `dialog.teacher.requested` in that same transaction. It shares the source mutation's event sequence with `dialog.message.created`; outbox uniqueness therefore includes subject.
- Updating or deleting an active student message in an active teacher dialog also inserts body-free `dialog.teacher.context-mutated` in the same transaction. It shares the ordinary lifecycle mutation's event sequence and cannot commit independently.
- An internally appended PersonalTeacher response or general-dialog proactive offer, dialog counters, student unread increment, and `dialog.message.created` evidence commit together.
- Read cursor, unread decrement, event sequence, and outbox insert commit together for one member only.
- Attachment ready/failed transitions and their message/dialog event evidence commit together after idempotent FileStorage work for active messages. Hidden messages finish the attachment transition without public event evidence; a later moderation restore carries the authoritative attachment snapshot.

Attachment activation and deletion batches are claimed atomically with `FOR UPDATE SKIP LOCKED` by moving the corresponding next-attempt timestamp to a finite lease deadline. This permits multiple worker replicas without concurrent normal processing; an abandoned claim becomes eligible again after the lease.

## Sequence distinction

`dialog.max_message_sequence` increments only for message creation. `dialog.max_event_sequence` increments for every durable change. `dialog_message.message_sequence` never changes; `last_event_sequence` advances on edits, deletes, moderation, or attachment projection changes.

Migration `004_teacher_dialogs` is additive and preserves existing personal/group rows. Its down migration fails closed while teacher-dialog, teacher-message, LearningAction, or teacher-request data exists instead of silently deleting it.

Migration `005_lesson_message_context` adds the optional JSON anchor with a
database constraint that limits it to user-authored rows with a revision string
and non-blank bounded selection. Message deletion clears the anchor. Its down
migration fails closed while any anchored row exists, so rollback cannot
silently erase selected-text provenance.

Migration `006_message_channel` backfills `web` and adds the bounded channel.
Its down migration fails closed while any non-web message exists.

Migration `007_assistant_ui` adds nullable JSONB without rewriting existing
messages. Its constraint limits the value to a non-deleted PersonalTeacher
message with the exact `assistant-ui.v1` root shape and no more than 32 blocks;
the application additionally enforces object blocks and a 65,536-byte encoded
cap while leaving block semantics opaque. Its down migration fails closed while
any structured message exists.

Migration `008_lesson_message_context_v1` widens the existing JSONB constraint
to an exact legacy-or-v1 union without rewriting rows. V1 stores exact schema,
mode, Course/lesson UUID strings, RFC3339Nano revision, and mode-dependent
selection; application validation binds the lesson UUID to the dialog context
and enforces the revision parser and Unicode count. Update SQL never replaces
the anchor, while delete clears it. The down migration fails closed while any
v1 anchor exists, then restores the legacy-only constraint.

Migration `009_teacher_context_contracts` additively allowlists the body-free
`dialog.teacher.context-mutated` outbox subject. No table or message row shape
changes. Its down migration fails closed while such events remain.
