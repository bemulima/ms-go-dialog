BEGIN;
LOCK TABLE dialog_message IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM dialog_message WHERE teacher_policy_selection IS NOT NULL) THEN
  RAISE EXCEPTION 'cannot discard retained Teacher policy selections';
 END IF;
END $$;
DROP TRIGGER dialog_teacher_policy_selection_no_truncate ON dialog_message;
DROP TRIGGER dialog_teacher_policy_selection_immutable ON dialog_message;
DROP FUNCTION protect_dialog_teacher_policy_selection();
ALTER TABLE dialog_message DROP CONSTRAINT chk_dialog_message_teacher_policy_selection;
ALTER TABLE dialog_message DROP COLUMN teacher_policy_selection;
COMMIT;
