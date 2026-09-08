# Dialog business rules

## Identity and access

- Every user API requires a non-guest UUID identity supplied by the trusted gateway.
- Request bodies never select the acting user or sender.
- A user may read or mutate a dialog only while its membership is active.
- Personal dialogs contain exactly two immutable participant identities and are unique per unordered pair inside one space.
- Teacher dialogs contain exactly one real student member and bind that student to one logical `personal_teacher_id`; the teacher is not an `ms-go-user` participant.
- Teacher dialogs are unique per space, student, PersonalTeacher, context type, and context ID. `general_teacher` has no context ID; `lesson`, `lesson_task`, `practice_task`, and `project` require one.
- Teacher-dialog provisioning is an internal command. Browsers cannot choose or impersonate a `personal_teacher_id`.
- Personal/group creation and group-member addition require every relevant participant to be active in `ms-go-user`; validation is batched and fails closed before the mutation transaction.
- Group owners and admins manage membership. A group must retain an active owner.
- Rejoining a group starts a new active membership interval and a new history boundary after the current last message.
- Dialogs in a disabled space are omitted from user lists and cannot be read or mutated until the space is active again.
- Blocking prevents new personal-dialog creation and new messages between the blocked pair.

## Messages

- A message belongs to exactly one dialog and has immutable `message_sequence`.
- An optional reply target must belong to the same dialog. Replies do not form a storage tree.
- Content requires non-blank text or at least one attachment.
- Raw HTML is rejected. Only absolute HTTP(S) links are accepted when links are enabled.
- Create is idempotent per sender UUID key. Update/delete use expected integer version.
- Message author is explicit: `user` has `sender_id`, while `personal_teacher` has `personal_teacher_id`; both at once are forbidden.
- Every message has a transport channel. Existing/browser/proactive messages use `web`; a trusted Telegram adapter may append only to the exact bound student's active `general_teacher` dialog. A teacher response inherits its source channel.
- Student messages in `lesson_task`, `practice_task`, and `project` teacher dialogs require an opaque `learning_action_id`. Other dialog contexts reject it. Dialog does not validate mastery or targets; Teacher verifies action ownership and OPEN state through Student.
- New student messages in a `lesson` teacher dialog use `lesson-message-context.v1`: exact `schema`, `mode`, Course UUID, lesson UUID, and RFC3339Nano `content_revision`. `lesson_overview` forbids `selected_text`; `selection` requires a non-blank selection of at most 12,000 Unicode code points. The lesson UUID must equal the dialog's immutable lesson context ID. Other dialog contexts and PersonalTeacher-authored messages reject the anchor. Dialog validates only shape and binding; Teacher verifies Course ownership, revision, and any exact substring.
- Selected text is copied exactly without trimming or reformatting. The complete anchor is immutable after append and participates in create-idempotency replay. Body edits retain it; deletion clears it with the private message content.
- Existing legacy `{content_revision, selected_text}` anchors remain readable, replayable, and temporarily accepted during producer rollout. They are not upgraded or assigned invented Course/lesson IDs.
- A committed student message in a teacher dialog atomically records both normal `dialog.message.created` evidence and a body-free `dialog.teacher.requested` trigger. A teacher-authored response never recursively emits a teacher request.
- The teacher trigger contains bounded source-channel metadata so delivery retry never needs another conversation store. It still excludes the body and history.
- Teacher responses are accepted only through the internal idempotent append command, must match the bound PersonalTeacher and source student message, inherit its LearningAction reference, and reply to that source message.
- Internal PersonalTeacher response/proactive append may carry `assistant_ui`. Dialog requires the exact `assistant-ui.v1` root envelope, at most 32 object blocks, and at most 65,536 encoded JSON bytes; it preserves block objects opaquely and does not validate their pedagogical discriminators or data.
- Structured UI never replaces text: the existing body policy remains mandatory for an internal PersonalTeacher append. Browser create/update cannot set `assistant_ui`, and user-authored messages cannot store it.
- Teacher append idempotency compares the normalized complete `body + assistant_ui` representation. Reusing a key with a changed or absent envelope conflicts.
- A proactive PersonalTeacher offer is accepted only through the internal command for the same student's active `general_teacher` dialog. It has no synthetic user source, reply, LearningAction, or lesson context, and is idempotent by the Teacher-supplied analytics event ID.
- Delete keeps a tombstone, identity, order, reply references, and sequence, but clears the complete lesson anchor and structured UI together with message content.

## Read state

- Every member owns `last_read_message_sequence`, `unread_count`, and `last_read_at`.
- Reading by one member never changes another member's row.
- A read cursor is monotonic and cannot exceed the dialog's current message sequence.
- A valid cursor at or below the member's current cursor is an idempotent no-op. This is required because viewport batches and multiple devices can arrive out of order.
- New messages increment unread count for active recipients only, never for the sender.
- `read-all` takes the current maximum message sequence inside its transaction and sets only the actor's unread count to zero.
- A new group member defaults to history beginning after the current last message. That boundary is enforced by window/list/changes/get/reply and attachment download authorization, not only stored as metadata.
- A disabled space cannot mutate read state even if a stale client still holds a dialog ID.

## Delivery

- Durable mutations allocate event sequence and insert outbox evidence in the same transaction.
- The lifecycle event and teacher-request integration event for one source-message mutation share one event sequence and have different subjects.
- FileStorage and NATS calls never run inside the domain transaction.
- REST is the command source of truth. WebSocket is a bounded low-latency projection.
- Clients deduplicate by event ID and reconcile event-sequence gaps through REST.
