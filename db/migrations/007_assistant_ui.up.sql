ALTER TABLE dialog_message
    ADD COLUMN assistant_ui JSONB;

ALTER TABLE dialog_message
    ADD CONSTRAINT chk_dialog_message_assistant_ui CHECK (
        assistant_ui IS NULL
        OR (
            author_type = 'personal_teacher'
            AND status <> 2
            AND jsonb_typeof(assistant_ui) = 'object'
            AND assistant_ui ? 'schema'
            AND assistant_ui ? 'blocks'
            AND assistant_ui->>'schema' = 'assistant-ui.v1'
            AND jsonb_typeof(assistant_ui->'blocks') = 'array'
            AND jsonb_array_length(assistant_ui->'blocks') <= 32
            AND NOT jsonb_path_exists(assistant_ui->'blocks', '$[*] ? (@.type() != "object")')
            AND assistant_ui - 'schema' - 'blocks' = '{}'::jsonb
        )
    );
