DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM dialog_message WHERE channel <> 'web') THEN
        RAISE EXCEPTION 'cannot remove message channel while non-web messages exist';
    END IF;
END $$;

ALTER TABLE dialog_message
    DROP CONSTRAINT chk_dialog_message_channel,
    DROP COLUMN channel;
