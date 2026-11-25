BEGIN;
LOCK TABLE dialog_message IN ACCESS EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM dialog_message WHERE content_anchor IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back while teacher content anchors exist';
    END IF;
END $$;
ALTER TABLE dialog_message DROP COLUMN content_anchor;
COMMIT;
