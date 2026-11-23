-- scripts/migrate.sh owns BEGIN/DDL/ledger/COMMIT for every *.up.sql.
ALTER TABLE dialog_message ADD COLUMN teacher_policy_selection JSONB;
ALTER TABLE dialog_message ADD CONSTRAINT chk_dialog_message_teacher_policy_selection CHECK (
 teacher_policy_selection IS NULL OR (
  jsonb_typeof(teacher_policy_selection)='object' AND octet_length(teacher_policy_selection::text)<=512
  AND jsonb_typeof(teacher_policy_selection->'kind')='string'
  AND author_type='user' AND channel='web' AND mentor_binding_id IS NULL
  AND (
   (teacher_policy_selection->>'kind'='OFFER'
    AND teacher_policy_selection ?& ARRAY['kind','offering_id','assignment_id']
    AND teacher_policy_selection - ARRAY['kind','offering_id','assignment_id'] = '{}'::jsonb
    AND jsonb_typeof(teacher_policy_selection->'offering_id')='string'
    AND jsonb_typeof(teacher_policy_selection->'assignment_id')='string'
    AND teacher_policy_selection->>'offering_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    AND teacher_policy_selection->>'assignment_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    AND teacher_policy_selection->>'offering_id'<>'00000000-0000-0000-0000-000000000000'
    AND teacher_policy_selection->>'assignment_id'<>'00000000-0000-0000-0000-000000000000')
   OR
   (teacher_policy_selection->>'kind'='COHORT'
    AND teacher_policy_selection ?& ARRAY['kind','cohort_id','enrollment_id']
    AND teacher_policy_selection - ARRAY['kind','cohort_id','enrollment_id'] = '{}'::jsonb
    AND jsonb_typeof(teacher_policy_selection->'cohort_id')='string'
    AND jsonb_typeof(teacher_policy_selection->'enrollment_id')='string'
    AND teacher_policy_selection->>'cohort_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    AND teacher_policy_selection->>'enrollment_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    AND teacher_policy_selection->>'cohort_id'<>'00000000-0000-0000-0000-000000000000'
    AND teacher_policy_selection->>'enrollment_id'<>'00000000-0000-0000-0000-000000000000')
  )
 )
);
CREATE FUNCTION protect_dialog_teacher_policy_selection() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='TRUNCATE' THEN
  IF EXISTS(SELECT 1 FROM dialog_message WHERE teacher_policy_selection IS NOT NULL) THEN
   RAISE EXCEPTION 'retained Teacher policy selection cannot be truncated';
  END IF;
  RETURN NULL;
 END IF;
 IF TG_OP='DELETE' THEN
  IF OLD.teacher_policy_selection IS NOT NULL THEN RAISE EXCEPTION 'retained Teacher policy selection cannot be deleted'; END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='UPDATE' THEN
  IF NEW.teacher_policy_selection IS DISTINCT FROM OLD.teacher_policy_selection THEN
   RAISE EXCEPTION 'Teacher policy selection is immutable from message creation';
  END IF;
  IF OLD.teacher_policy_selection IS NOT NULL AND (NEW.dialog_id IS DISTINCT FROM OLD.dialog_id
   OR NEW.sender_id IS DISTINCT FROM OLD.sender_id OR NEW.author_type IS DISTINCT FROM OLD.author_type
   OR NEW.channel IS DISTINCT FROM OLD.channel OR NEW.mentor_binding_id IS NOT NULL) THEN
   RAISE EXCEPTION 'Teacher policy selection identity is immutable';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.teacher_policy_selection IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM dialog d WHERE d.id=NEW.dialog_id AND d.type=3
   AND d.student_id=NEW.sender_id AND NEW.author_type='user' AND NEW.channel='web'
   AND (d.teacher_context_type IN ('practice_task','project')
    OR (d.teacher_context_type='lesson' AND NEW.lesson_context->>'schema'='lesson-message-context.v2'))
 ) THEN RAISE EXCEPTION 'Teacher policy selection requires an explicit supported Teacher context'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER dialog_teacher_policy_selection_immutable BEFORE INSERT OR UPDATE OR DELETE ON dialog_message
 FOR EACH ROW EXECUTE FUNCTION protect_dialog_teacher_policy_selection();
CREATE TRIGGER dialog_teacher_policy_selection_no_truncate BEFORE TRUNCATE ON dialog_message
 FOR EACH STATEMENT EXECUTE FUNCTION protect_dialog_teacher_policy_selection();
