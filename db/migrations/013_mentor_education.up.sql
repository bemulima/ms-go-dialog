-- The repository migration runner owns the DDL + ledger transaction.
CREATE TABLE dialog_mentor_binding (
 id UUID PRIMARY KEY, dialog_id UUID NOT NULL REFERENCES dialog(id) ON DELETE RESTRICT,
 source_kind TEXT NOT NULL CHECK(source_kind IN('ASSIGNMENT','COHORT')), source_id UUID NOT NULL,
 mentor_id UUID NOT NULL, student_id UUID,
 UNIQUE(source_kind,source_id), UNIQUE(id,dialog_id),
 CHECK((source_kind='ASSIGNMENT' AND student_id IS NOT NULL AND student_id<>mentor_id) OR (source_kind='COHORT' AND student_id IS NULL))
);
CREATE UNIQUE INDEX dialog_mentor_cohort_once ON dialog_mentor_binding(dialog_id) WHERE source_kind='COHORT';
ALTER TABLE dialog_message ADD COLUMN mentor_binding_id UUID, ADD COLUMN mentor_context_revision BIGINT NOT NULL DEFAULT 0, ADD COLUMN mentor_author_membership_id UUID;
ALTER TABLE dialog_message ADD CONSTRAINT mentor_message_binding_fk FOREIGN KEY(mentor_binding_id,dialog_id) REFERENCES dialog_mentor_binding(id,dialog_id) ON DELETE RESTRICT;
ALTER TABLE dialog_message ADD CONSTRAINT mentor_message_shape CHECK((mentor_binding_id IS NULL AND mentor_context_revision=0 AND mentor_author_membership_id IS NULL) OR(mentor_binding_id IS NOT NULL AND mentor_context_revision>0 AND mentor_author_membership_id IS NOT NULL AND author_type='user' AND learning_action_id IS NULL AND lesson_context IS NULL AND assistant_ui IS NULL));
CREATE INDEX dialog_mentor_message_cursor ON dialog_message(mentor_binding_id,mentor_context_revision,message_sequence) WHERE mentor_binding_id IS NOT NULL;
ALTER TABLE dialog_attachment ADD COLUMN mentor_binding_id UUID;
ALTER TABLE dialog_attachment ADD CONSTRAINT mentor_attachment_binding_fk FOREIGN KEY(mentor_binding_id,dialog_id) REFERENCES dialog_mentor_binding(id,dialog_id) ON DELETE RESTRICT;
CREATE TABLE dialog_mentor_read_state(
 binding_id UUID NOT NULL REFERENCES dialog_mentor_binding(id) ON DELETE RESTRICT,
 user_id UUID NOT NULL,membership_id UUID NOT NULL,last_read_sequence BIGINT NOT NULL CHECK(last_read_sequence>=0),
 PRIMARY KEY(binding_id,user_id,membership_id)
);
