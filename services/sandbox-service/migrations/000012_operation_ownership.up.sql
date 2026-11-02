-- Sandbox becomes the canonical source owner; legacy tables remain for backup.
CREATE TABLE sandbox_session_operations (LIKE session_operations INCLUDING ALL);
CREATE SEQUENCE sandbox_session_operations_id_seq;
ALTER TABLE sandbox_session_operations ALTER COLUMN id SET DEFAULT nextval('sandbox_session_operations_id_seq');
ALTER SEQUENCE sandbox_session_operations_id_seq OWNED BY sandbox_session_operations.id;
INSERT INTO sandbox_session_operations SELECT * FROM session_operations;
SELECT setval('sandbox_session_operations_id_seq',COALESCE((SELECT max(id) FROM sandbox_session_operations),1),EXISTS(SELECT 1 FROM sandbox_session_operations));
ALTER TABLE sandbox_session_operations ADD COLUMN projection_delivered BOOLEAN NOT NULL DEFAULT false;
CREATE TABLE sandbox_operation_reports(request_id TEXT PRIMARY KEY,request_hash TEXT NOT NULL,receipt JSONB,created_at TIMESTAMPTZ NOT NULL DEFAULT now());
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM operation_intents a JOIN sandbox_operation_intents b USING(operation_id) WHERE a.session_id IS DISTINCT FROM b.session_id OR a.evidence->>'actor' IS DISTINCT FROM b.evidence->>'actor' OR a.evidence->>'kind' IS DISTINCT FROM b.evidence->>'kind' OR (a.revision=b.revision AND a.evidence IS DISTINCT FROM b.evidence)) THEN RAISE EXCEPTION 'Historical operation identity conflict'; END IF;
 IF EXISTS(SELECT 1 FROM operation_events a JOIN sandbox_operation_events b USING(event_id) WHERE a.operation_id IS DISTINCT FROM b.operation_id OR a.session_id IS DISTINCT FROM b.session_id OR a.sequence IS DISTINCT FROM b.sequence OR a.evidence IS DISTINCT FROM b.evidence OR a.delivery IS DISTINCT FROM b.delivery) THEN RAISE EXCEPTION 'Historical operation event conflict'; END IF;
 IF EXISTS(SELECT 1 FROM operation_texts a JOIN sandbox_operation_texts b USING(ref) WHERE a.operation_id IS DISTINCT FROM b.operation_id OR a.session_id IS DISTINCT FROM b.session_id OR a.body IS DISTINCT FROM b.body) THEN RAISE EXCEPTION 'Historical operation text conflict'; END IF;
END $$;
INSERT INTO sandbox_operation_intents SELECT * FROM operation_intents ON CONFLICT(operation_id) DO UPDATE SET evidence=CASE WHEN EXCLUDED.revision>sandbox_operation_intents.revision THEN EXCLUDED.evidence ELSE sandbox_operation_intents.evidence END,revision=GREATEST(EXCLUDED.revision,sandbox_operation_intents.revision);
INSERT INTO sandbox_operation_events(event_id,operation_id,session_id,sequence,evidence,delivery,delivered,created_at) SELECT event_id,operation_id,session_id,sequence,evidence,delivery,false,created_at FROM operation_events ON CONFLICT(event_id) DO NOTHING;
INSERT INTO sandbox_operation_texts SELECT * FROM operation_texts ON CONFLICT(ref) DO NOTHING;
UPDATE sandbox_operation_events SET delivered=false;
