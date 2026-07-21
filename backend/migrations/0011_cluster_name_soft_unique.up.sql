-- 集群名称唯一性仅约束未删除记录，允许软删除后复用同名重新接入。
ALTER TABLE vo_clusters DROP CONSTRAINT IF EXISTS vo_clusters_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS uk_clusters_name ON vo_clusters (name) WHERE deleted = false;
