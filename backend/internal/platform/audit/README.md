# 审计哈希链

`Store.Append` 在同一事务内通过 `audit.lock_chain_head()` 锁住唯一链头、写入规范化且递归脱敏的 JSON，再调用受限函数推进链头。应用连接使用 `AUDIT_DATABASE_URL` 对应的 `audit_writer` 角色；该角色只有 `SELECT`、`INSERT` 和两项受限函数的执行权。

```sh
go run ./cmd/auditctl verify
go run ./cmd/auditctl verify --from 100 --to 200
go run ./cmd/auditctl anchor --day 2026-10-25 --file ./var/audit-anchors.jsonl
```

锚定日按 UTC 计算。本地文件接收器只用于开发与验证；异地不可变对象存储的地址与实现待运维任务确认。审计日志默认永久保留。

PostgreSQL 会在触发器之前检查表权限。因此管理员的 UPDATE/DELETE/TRUNCATE 会被不可变触发器以 SQLSTATE `55000` 拒绝，而只有 SELECT/INSERT 的 `audit_writer` 会更早以 `42501` 拒绝。不能为了统一错误码而向应用角色开放修改权限。
