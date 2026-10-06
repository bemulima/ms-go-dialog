ALTER TABLE dialog_message
    DROP CONSTRAINT chk_dialog_message_lesson_context,
    ADD CONSTRAINT chk_dialog_message_lesson_context CHECK ((
        lesson_context IS NULL
        OR (
            author_type = 'user'
            AND jsonb_typeof(lesson_context) = 'object'
            AND lesson_context ? 'content_revision'
            AND jsonb_typeof(lesson_context->'content_revision') = 'string'
            AND char_length(btrim(lesson_context->>'content_revision')) BETWEEN 1 AND 64
            AND (
                (
                    NOT (lesson_context ? 'schema')
                    AND lesson_context - 'content_revision' - 'selected_text' = '{}'::jsonb
                    AND lesson_context ? 'selected_text'
                    AND jsonb_typeof(lesson_context->'selected_text') = 'string'
                    AND char_length(btrim(lesson_context->>'selected_text')) >= 1
                    AND char_length(lesson_context->>'selected_text') <= 48000
                )
                OR (
                    lesson_context->>'schema' = 'lesson-message-context.v1'
                    AND jsonb_typeof(lesson_context->'schema') = 'string'
                    AND lesson_context ? 'mode'
                    AND jsonb_typeof(lesson_context->'mode') = 'string'
                    AND lesson_context ? 'course_id'
                    AND jsonb_typeof(lesson_context->'course_id') = 'string'
                    AND lesson_context->>'course_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                    AND lesson_context ? 'lesson_id'
                    AND jsonb_typeof(lesson_context->'lesson_id') = 'string'
                    AND lesson_context->>'lesson_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                    AND lesson_context - 'schema' - 'mode' - 'course_id' - 'lesson_id' - 'content_revision' - 'selected_text' = '{}'::jsonb
                    AND (
                        (lesson_context->>'mode' = 'lesson_overview' AND NOT (lesson_context ? 'selected_text'))
                        OR (
                            lesson_context->>'mode' = 'selection'
                            AND lesson_context ? 'selected_text'
                            AND jsonb_typeof(lesson_context->'selected_text') = 'string'
                            AND char_length(btrim(lesson_context->>'selected_text')) >= 1
                            AND char_length(lesson_context->>'selected_text') <= 12000
                        )
                    )
                )
                OR ((
                    lesson_context->>'schema' = 'lesson-message-context.v2'
                    AND lesson_context ?& ARRAY['schema', 'mode', 'course_id', 'lesson_id', 'content_revision', 'learning_path_id', 'learning_path_item_id', 'content_digest']
                    AND lesson_context - 'schema' - 'mode' - 'course_id' - 'lesson_id' - 'content_revision' - 'learning_path_id' - 'learning_path_item_id' - 'content_digest' - 'selected_text' = '{}'::jsonb
                    AND jsonb_typeof(lesson_context->'schema') = 'string'
                    AND jsonb_typeof(lesson_context->'mode') = 'string'
                    AND jsonb_typeof(lesson_context->'course_id') = 'string'
                    AND jsonb_typeof(lesson_context->'lesson_id') = 'string'
                    AND jsonb_typeof(lesson_context->'content_revision') = 'string'
                    AND jsonb_typeof(lesson_context->'learning_path_id') = 'string'
                    AND jsonb_typeof(lesson_context->'learning_path_item_id') = 'string'
                    AND jsonb_typeof(lesson_context->'content_digest') = 'string'
                    AND lesson_context->>'course_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                    AND lesson_context->>'course_id' <> '00000000-0000-0000-0000-000000000000'
                    AND lesson_context->>'lesson_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                    AND lesson_context->>'lesson_id' <> '00000000-0000-0000-0000-000000000000'
                    AND lesson_context->>'content_revision' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                    AND lesson_context->>'content_revision' <> '00000000-0000-0000-0000-000000000000'
                    AND lesson_context->>'learning_path_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                    AND lesson_context->>'learning_path_id' <> '00000000-0000-0000-0000-000000000000'
                    AND lesson_context->>'learning_path_item_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                    AND lesson_context->>'learning_path_item_id' <> '00000000-0000-0000-0000-000000000000'
                    AND lesson_context->>'content_digest' ~ '^sha256:[0-9a-f]{64}$'
                    AND (
                        (lesson_context->>'mode' = 'lesson_overview' AND NOT (lesson_context ? 'selected_text'))
                        OR (
                            lesson_context->>'mode' = 'selection'
                            AND lesson_context ? 'selected_text'
                            AND jsonb_typeof(lesson_context->'selected_text') = 'string'
                            AND lesson_context->>'selected_text' ~ '[^[:space:]]'
                            AND char_length(lesson_context->>'selected_text') BETWEEN 1 AND 12000
                        )
                    )
                ) IS TRUE)
            )
        )
    ) IS TRUE);

COMMENT ON COLUMN dialog_message.lesson_context IS
    'Immutable legacy, v1 Course timestamp, or v2 frozen Student assignment anchor; application binds versioned lesson_id to dialog context';
