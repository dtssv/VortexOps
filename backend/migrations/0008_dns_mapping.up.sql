-- Phase 4: 域名→Pod IP 映射表
CREATE TABLE IF NOT EXISTS vo_dns_zones (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO vo_dns_zones (name, description)
VALUES ('vortexops.local', 'VortexOps internal DNS zone')
ON CONFLICT (name) DO NOTHING;

CREATE TABLE IF NOT EXISTS vo_dns_records (
    id BIGSERIAL PRIMARY KEY,
    group_id BIGINT NOT NULL REFERENCES vo_groups(id),
    cluster_id BIGINT NOT NULL REFERENCES vo_clusters(id),
    zone VARCHAR(255) NOT NULL DEFAULT 'vortexops.local',
    name VARCHAR(512) NOT NULL,
    fqdn VARCHAR(1024) NOT NULL,
    record_type VARCHAR(16) NOT NULL DEFAULT 'A',
    ttl INT NOT NULL DEFAULT 30,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (group_id, fqdn)
);

CREATE INDEX IF NOT EXISTS idx_dns_records_group ON vo_dns_records(group_id);
CREATE INDEX IF NOT EXISTS idx_dns_records_fqdn ON vo_dns_records(fqdn);
CREATE INDEX IF NOT EXISTS idx_dns_records_cluster ON vo_dns_records(cluster_id);

CREATE TABLE IF NOT EXISTS vo_dns_backends (
    id BIGSERIAL PRIMARY KEY,
    record_id BIGINT NOT NULL REFERENCES vo_dns_records(id) ON DELETE CASCADE,
    pod_ip VARCHAR(64) NOT NULL,
    pod_name VARCHAR(255) NOT NULL DEFAULT '',
    healthy BOOLEAN NOT NULL DEFAULT true,
    weight INT NOT NULL DEFAULT 100,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (record_id, pod_ip)
);

CREATE INDEX IF NOT EXISTS idx_dns_backends_record ON vo_dns_backends(record_id);
CREATE INDEX IF NOT EXISTS idx_dns_backends_healthy ON vo_dns_backends(record_id, healthy);
