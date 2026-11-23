-- Source only: MENT-27 abuse intake and immutable moderation outcome.
CREATE TABLE dialog_message_report (
 id uuid PRIMARY KEY,
 reporter_id uuid NOT NULL,
 request_id uuid NOT NULL,
 message_id uuid NOT NULL REFERENCES dialog_message(id),
 created_at timestamptz NOT NULL,
 revision integer NOT NULL CHECK (revision IN (1,2)),
 command_digest text NOT NULL CHECK (command_digest ~ '^[a-f0-9]{64}$'),
 mentor_binding_id uuid REFERENCES dialog_mentor_binding(id),
 resolution_digest text NOT NULL DEFAULT '',
 resolution_actor_id uuid,
 resolution_request_id uuid,
 snapshot jsonb NOT NULL CHECK (jsonb_typeof(snapshot)='object'),
 UNIQUE (reporter_id,request_id),
 UNIQUE (resolution_actor_id,resolution_request_id),
 CHECK ((revision=1 AND resolution_digest='' AND resolution_actor_id IS NULL AND resolution_request_id IS NULL)
 OR (revision=2 AND resolution_digest ~ '^[a-f0-9]{64}$' AND resolution_actor_id IS NOT NULL AND resolution_request_id IS NOT NULL)),
 CHECK (snapshot->>'id'=id::text AND (snapshot->>'revision')::integer=revision)
);
CREATE INDEX dialog_message_report_queue ON dialog_message_report(created_at DESC,id DESC);
