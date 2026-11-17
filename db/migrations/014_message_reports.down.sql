-- Never silently discard a report or its moderation history on rollback.
BEGIN;
LOCK TABLE dialog_message_report IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS (SELECT 1 FROM dialog_message_report) THEN
 RAISE EXCEPTION 'message reports exist; rollback would discard moderation history';
END IF; END $$;
DROP TABLE dialog_message_report;
COMMIT;
