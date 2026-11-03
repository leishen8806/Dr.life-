# P0-3：OpenAI 兼容客户端与回退 PoC 结论

## 结论

用一个标准库 Go 客户端即可调用 DeepSeek、ChatGPT 和 Ollama 风格的
OpenAI 兼容端点；供应商差异仅来自 `Endpoint{Name, BaseURL, Model}` 配置。
每次请求使用 `POST /v1/chat/completions`，按配置顺序尝试。超时或非 2xx
响应会切换到下一个端点；全部失败时调用本地规则函数。

客户端接口形状：

```go
client := aiclient.New(aiclient.Config{
    Endpoints: []aiclient.Endpoint{{Name: "Ollama", BaseURL: "http://...", Model: "..."}},
    Timeout: 2 * time.Second,
    MaxRetries: 1,
    RetryDelay: 50 * time.Millisecond,
    Fallback: func(request aiclient.Request) aiclient.Result { /* local rule */ },
})
result, err := client.Complete(ctx, aiclient.Request{Messages: messages, Idempotent: true})
```

默认超时为 2 秒、默认退避为 50 ms；`MaxRetries` 的零值为不重试。
只有调用者声明 `Idempotent: true` 才会重试并继续供应商回退。非幂等请求在
首次失败后直接走本地规则，避免重复提交。

## 验收对照

| 原验收 | 本地 PoC 证据 | 结论 |
|---|---|---|
| ① 同一客户端调用三种接口 | `TestClientUsesOneOpenAICompatibleProtocolForConfiguredEndpoints` | 通过（3 个本地假服务器） |
| ② Ollama 无响应后换下一个 | `TestClientTimesOutThenUsesNextEndpoint` | 通过（30 ms 超时后切换） |
| ③ 全部失败回退本地规则 | `TestClientFallsBackToLocalRuleAfterAllEndpointsFail` | 通过 |
| ④ 30 句真实高棉语评测 | 不在本卡收窄范围 | 未验证，需真实账号与 Ollama 后另卡执行 |

## 未验证项

- 真实 DeepSeek / ChatGPT 的延迟、配额和认证行为；
- 内网 Ollama 的可达性与断线恢复；
- 高棉语回答质量及三者优先级；
- 生产网关的密钥加密、脱敏、预算、日志和队列集成。

本目录不含任何真实端点、密钥或供应商调用。

## 验证命令

仓库根没有顶层 Go 模块，且本卡明确禁止修改 `go.mod` / `go.sum`。因此执行：

```powershell
$env:GO111MODULE='off'
C:\Go\bin\go.exe test -v ./poc/ai-client
C:\Go\bin\go.exe build ./poc/ai-client
```

根目录的 `go build ./...` 和 `go test ./poc/...` 在当前模块布局下无法作为本卡
验证命令；它们需要顶层工作区或枚举各个独立 PoC 模块，均超出本卡允许范围。
