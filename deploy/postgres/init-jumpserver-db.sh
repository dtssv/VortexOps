#!/bin/bash
set -e

# JumpServer 需要独立数据库；superuser vortexops 由 POSTGRES_USER 创建并具备建库权限。
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
    CREATE DATABASE jumpserver;
    GRANT ALL PRIVILEGES ON DATABASE jumpserver TO $POSTGRES_USER;
EOSQL
