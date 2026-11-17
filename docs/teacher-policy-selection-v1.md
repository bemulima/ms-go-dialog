# Explicit Mentor policy intent on a Teacher message

`POST /api/v1/message/create` accepts one optional `teacher_policy_selection`.
It uses the existing authenticated Student author, actual Teacher dialog,
message idempotency key, transaction, and durable Teacher request trigger.
There is no new chat, endpoint, role, bearer credential, or relationship grant.

The field is a closed, non-null union of exactly three strings:

```json
{"kind":"OFFER","offering_id":"11111111-aaaa-4aaa-8aaa-aaaaaaaaaaaa","assignment_id":"22222222-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}
```

```json
{"kind":"COHORT","cohort_id":"11111111-aaaa-4aaa-8aaa-aaaaaaaaaaaa","enrollment_id":"22222222-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}
```

IDs are non-zero canonical lowercase UUIDs. Unknown fields, case aliases,
duplicates, nulls, empty objects, and mixed unions fail with `400 invalid_request`.
The existing body limit remains 1 MiB; the selector itself is limited to 512
bytes. Omission means that this message has no explicit override selection.
Dialog does not copy an intent from an earlier message or a human Mentor binding.

## Context and permission boundary

Only a user/web source in an actual `DialogTypeTeacher` may carry this field.
Supported contexts are `practice_task`, `project`, and `lesson` with an exact
`lesson-message-context.v2` anchor. Human personal/group/Cohort messages,
`general_teacher`, `lesson_task`, legacy/v1 lesson anchors, assistant replies,
proactive messages, Telegram ingress, and canonical onboarding actions cannot
carry it. The source author must already be the Teacher dialog's real Student.

Dialog stores pedagogical intent. The companion Teacher consumer gets it only
through the existing private request-context read and asks the published Mentor
owner (`faaf51c4883bad255ef426c844a802f82e7fcf27`) to prove current Offer Assignment
or Cohort Enrollment, approved Mentor, exact scope, and immutable learning tuple.
No client value here supplies policy text, policy version, progress/code access,
or authority. This field is unrelated to nullable human `mentor_binding_id`.

## Persistence and replay

The nullable message column is written in the same transaction as the ordinary
message and Teacher request outbox trigger. Updates cannot add, remove, or
replace it. Body edits preserve it; soft deletion preserves the identity while
clearing ordinary content/lesson context under the existing contract. Deleted
messages cannot become Teacher generation sources. Physical deletion/truncation
cannot erase retained selected-message history.

The complete selector participates in existing Create replay equality. A changed
or removed selector with the same key returns `409 conflict`. An exact replay
returns the current message version/status/timestamps; this endpoint does not
store an immutable original response DTO. Edits/deletion can therefore make the
original request conflict. **After a prior unknown acknowledgement, a later 409
does not prove that the message never committed.** Clients must retain the exact
pending key, body, attachment IDs, lesson/action context, and selector for
explicit recovery rather than inventing a new key or reporting non-commit.
An attachment-only Practice/Project request with an empty body may still match
its tombstone because attachment IDs are retained. Successful correlation uses
the submitted fields and IDs, not an assumption of version 1 or active status.

Successful REST Message DTOs (including get/window/list/changes) and private
Teacher source/history DTOs echo the exact optional union. Absence omits the
field; it is never JSON null. Assistant messages have no field. Existing ordinary
WebSocket/lifecycle and Teacher request trigger envelopes remain unchanged;
the private source read is authoritative. Selection must not be restored as the
composer's current choice merely because it appears in message history.

## Admission and deployment

`DIALOG_TEACHER_MENTOR_POLICY_ENABLED=false` is the default. A new message with a
selector is rejected with existing `409 feature_disabled` while disabled.
Already committed exact replays remain recoverable after admission rollback.
Ordinary messages without a selector keep their existing behavior. Teacher's
separate `TEACHER_MENTOR_POLICY_ENABLED` gates actual policy application and fails
closed when a selected message reaches an unconfigured consumer.

Migration `015_teacher_policy_selection` is required before deploying the new
SQL projections even while admission is off. Its up file leaves transaction
ownership to actual `scripts/migrate.sh` (DDL and migration ledger together).
The down file owns its transaction, locks `dialog_message`, and refuses to drop
the column while selected messages exist. The database constraint binds the
field to user/web messages, disallows human Mentor binding, and its insert
trigger checks the real numeric Teacher dialog type and supported context.
Migration SQL was not executed in this source change.

The exact private producer vectors are generated through the real private
HTTP handler and use case in in-process tests, then consumed unchanged by the
Teacher adapter. Selected unit/contract/scanner/source-migration checks run
without services or listeners. Gherkin, CI, full suites, SQL execution, model
requests, and runtime acceptance remain outside these checks.
