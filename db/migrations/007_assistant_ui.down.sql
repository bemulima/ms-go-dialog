DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM dialog_message WHERE assistant_ui IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back assistant UI while structured messages exist';
    END IF;
END
$$;

ALTER TABLE dialog_message
    DROP CONSTRAINT chk_dialog_message_assistant_ui,
    DROP COLUMN assistant_ui;
