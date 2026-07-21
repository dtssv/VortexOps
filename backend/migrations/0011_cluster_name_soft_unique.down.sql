DROP INDEX IF EXISTS uk_clusters_name;
ALTER TABLE vo_clusters ADD CONSTRAINT vo_clusters_name_key UNIQUE (name);
