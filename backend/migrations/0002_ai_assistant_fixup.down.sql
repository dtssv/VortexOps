-- ============================================================================
-- 0002_ai_assistant_fixup.down.sql
-- ----------------------------------------------------------------------------
-- 回滚 0002 补丁创建的 AI 助手相关表与种子数据。
-- 注意：down 会删除 vo_chat_messages / vo_chat_sessions / vo_user_profiles /
--       vo_kb_chunks，但保留 vo_kb_documents / vo_kb_categories（由 0001 创建）。
-- ============================================================================

DROP TABLE IF EXISTS vo_chat_messages CASCADE;
DROP TABLE IF EXISTS vo_chat_sessions CASCADE;
DROP TABLE IF EXISTS vo_user_profiles CASCADE;
DROP TABLE IF EXISTS vo_kb_chunks CASCADE;

DELETE FROM vo_kb_documents WHERE title IN
 ('构建失败常见原因', 'Pod 启动失败排查指南', '发布失败排查指南');
DELETE FROM vo_kb_categories WHERE code IN
 ('general', 'build', 'release', 'k8s', 'system')
 AND NOT EXISTS (SELECT 1 FROM vo_kb_documents WHERE category_id = vo_kb_categories.id);

DELETE FROM vo_permissions WHERE code IN ('kb:view', 'kb:manage');
DELETE FROM vo_system_settings WHERE key LIKE 'ai.embedding.%';

-- 不主动 DROP vector 扩展：它可能被其它对象引用，且不影响后续重建。
