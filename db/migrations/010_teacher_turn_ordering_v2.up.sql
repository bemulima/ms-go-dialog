ALTER TABLE dialog
    ADD COLUMN max_teacher_turn_sequence BIGINT NOT NULL DEFAULT 0,
    ADD CONSTRAINT chk_dialog_max_teacher_turn_sequence CHECK (max_teacher_turn_sequence >= 0);

ALTER TABLE dialog_message
    ADD COLUMN teacher_turn_sequence BIGINT,
    ADD CONSTRAINT chk_dialog_message_teacher_turn_sequence CHECK (
        teacher_turn_sequence IS NULL
        OR (teacher_turn_sequence >= 1 AND author_type = 'user')
    );

CREATE UNIQUE INDEX uq_dialog_message_teacher_turn_sequence
    ON dialog_message (dialog_id, teacher_turn_sequence)
    WHERE teacher_turn_sequence IS NOT NULL;

COMMENT ON COLUMN dialog.max_teacher_turn_sequence IS
    'Dialog-owned high-water mark for dense V2 Teacher request turns; legacy rows are not backfilled';

COMMENT ON COLUMN dialog_message.teacher_turn_sequence IS
    'Private V2 Teacher request order; never a REST, WebSocket, or lifecycle projection field';
