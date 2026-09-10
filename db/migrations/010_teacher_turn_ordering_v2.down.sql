DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM dialog WHERE max_teacher_turn_sequence <> 0)
        OR EXISTS (SELECT 1 FROM dialog_message WHERE teacher_turn_sequence IS NOT NULL)
        OR EXISTS (
            SELECT 1
            FROM dialog_outbox
            WHERE subject = 'dialog.teacher.requested' AND schema_version >= 2
        ) THEN
        RAISE EXCEPTION 'cannot roll back teacher turn ordering v2 while ordered turns or v2 requests exist';
    END IF;
END
$$;

DROP INDEX uq_dialog_message_teacher_turn_sequence;

ALTER TABLE dialog_message
    DROP CONSTRAINT chk_dialog_message_teacher_turn_sequence,
    DROP COLUMN teacher_turn_sequence;

ALTER TABLE dialog
    DROP CONSTRAINT chk_dialog_max_teacher_turn_sequence,
    DROP COLUMN max_teacher_turn_sequence;
