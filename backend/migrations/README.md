# backend/migrations

存放 PostgreSQL SQL 迁移文件，使用 goose 管理版本。

- 文件名使用递增序号 `00001_xxx.sql`；发生序号冲突时，后合并者重新编号。
- 每个迁移必须同时提供可执行的 Up 和 Down。
- 迁移保持向前兼容：先增加结构、改代码，再删除旧结构。
- 账务表结构变更必须由两人评审。
- 生产迁移不加载演示数据；演示数据只能通过真实业务流程生成。

`pg_trgm` 安装在 `public`，沿用 PostgreSQL 扩展的标准可见位置；应用角色没有 `public` 的建表权限。

## 端口冲突怎么办

开发环境默认把 PostgreSQL 绑定到 `127.0.0.1:55432`，Redis 绑定到 `127.0.0.1:56379`；容器内仍使用标准端口 5432 和 6379。需要更换宿主机端口时，在 `backend/.env` 设置 `POSTGRES_HOST_PORT`、`REDIS_HOST_PORT`，并同步修改同一文件里的数据库和 Redis URL。所有 `db-*` 命令与 `db-bootstrap` 都读取这一个环境文件。

Windows 可用 `netstat -ano | findstr :55432`（Redis 对应 `:56379`）查看占用端口的进程编号。

技术依据：`Dr.life+数据库表结构与账务详细设计.md` 第一、十章。
