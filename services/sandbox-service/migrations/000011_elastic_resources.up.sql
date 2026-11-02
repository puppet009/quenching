CREATE TABLE resource_elastic_policies (
 namespace text NOT NULL, name text NOT NULL, version bigint NOT NULL DEFAULT 1,
 policy jsonb NOT NULL, PRIMARY KEY(namespace,name)
);
CREATE TABLE resource_scaling_holds (
 namespace text NOT NULL, hold_id text NOT NULL, reason text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(namespace,hold_id)
);
CREATE TABLE resource_elastic_audit (
 id bigserial PRIMARY KEY, namespace text NOT NULL, name text NOT NULL,
 action text NOT NULL, detail text NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX resource_elastic_audit_created ON resource_elastic_audit(created_at DESC);
