ALTER TABLE dialog_message
    ADD COLUMN lesson_context JSONB;

ALTER TABLE dialog_message
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
