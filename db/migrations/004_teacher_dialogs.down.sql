DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM dialog WHERE type = 3)
        OR EXISTS (SELECT 1 FROM dialog_message WHERE author_type <> 'user' OR learning_action_id IS NOT NULL)
        OR EXISTS (SELECT 1 FROM dialog_outbox WHERE subject = 'dialog.teacher.requested') THEN
        RAISE EXCEPTION 'cannot roll back teacher dialogs while teacher-dialog data exists';
    END IF;
END
$$;

ALTER TABLE dialog_outbox
    DROP CONSTRAINT chk_dialog_outbox_subject;

DROP INDEX uq_dialog_outbox_sequence_subject;

ALTER TABLE dialog_outbox
    ADD CONSTRAINT uq_dialog_outbox_sequence UNIQUE (dialog_id, event_sequence),
    ADD CONSTRAINT chk_dialog_outbox_subject CHECK (subject IN (
        'dialog.created', 'dialog.updated', 'dialog.closed',
        'dialog.member.added', 'dialog.member.removed', 'dialog.member.role_updated',
        'dialog.message.created', 'dialog.message.updated', 'dialog.message.deleted',
        'dialog.message.hidden', 'dialog.message.restored',
        'dialog.attachment.ready', 'dialog.attachment.failed', 'dialog.read.updated'
    ));

DROP INDEX idx_dialog_message_learning_action;
DROP INDEX uq_dialog_message_teacher_idempotency;

ALTER TABLE dialog_message
    DROP CONSTRAINT fk_dialog_message_teacher_binding,
    DROP CONSTRAINT chk_dialog_message_author,
    ALTER COLUMN sender_id SET NOT NULL,
    DROP COLUMN learning_action_id,
    DROP COLUMN personal_teacher_id,
    DROP COLUMN author_type;

DROP INDEX uq_dialog_teacher_context;

ALTER TABLE dialog
    DROP CONSTRAINT chk_dialog_shape,
    DROP CONSTRAINT chk_dialog_type,
    DROP CONSTRAINT fk_dialog_teacher_student_member,
    DROP CONSTRAINT uq_dialog_id_personal_teacher;

ALTER TABLE dialog
    DROP COLUMN context_id,
    DROP COLUMN teacher_context_type,
    DROP COLUMN personal_teacher_id,
    DROP COLUMN student_id;

ALTER TABLE dialog
    ADD CONSTRAINT chk_dialog_type CHECK (type IN (1, 2)),
    ADD CONSTRAINT chk_dialog_personal_shape CHECK (
        (type = 1 AND personal_key IS NOT NULL AND octet_length(personal_key) = 32 AND title IS NULL AND member_count = 2)
        OR
        (type = 2 AND personal_key IS NULL AND title IS NOT NULL AND char_length(btrim(title)) BETWEEN 1 AND 200 AND member_count BETWEEN 1 AND 1000)
    );
