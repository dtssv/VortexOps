-- 0001_init_schema.down.sql
-- 回滚初始 schema：删除全部表、视图、函数与扩展。
-- 注意：生产环境回滚需谨慎，会丢失全部数据。
--
-- 说明：schema 中存在循环外键（如 vo_inference_services ↔ vo_inference_releases），
-- 无法用纯线性依赖逆序删除。这里对所有表使用 CASCADE，由 PostgreSQL 递归删除依赖对象
-- （约束/触发器/分区等），保证回滚幂等且无顺序耦合。

DROP VIEW IF EXISTS vo_v_workspace_overview;
DROP VIEW IF EXISTS vo_v_group_detail;

-- 角色相关（含角色-菜单直接绑定）
DROP TABLE IF EXISTS vo_role_menus CASCADE;

-- 集群运维相关
DROP TABLE IF EXISTS vo_cluster_node_status CASCADE;
DROP TABLE IF EXISTS vo_cluster_operations CASCADE;

-- 构建工具配置
DROP TABLE IF EXISTS vo_build_tools CASCADE;

DROP TABLE IF EXISTS vo_config_snapshots CASCADE;
DROP TABLE IF EXISTS vo_behavior_audit_logs CASCADE;
DROP TABLE IF EXISTS vo_ops_sessions CASCADE;
DROP TABLE IF EXISTS vo_bastion_sessions CASCADE;
DROP TABLE IF EXISTS vo_bastion_assets CASCADE;
DROP TABLE IF EXISTS vo_audit_logs CASCADE;
DROP TABLE IF EXISTS vo_alert_events CASCADE;
DROP TABLE IF EXISTS vo_alert_rules CASCADE;
DROP TABLE IF EXISTS vo_notification_channels CASCADE;
DROP TABLE IF EXISTS vo_notifications CASCADE;
DROP TABLE IF EXISTS vo_notification_templates CASCADE;
DROP TABLE IF EXISTS vo_approvals CASCADE;

DROP TABLE IF EXISTS vo_inference_routes CASCADE;
DROP TABLE IF EXISTS vo_inference_usage CASCADE;
DROP TABLE IF EXISTS vo_inference_api_keys CASCADE;
DROP TABLE IF EXISTS vo_inference_services CASCADE;
DROP TABLE IF EXISTS vo_inference_releases CASCADE;
DROP TABLE IF EXISTS vo_model_adapters CASCADE;
DROP TABLE IF EXISTS vo_model_versions CASCADE;
DROP TABLE IF EXISTS vo_models CASCADE;
DROP TABLE IF EXISTS vo_model_registries CASCADE;

DROP TABLE IF EXISTS vo_middleware_operations CASCADE;
DROP TABLE IF EXISTS vo_middleware_instances CASCADE;
DROP TABLE IF EXISTS vo_middleware_catalog CASCADE;

DROP TABLE IF EXISTS vo_group_local_configs CASCADE;
DROP TABLE IF EXISTS vo_group_config_bindings CASCADE;
DROP TABLE IF EXISTS vo_config_sets CASCADE;
DROP TABLE IF EXISTS vo_configs CASCADE;

DROP TABLE IF EXISTS vo_release_windows CASCADE;
DROP TABLE IF EXISTS vo_release_orchestration_targets CASCADE;
DROP TABLE IF EXISTS vo_release_orchestrations CASCADE;
DROP TABLE IF EXISTS vo_release_presets CASCADE;
DROP TABLE IF EXISTS vo_release_batch_records CASCADE;
DROP TABLE IF EXISTS vo_release_events CASCADE;
DROP TABLE IF EXISTS vo_releases CASCADE;

DROP TABLE IF EXISTS vo_artifacts_signatures CASCADE;
DROP TABLE IF EXISTS vo_promotions CASCADE;
DROP TABLE IF EXISTS vo_pipeline_stage_runs CASCADE;
DROP TABLE IF EXISTS vo_pipeline_runs CASCADE;
DROP TABLE IF EXISTS vo_pipeline_stages CASCADE;
DROP TABLE IF EXISTS vo_pipelines CASCADE;

DROP TABLE IF EXISTS vo_build_steps CASCADE;
DROP TABLE IF EXISTS vo_builds CASCADE;
DROP TABLE IF EXISTS vo_image_version_tags CASCADE;
DROP TABLE IF EXISTS vo_images CASCADE;
DROP TABLE IF EXISTS vo_build_templates CASCADE;
DROP TABLE IF EXISTS vo_base_images CASCADE;

DROP TABLE IF EXISTS vo_groups CASCADE;
DROP TABLE IF EXISTS vo_git_sources CASCADE;
DROP TABLE IF EXISTS vo_application_members CASCADE;
DROP TABLE IF EXISTS vo_applications CASCADE;
DROP TABLE IF EXISTS vo_workspace_quotas CASCADE;
DROP TABLE IF EXISTS vo_workspace_clusters CASCADE;
DROP TABLE IF EXISTS vo_workspaces CASCADE;

DROP TABLE IF EXISTS vo_resource_templates CASCADE;
DROP TABLE IF EXISTS vo_cluster_ip_allocations CASCADE;
DROP TABLE IF EXISTS vo_cluster_ip_pools CASCADE;
DROP TABLE IF EXISTS vo_cluster_node_pools CASCADE;
DROP TABLE IF EXISTS vo_node_pools CASCADE;
DROP TABLE IF EXISTS vo_credentials CASCADE;
DROP TABLE IF EXISTS vo_jenkins_instances CASCADE;
DROP TABLE IF EXISTS vo_registries CASCADE;
DROP TABLE IF EXISTS vo_clusters CASCADE;

DROP TABLE IF EXISTS vo_workspace_members CASCADE;
DROP TABLE IF EXISTS vo_platform_role_bindings CASCADE;
DROP TABLE IF EXISTS vo_role_permissions CASCADE;
DROP TABLE IF EXISTS vo_roles CASCADE;
DROP TABLE IF EXISTS vo_menus CASCADE;
DROP TABLE IF EXISTS vo_permissions CASCADE;

DROP TABLE IF EXISTS vo_workspace_creation_policies CASCADE;
DROP TABLE IF EXISTS vo_external_api_call_logs CASCADE;

-- AI 助手（知识库 RAG / 用户画像 / 对话会话）
DROP TABLE IF EXISTS vo_chat_messages CASCADE;
DROP TABLE IF EXISTS vo_chat_sessions CASCADE;
DROP TABLE IF EXISTS vo_user_profiles CASCADE;
DROP TABLE IF EXISTS vo_kb_chunks CASCADE;
DROP TABLE IF EXISTS vo_kb_documents CASCADE;
DROP TABLE IF EXISTS vo_kb_categories CASCADE;

DROP TABLE IF EXISTS vo_system_settings CASCADE;
DROP TABLE IF EXISTS vo_sys_dictionaries CASCADE;
DROP TABLE IF EXISTS vo_user_preferences CASCADE;
DROP TABLE IF EXISTS vo_api_tokens CASCADE;
DROP TABLE IF EXISTS vo_refresh_tokens CASCADE;
DROP TABLE IF EXISTS vo_users CASCADE;

-- 触发器依附于表，表删除后触发器自动消失，此时函数不再被依赖，可安全删除。
DROP FUNCTION IF EXISTS set_updated_at();

DROP EXTENSION IF EXISTS "vector";
DROP EXTENSION IF EXISTS "uuid-ossp";
DROP EXTENSION IF EXISTS "pg_trgm";
DROP EXTENSION IF EXISTS "pgcrypto";
