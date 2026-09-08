DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM dialog_message
        WHERE lesson_context->>'schema' = 'lesson-message-context.v1'
    ) THEN
        RAISE EXCEPTION 'cannot roll back lesson message context v1 while versioned anchors exist';
    END IF;
END
$$;

ALTER TABLE dialog_message
    DROP CONSTRAINT chk_dialog_message_lesson_context,
    ADD CONSTRAINT chk_dialog_message_lesson_context CHECK (
        lesson_context IS NULL
        OR (
            author_type = 'user'
            AND jsonb_typeof(lesson_context) = 'object'
            AND lesson_context ? 'content_revision'
            AND lesson_context ? 'selected_text'
            AND jsonb_typeof(lesson_context->'content_revision') = 'string'
            AND char_length(btrim(lesson_context->>'content_revision')) BETWEEN 1 AND 64
            AND jsonb_typeof(lesson_context->'selected_text') = 'string'
            AND char_length(btrim(lesson_context->>'selected_text')) BETWEEN 1 AND 48000
        )
    );
