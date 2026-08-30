ALTER TABLE dialog
    ADD COLUMN student_id UUID,
    ADD COLUMN personal_teacher_id UUID,
    ADD COLUMN teacher_context_type VARCHAR(32),
    ADD COLUMN context_id UUID;

ALTER TABLE dialog
    DROP CONSTRAINT chk_dialog_type,
    DROP CONSTRAINT chk_dialog_personal_shape;

ALTER TABLE dialog
    ADD CONSTRAINT chk_dialog_type CHECK (type IN (1, 2, 3)),
    ADD CONSTRAINT chk_dialog_shape CHECK (
        (
            type = 1
            AND personal_key IS NOT NULL
            AND octet_length(personal_key) = 32
            AND title IS NULL
            AND member_count = 2
            AND student_id IS NULL
            AND personal_teacher_id IS NULL
            AND teacher_context_type IS NULL
            AND context_id IS NULL
        )
        OR
        (
            type = 2
            AND personal_key IS NULL
            AND title IS NOT NULL
            AND char_length(btrim(title)) BETWEEN 1 AND 200
            AND member_count BETWEEN 1 AND 1000
            AND student_id IS NULL
            AND personal_teacher_id IS NULL
            AND teacher_context_type IS NULL
            AND context_id IS NULL
        )
        OR
        (
            type = 3
            AND personal_key IS NULL
            AND title IS NULL
            AND member_count = 1
            AND student_id IS NOT NULL
            AND personal_teacher_id IS NOT NULL
            AND (
                (teacher_context_type = 'general_teacher' AND context_id IS NULL)
                OR
                (teacher_context_type IN ('lesson', 'lesson_task', 'practice_task', 'project') AND context_id IS NOT NULL)
            )
        )
    );

ALTER TABLE dialog
    ADD CONSTRAINT uq_dialog_id_personal_teacher UNIQUE (id, personal_teacher_id),
    ADD CONSTRAINT fk_dialog_teacher_student_member
        FOREIGN KEY (id, student_id)
        REFERENCES dialog_member (dialog_id, user_id)
        ON DELETE RESTRICT
        DEFERRABLE INITIALLY DEFERRED;

CREATE UNIQUE INDEX uq_dialog_teacher_context
    ON dialog (
        space_id,
        student_id,
        personal_teacher_id,
        teacher_context_type,
        COALESCE(context_id, '00000000-0000-0000-0000-000000000000'::UUID)
    )
    WHERE type = 3;

ALTER TABLE dialog_message
    ADD COLUMN author_type VARCHAR(32) NOT NULL DEFAULT 'user',
    ADD COLUMN personal_teacher_id UUID,
    ADD COLUMN learning_action_id UUID,
    ALTER COLUMN sender_id DROP NOT NULL;

ALTER TABLE dialog_message
    ADD CONSTRAINT chk_dialog_message_author CHECK (
        (author_type = 'user' AND sender_id IS NOT NULL AND personal_teacher_id IS NULL)
        OR
        (author_type = 'personal_teacher' AND sender_id IS NULL AND personal_teacher_id IS NOT NULL)
    ),
    ADD CONSTRAINT fk_dialog_message_teacher_binding
        FOREIGN KEY (dialog_id, personal_teacher_id)
        REFERENCES dialog (id, personal_teacher_id)
        ON DELETE RESTRICT;

CREATE UNIQUE INDEX uq_dialog_message_teacher_idempotency
    ON dialog_message (personal_teacher_id, idempotency_key)
    WHERE author_type = 'personal_teacher';

CREATE INDEX idx_dialog_message_learning_action
    ON dialog_message (learning_action_id, dialog_id, message_sequence)
    WHERE learning_action_id IS NOT NULL;

ALTER TABLE dialog_outbox
    DROP CONSTRAINT uq_dialog_outbox_sequence,
    DROP CONSTRAINT chk_dialog_outbox_subject;

CREATE UNIQUE INDEX uq_dialog_outbox_sequence_subject
    ON dialog_outbox (dialog_id, event_sequence, subject);

ALTER TABLE dialog_outbox
    ADD CONSTRAINT chk_dialog_outbox_subject CHECK (subject IN (
        'dialog.created', 'dialog.updated', 'dialog.closed',
        'dialog.member.added', 'dialog.member.removed', 'dialog.member.role_updated',
        'dialog.message.created', 'dialog.message.updated', 'dialog.message.deleted',
        'dialog.message.hidden', 'dialog.message.restored',
        'dialog.attachment.ready', 'dialog.attachment.failed', 'dialog.read.updated',
        'dialog.teacher.requested'
    ));
