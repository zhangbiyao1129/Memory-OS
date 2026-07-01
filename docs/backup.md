# Memory OS 备份与恢复手册

Memory OS 的 T480 单机部署必须每天备份三类权威数据：PostgreSQL 元数据、Markdown Archive 文件、Qdrant 向量快照。默认保留 30 天。

## 备份

```bash
make backup
```

默认输出目录：`backups/<UTC_RUN_ID>/`。

目录内容：

```text
backups/<run_id>/
  manifest.json
  postgres/
    pg_dump.command
    memory_os.sql
  archives/
    markdown-archive.tar.gz
  qdrant/
    snapshot.command
    snapshot-response.json
    <qdrant-snapshot-file>
```

可配置环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `BACKUP_ROOT` | `./backups` | 备份输出目录 |
| `ARCHIVE_DIR` | `./archives` | Markdown Archive 根目录 |
| `RETENTION_DAYS` | `30` | 本地保留天数 |
| `POSTGRES_DB` | `memory_os` | PostgreSQL 数据库名 |
| `POSTGRES_USER` | `memory_os` | PostgreSQL 用户 |
| `QDRANT_URL` | `http://localhost:18083` | Qdrant HTTP 地址 |
| `QDRANT_COLLECTION` | `memory_os` | Qdrant collection |
| `DRY_RUN` | `0` | 设置为 `1` 时只生成可审计占位与命令记录 |

安装每日备份 cron dry-run：

```bash
PROJECT_DIR=/opt/memory-os make install-backup-cron
```

真实安装必须显式确认：

```bash
DRY_RUN=0 CONFIRM_CRON_INSTALL=I_UNDERSTAND PROJECT_DIR=/opt/memory-os make install-backup-cron
```

## 恢复 dry-run

恢复入口默认只做 dry-run，不覆盖任何数据：

```bash
BACKUP_DIR=backups/<run_id> make restore
```

它会生成审计命令文件：

```text
artifacts/restore-<timestamp>/
  postgres.restore.command
  archives.restore.command
  qdrant.restore.command
```

## 真实恢复

恢复前先停止写入流量，避免恢复期间产生新的 archive、索引或记忆写入。真实恢复必须显式确认：

```bash
DRY_RUN=0 CONFIRM_RESTORE=I_UNDERSTAND BACKUP_DIR=backups/<run_id> make restore
```

恢复脚本会依次执行：

1. PostgreSQL `psql` 导入。
2. Markdown Archive 解包。
3. Qdrant snapshot upload。
4. `make smoke` 恢复后烟测。

## 安全约束

- 备份目录默认 `umask 077`，避免其他用户读取。
- `manifest.json` 只记录路径、collection、数据库名，不写入密码或 API key。
- `make restore` 默认 dry-run，真实恢复必须设置 `CONFIRM_RESTORE=I_UNDERSTAND`。
- 不把 `backups/` 和 `artifacts/` 提交到 Git。
