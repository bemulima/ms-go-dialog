DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM dialog_message WHERE lesson_context IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back lesson message context while anchored messages exist';
    END IF;
END
$$;

ALTER TABLE dialog_message
    DROP CONSTRAINT chk_dialog_message_lesson_context,
    DROP COLUMN lesson_context;
