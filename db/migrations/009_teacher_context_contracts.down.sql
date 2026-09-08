DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM dialog_outbox WHERE subject = 'dialog.teacher.context-mutated') THEN
        RAISE EXCEPTION 'cannot roll back teacher context contracts while mutation events exist';
    END IF;
END
$$;

ALTER TABLE dialog_outbox
    DROP CONSTRAINT chk_dialog_outbox_subject;

ALTER TABLE dialog_outbox
    ADD CONSTRAINT chk_dialog_outbox_subject CHECK (subject IN (
        'dialog.created', 'dialog.updated', 'dialog.closed',
        'dialog.member.added', 'dialog.member.removed', 'dialog.member.role_updated',
        'dialog.message.created', 'dialog.message.updated', 'dialog.message.deleted',
        'dialog.message.hidden', 'dialog.message.restored',
        'dialog.attachment.ready', 'dialog.attachment.failed', 'dialog.read.updated',
        'dialog.teacher.requested'
    ));
