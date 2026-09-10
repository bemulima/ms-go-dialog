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
- `DIALOG_TEACHER_ORDERING_V2_ENABLED` defaults to `false`. When enabled uniformly after the V2 consumer cutover, each new qualifying Student source message receives the next dense private per-dialog `teacher_turn_sequence` while the source message, Dialog high-water counter, ordinary lifecycle event, and exactly one V2 Teacher trigger commit atomically. Idempotent replay allocates nothing; PersonalTeacher replies/proactive messages and legacy rows remain unsequenced. V1 and V2 triggers never dual emit.
- `teacher_turn_sequence` is internal ordering evidence only. It is never inferred from message/event timestamps or delivery order, and it is not exposed through REST, WebSocket, or ordinary `dialog.message.created` evidence.
- The teacher trigger contains bounded source-channel metadata so delivery retry never needs another conversation store. V2 source metadata includes a positive `source_message_version` and an explicit nullable `reply_to_message_id` from the committed source snapshot; pre-extension queued V2 payloads may omit either additive field. It still excludes the body and history.
- Teacher responses are accepted only through the internal idempotent append command, must match the bound PersonalTeacher and source student message, inherit its LearningAction reference, and reply to that source message.
- Internal PersonalTeacher response/proactive append may carry `assistant_ui`. Dialog requires the exact `assistant-ui.v1` root envelope, at most 32 object blocks, and at most 65,536 encoded JSON bytes; it preserves block objects opaquely and does not validate their pedagogical discriminators or data.
- Structured UI never replaces text: the existing body policy remains mandatory for an internal PersonalTeacher append. Browser create/update cannot set `assistant_ui`, and user-authored messages cannot store it.
- Teacher append idempotency compares the normalized complete `body + assistant_ui` representation. Reusing a key with a changed or absent envelope conflicts.
- `DIALOG_CANONICAL_STUDENT_TURN_ENABLED` defaults to `false` and may be enabled only with `DIALOG_TEACHER_ORDERING_V2_ENABLED=true`. Its exact-token internal command materializes one normal `web` Student message only for an active bound `general_teacher` dialog. It requires server-derived non-blank body, exact active PersonalTeacher assistant-UI source/version/digest, a reply to that source, private dense V2 ordering, and exactly one V2 Teacher trigger in the same transaction. Browser `message/create` cannot accept trusted identity and the browser must never dual-write Dialog plus Teacher. With either runtime gate disabled, a new receipt has no effect; an exact committed replay remains recoverable with no new event or sequence.
- Migration `011_canonical_student_turn` persists private `canonical-student-turn-identity.v1` evidence keyed by `action_receipt_id`, `canonical_message_command_id`, canonical message ID, and unique `(dialog_id, source_prompt_message_id, block_id, action_id)`. It contains no canonical body, browser action payload, assistant UI, or Student state. `canonical-student-turn-interaction.v1` has sole kind `teacher_action_receipt`, source prompt ID/version, bounded opaque block/action IDs, and the lowercase SHA-256 digest of stored canonical `assistant_ui`. Dialog validates private shape, binding, source version, and UI digest but not pedagogical action semantics; Teacher validates the source/action before issuing the command. The ledger is never in a public Message/View, browser request, WebSocket frame, or ordinary lifecycle event.
- An accepted canonical Student message is immutable to public update/delete so its receipt, source reply, body, V2 ordering, and trigger cannot be altered. Moderator hide/restore remains the existing administrative lifecycle.
- The causal ledger is `source_prompt_message_id` -> `action_receipt_id` -> `canonical_student_message_id` -> `source_event_id` -> `teacher_job_id` -> `teacher_response_message_id`. Every ledger identifier, including `correlation_id` and `causation_id`, is a non-zero UUID; `block_id` and `action_id` remain bounded opaque identifiers, and `source_ui_digest` remains a SHA-256 digest. `correlation_id` equals `action_receipt_id` across the action chain. Causation is boundary-specific: receipt validation is caused by `source_prompt_message_id`; Dialog materialization by `action_receipt_id`; the `dialog.teacher.requested` event by `canonical_student_message_id`; Teacher-job intake by `source_event_id`; and Teacher response append by `teacher_job_id`. Action V2 additionally carries its canonical source message's positive version and explicit reply-to-prompt UUID, as well as optional `action_receipt_id`; it does not include block/action/digest/body or `student_command_id`.
- Reserved end-to-end observability names are `action_receipt_id`, `correlation_id`, `causation_id`, `source_prompt_message_id`, `source_prompt_message_version`, `source_ui_digest`, `block_id`, `action_id`, `canonical_message_command_id`, `canonical_student_message_id`, `source_event_id`, `teacher_job_id`, `teacher_response_message_id`, `no_effect_proof_id`, `recoverable_outcome_id`, `failure_code`, `source_message_sequence`, `source_message_version`, `reply_to_message_id`, `teacher_turn_sequence`, and `event_sequence`. IDs are recorded only in the owning durable records and structured logs; they are never metric labels. The fields are a shared ledger vocabulary, not a separate observability subsystem.
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
