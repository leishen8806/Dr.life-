# Ledger 第十一章测试矩阵

`TestChapter11Matrix` 在 CI 中验证每个编号：必须引用存在的测试函数，或标为
`BLOCKED(任务编号)`。金额断言均为 `int64` 最小货币单位。

| 编号 | 第十一章类别 | 自动化测试 / 期望断言 |
| --- | --- | --- |
| C11-01 | 平衡 | `TestBusinessPostingIntegration`、`TestBusinessRandomSequenceMaintainsInvariant`：每种业务动作与随机序列借贷平衡、恒等式成立。 |
| C11-02 | 幂等 | `TestPostIntegration`、`TestHoldIntegration`、`TestWithdrawalConcurrencyIntegration`、`TestReverseConcurrencyIntegration`、`TestAdjustIntegration`：同一幂等键只入账一次。 |
| C11-03 | 并发 | `TestPostIntegration`、`TestHoldIntegration`、`TestWithdrawalConcurrencyIntegration`、`TestReverseConcurrencyIntegration`：不超扣、不重复扣款且交叉账户无死锁。 |
| C11-04 | 分账 | `TestAllocateBoundaries`、`TestAllocateProperty`、`TestAllocateRejectsInvalidShares`：三方分账、奇数和舍入边界之和恒等。 |
| C11-05 | 不可变 | `TestLedgerMigrationAndConstraints`、`TestPostIntegration`、`TestHoldIntegration`、`TestSnapshotDailyImmutableAndDetectsDrift`、`TestDatabaseFramework`：ledger 流水不可改删。 |
| C11-05a | 审计日志不可变 | **BLOCKED(Q-01)**：`audit.log` 的 UPDATE/DELETE 必须失败，哈希链连续；账务模块不跨 schema 写入审计表。 |
| C11-06 | 冲销 | `TestReverseIntegration`、`TestReverseConcurrencyIntegration`、`TestReverseRandomSequenceIntegration`：原值恢复、仅一次冲销、链路可追溯。 |
| C11-07 | 提现 | `TestWithdrawalWorkflowIntegration`、`TestWithdrawalConcurrencyIntegration`：职责分离、失败回滚、24 小时超时检测。 |
| C11-08 | 对账 | **BLOCKED(L-08)**：银行文件匹配、未匹配、重复导入；差异必须入 `SUSPENSE` 并告警。 |
| C11-09 | 权限 | `TestLedgerMigrationAndConstraints`、`TestPostIntegration`：非 ledger 数据库角色不能读写 ledger 表。 |
| C11-10a | 灾备恢复 | `TestChapter11BackupRestoreIntegration`：`pg_dump` / `pg_restore` 到临时数据库后，`RunDailyReconciliation` 无差异且余额与分录重算一致。 |
| C11-10b | 灾备流程 | **BLOCKED(Q-03)**：按已定义备份与恢复流程执行演练，并验证恢复产物。 |

## 阻塞清单

- `C11-08` — **BLOCKED(L-08)**：等待银行对账文件导入器及匹配工作流。
- `C11-05a` — **BLOCKED(Q-01)**：等待审计日志（哈希链）模块及其 `audit.log` 不可变性断言。
- `C11-10b` — **BLOCKED(Q-03)**：等待备份恢复流程、保留策略和演练入口。
