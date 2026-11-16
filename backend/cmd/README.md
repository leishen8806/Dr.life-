# backend/cmd

存放 Go 可执行程序入口。
服务入口按模块化单体方式启动后端服务，不放业务模块实现。
技术依据：`Dr.life+第一版交付形态与技术架构.md` 第三、六章。

## `admininit`

Owner-operated bootstrap utility for an empty IAM database. Run `super
--username <name>` once to create the first super administrator, or `bot
--end buyer|merchant --username <name> --actor <super public id>` with the bot
token supplied as one line on standard input. It prints bootstrap credentials
only for the direct `super` invocation; it never accepts passwords or tokens as
command-line arguments.
