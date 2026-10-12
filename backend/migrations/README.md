# backend/migrations

存放 PostgreSQL SQL 迁移文件，使用 goose 管理版本。

- 文件名使用递增序号 `00001_xxx.sql`；发生序号冲突时，后合并者重新编号。
- 每个迁移必须同时提供可执行的 Up 和 Down。
- 迁移保持向前兼容：先增加结构、改代码，再删除旧结构。
- 账务表结构变更必须由两人评审。
- 生产迁移不加载演示数据；演示数据只能通过真实业务流程生成。

`pg_trgm` 安装在 `public`，沿用 PostgreSQL 扩展的标准可见位置；应用角色没有 `public` 的建表权限。

## 固定迁移角色

所有环境中的迁移必须始终由 `MIGRATION_DATABASE_URL` 对应的同一个迁移角色执行。`ALTER DEFAULT PRIVILEGES` 只影响该角色以后创建的对象；如果改用其他角色创建表，现有业务角色不会自动获得权限，访问时会返回 `permission denied`。

## 启动服务

服务启动需要 `DATABASE_URL`，缺失时会直接退出。本地开发先把 `backend/.env.example` 复制为 `backend/.env`，在 `backend` 目录将其中变量加载到当前 shell，再执行 `make run`。例如 POSIX shell：

```sh
set -a
. ./.env
set +a
make run
```

服务直接读取环境变量，不会自行解析 `.env`；Makefile 的 `db-*` 目标会从 `backend/.env` 加载数据库相关变量。

## 端口冲突怎么办

开发环境默认把 PostgreSQL 绑定到 `127.0.0.1:55432`，Redis 绑定到 `127.0.0.1:56379`；容器内仍使用标准端口 5432 和 6379。需要更换宿主机端口时，在 `backend/.env` 设置 `POSTGRES_HOST_PORT`、`REDIS_HOST_PORT`，并同步修改同一文件里的数据库和 Redis URL。所有 `db-*` 命令与 `db-bootstrap` 都读取这一个环境文件。

Windows 可用 `netstat -ano | findstr :55432`（Redis 对应 `:56379`）查看占用端口的进程编号。

技术依据：`Dr.life+数据库表结构与账务详细设计.md` 第一、十章。
