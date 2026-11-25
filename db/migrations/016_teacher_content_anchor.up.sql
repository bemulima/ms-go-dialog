ALTER TABLE dialog_message
    ADD COLUMN content_anchor jsonb,
    ADD CONSTRAINT chk_dialog_message_teacher_content_anchor CHECK (
        content_anchor IS NULL OR (
            author_type = 'user' AND mentor_binding_id IS NULL AND status <> 2
            AND jsonb_typeof(content_anchor) = 'object'
            AND octet_length(content_anchor::text) <= 768
            AND (content_anchor->>'schema' = 'teacher-content-anchor.v1') IS TRUE
        )
    );
