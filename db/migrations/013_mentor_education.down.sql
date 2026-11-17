BEGIN;
LOCK TABLE dialog_mentor_binding,dialog_message,dialog_attachment IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM dialog_mentor_binding) THEN RAISE EXCEPTION 'Mentor conversation history exists; destructive rollback refused'; END IF; END $$;
DROP TABLE dialog_mentor_read_state;
ALTER TABLE dialog_attachment DROP CONSTRAINT mentor_attachment_binding_fk,DROP COLUMN mentor_binding_id;
ALTER TABLE dialog_message DROP CONSTRAINT mentor_message_shape,DROP CONSTRAINT mentor_message_binding_fk,DROP COLUMN mentor_binding_id,DROP COLUMN mentor_context_revision,DROP COLUMN mentor_author_membership_id;
DROP TABLE dialog_mentor_binding;
COMMIT;
