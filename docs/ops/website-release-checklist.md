# Website release checklist

This is the release note template required by DISPATCH 七-C/七-E. Fill in the
run numbers and links for the specific PR; do not paste credentials, hostnames,
tokens or deployment secret values.

## 发布单

- 改动与对应 Issue / PR：
- 验收项与测试命令：
- 设计稿或 source.html 对照：
- CI run、截图 artifact、证据分支：
- 风险点与未执行事项：
- 回滚点：上一次正常 Website Deploy production run：

## 自动门槛

- Website build/typecheck/test、gitleaks、ci-gate：PASS / FAIL / NOT_RUN
- 截图矩阵与 h1/溢出断言：PASS / FAIL / NOT_RUN
- Lighthouse：手机性能 >= 80、可访问性 >= 90、LCP <= 2.5s：PASS / FAIL / NOT_RUN
- 内链与锚点：PASS / FAIL / NOT_RUN
- claude-review 当前 head：PASS / FAIL / NOT_RUN

## 发布后

- `Website smoke and Lighthouse` run：
- HTTP 200、关键标题、privacy/terms/404：PASS / FAIL / NOT_RUN
- 390px 无水平溢出与首页截图：PASS / FAIL / NOT_RUN
- 失败时回滚与负责人通知记录：
