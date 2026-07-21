-- 0005_node_pod_metrics.down.sql

DROP TABLE IF EXISTS vo_cluster_pod_metric_sample CASCADE;
DROP TABLE IF EXISTS vo_cluster_node_metric_sample CASCADE;

SELECT '0005_node_pod_metrics rolled back' AS status;
