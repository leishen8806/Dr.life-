# backend/migrations

存放 PostgreSQL SQL 迁移文件，使用 goose 管理版本。
迁移保持向前兼容：先增加结构、改代码，再删除旧结构；每次变更须可审查和回滚。
技术依据：`Dr.life+数据库表结构与账务详细设计.md` 第一、十章。
