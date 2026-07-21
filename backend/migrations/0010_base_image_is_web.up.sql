-- 基础镜像增加 is_web 标记：Web 镜像在应用启动之外额外启动 nginx。
ALTER TABLE vo_base_images
    ADD COLUMN IF NOT EXISTS is_web BOOLEAN NOT NULL DEFAULT false;

-- 历史数据：entrypoint 含 nginx 的视为 Web 镜像。
UPDATE vo_base_images
SET is_web = true
WHERE entrypoint IS NOT NULL
  AND entrypoint::text LIKE '%nginx%';

-- 历史纯 nginx 启动命令迁移为 is_web=true + 空 entrypoint（由渲染层生成 nginx 前台命令）。
UPDATE vo_base_images
SET entrypoint = NULL
WHERE is_web = true
  AND entrypoint::text IN ('["nginx", "-g", "daemon off;"]', '["nginx","-g","daemon off;"]');
