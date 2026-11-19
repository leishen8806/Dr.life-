# backend/cmd

存放 Go 可执行程序入口。
服务入口按模块化单体方式启动后端服务，不放业务模块实现。
技术依据：`Dr.life+第一版交付形态与技术架构.md` 第三、六章。

## `admininit`

Owner-operated bootstrap utility for an empty IAM database. Run `super
--username <name> --email <email>` once to create the first super administrator,
or `super-email --username <name> --email <email>` to repair the legacy
`@bootstrap.invalid` address of an already-created active super administrator.
The repair command changes no other record, writes one non-sensitive audit entry,
and is safe to repeat. Or run `bot
--end buyer|merchant --username <name> --actor <super public id>` with the bot
token supplied as one line on standard input. It prints bootstrap credentials
only for the direct `super` invocation; it never accepts passwords or tokens as
command-line arguments. In production, `IAM_DATABASE_URL` or `DATABASE_URL`
must use an owner-created least-privilege login with `iam_rw` and
`audit_writer` membership; never use the migration account or a superuser.
