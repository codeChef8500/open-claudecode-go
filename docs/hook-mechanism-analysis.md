# OpenCode vs Claude Code Hook 机制深度分析

## 一、概述

本文档从理论架构和技术实现两个维度，对比分析以下三个系统的 Hook 机制：

1. **Claude Code** (Anthropic) - 官方实现，TypeScript
2. **OpenCode** (anomalyco) - 开源实现，TypeScript/Bun  
3. **open-claudecode-go** (本项目) - Go 实现

---

## 二、理论架构对比

### 2.1 设计哲学

| 维度 | Claude Code | OpenCode | open-claudecode-go |
|------|-------------|----------|-------------------|
| **架构模式** | 配置驱动 (settings.json) | 插件驱动 (Plugin 接口) | 配置驱动 + 插件集成 |
| **扩展方式** | 用户配置文件 + 插件 | 插件系统 (npm/file) | 配置文件 + 插件函数注册 |
| **核心原则** | 确定性控制，声明式配置 | 统一合约，运行时加载 | 兼容 Claude Code 协议 |

### 2.2 Hook 类型体系

#### Claude Code (4 种 Hook 类型)

```
┌─────────────────────────────────────────────────────────────┐
│                    Claude Code Hook Types                   │
├─────────────────────────────────────────────────────────────┤
│ 1. command  - Shell 命令执行 (stdin/stdout/stderr + exit)   │
│ 2. http     - HTTP POST 请求                                │
│ 3. prompt   - 单轮 LLM 决策 (Haiku 默认)                    │
│ 4. agent    - 多轮子 Agent 验证 (60s, 50 turns)             │
└─────────────────────────────────────────────────────────────┘
```

#### OpenCode (2 种插件类型)

```
┌─────────────────────────────────────────────────────────────┐
│                     OpenCode 插件类型                        │
├─────────────────────────────────────────────────────────────┤
│ 1. Server Plugin (Hook-based)                               │
│    - 运行在后端进程                                         │
│    - 通过 Hooks 接口注册扩展点                              │
│    - 参与会话生命周期                                       │
│                                                             │
│ 2. TUI Plugin (Slot-based)                                  │
│    - 自定义终端 UI                                          │
│    - 注册 SolidJS 组件到命名槽位                            │
│    - 添加命令和自定义路由                                   │
└─────────────────────────────────────────────────────────────┘
```

#### open-claudecode-go (3 种 Hook 执行方式)

```
┌─────────────────────────────────────────────────────────────┐
│                open-claudecode-go Hook 类型                 │
├─────────────────────────────────────────────────────────────┤
│ 1. Command Hook  - Shell 命令执行 (对齐 Claude Code)        │
│ 2. HTTP Hook     - HTTP 请求 (带 SSRF 防护)                 │
│ 3. Prompt Hook   - LLM 评估 (PromptEvaluator 接口)          │
│ 4. Session Hook  - 会话级临时 Hook (运行时注册/清除)        │
└─────────────────────────────────────────────────────────────┘
```

### 2.3 事件体系对比

#### Claude Code (25+ 事件)

```
会话生命周期:
├── SessionStart      - 会话开始/恢复
├── SessionEnd        - 会话终止
├── UserPromptSubmit  - 用户提交提示
├── Stop              - 响应完成
├── StopFailure       - API 错误终止

工具生命周期:
├── PreToolUse           - 工具执行前 (可阻塞)
├── PostToolUse          - 工具执行后
├── PostToolUseFailure   - 工具执行失败
├── PermissionRequest    - 权限请求
├── PermissionDenied     - 权限拒绝

子 Agent:
├── SubagentStart    - 子 Agent 启动
├── SubagentStop     - 子 Agent 停止

任务管理:
├── TaskCreated      - 任务创建
├── TaskCompleted    - 任务完成
├── TeammateIdle     - 队友空闲

上下文管理:
├── PreCompact       - 压缩前
├── PostCompact      - 压缩后
├── InstructionsLoaded - CLAUDE.md 加载

系统事件:
├── Notification     - 通知
├── ConfigChange     - 配置变更
├── CwdChanged       - 目录变更
├── FileChanged      - 文件变更
├── WorktreeCreate   - 工作树创建
├── WorktreeRemove   - 工作树移除
├── Elicitation      - MCP 请求输入
└── ElicitationResult - MCP 输入结果
```

#### OpenCode (14 个扩展点)

```
通知类 (Event):
├── event           - 接收所有 Bus 事件

配置类:
└── config          - 配置加载后调用

Chat 生命周期 (Trigger):
├── chat.message              - 拦截用户消息
├── chat.params               - 修改 LLM 参数
├── chat.headers              - 注入 HTTP 头
├── experimental.chat.messages.transform
├── experimental.chat.system.transform
└── experimental.text.complete

压缩 (Trigger):
└── experimental.session.compacting

权限 (Trigger):
└── permission.ask

工具 (Trigger):
├── tool              - 注册自定义工具
├── tool.definition   - 修改工具描述
├── tool.execute.before
└── tool.execute.after
```

#### open-claudecode-go (6 个核心事件 + 扩展)

```
核心 (对齐 Claude Code):
├── PreToolUse       - 工具执行前
├── PostToolUse      - 工具执行后
├── Stop             - 响应完成
├── UserPromptSubmit - 用户提示提交
├── SessionStart     - 会话开始
└── Notification     - 通知

扩展中:
├── StopFailure
├── PreCompact / PostCompact
├── SubagentStart / SubagentStop
└── ...
```

---

## 三、技术框架设计对比

### 3.1 核心架构模式

#### Claude Code: 配置驱动的命令执行

```json
// settings.json 配置格式
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Edit|Write",
        "hooks": [
          {
            "type": "command",
            "command": "./check-permission.sh"
          }
        ]
      }
    ]
  }
}
```

**执行流程:**
1. 事件触发 → 加载 settings.json 中的 hook 配置
2. 匹配 matcher (工具名/正则)
3. 并行执行所有匹配的 hook
4. 收集 stdout/stderr 和退出码
5. 决策: 0=允许, 2=阻止, 其他=允许+错误

#### OpenCode: 插件系统的 Effect 调度

```typescript
// 插件导出 Hooks 接口
export type Plugin = (input: PluginInput) => Promise<Hooks>

// Trigger Hook 使用顺序变异模式
const trigger = Effect.fn("Plugin.trigger")(function* <Name extends TriggerName>(
  name: Name, input: Input, output: Output,
) {
  for (const hook of s.hooks) {
    const fn = hook[name]
    if (!fn) continue
    yield* Effect.promise(async () => fn(input, output))
  }
  return output
})
```

**执行流程:**
1. 插件加载 → 解析 package.json 和 exports
2. 插件注册 → 收集 Hooks 接口方法
3. 触发时 → 按注册顺序依次处理
4. 共享输出对象 → 后面的插件可覆盖前面的修改

#### open-claudecode-go: Go 风格的 Hook 链

```go
// Hook 类型定义
type HookType string

const (
    HookPreToolUse       HookType = "pre_tool_use"
    HookPostToolUse      HookType = "post_tool_use"
    HookStop             HookType = "stop"
    HookUserPromptSubmit HookType = "user_prompt_submit"
    HookSessionStart     HookType = "session_start"
    HookNotification     HookType = "notification"
)

// HookEngine 调度器
type HookEngine struct {
    handlers map[HookType][]HookHandler
}

func (he *HookEngine) Run(ctx context.Context, payload HookPayload) (*HookResult, error) {
    handlers := he.handlers[payload.Type]
    for _, h := range handlers {
        result, err := h(ctx, payload)
        if result != nil && result.Block {
            return result, nil  // 短路: 第一个阻止生效
        }
    }
    return nil, nil
}
```

### 3.2 数据流与输入/输出协议

#### Claude Code: JSON via stdin/stdout

**输入 (stdin):**
```json
{
  "session_id": "abc123",
  "cwd": "/Users/project",
  "hook_event_name": "PreToolUse",
  "tool_name": "Bash",
  "tool_input": { "command": "npm test" }
}
```

**输出 (stdout + exit code):**
```bash
# 方式1: 退出码
exit 0  # 允许
exit 2  # 阻止

# 方式2: JSON 输出
echo '{"hookSpecificOutput": {"permissionDecision": "deny"}}'
```

#### OpenCode: 函数参数 + 返回值

```typescript
// 触发 hook 时传递 input 对象
// 函数直接返回修改后的 output 对象

interface Hooks {
  'chat.params'?: (input: ChatParams, output: ChatParams) => ChatParams
  'tool.execute.before'?: (input: ToolInput, output: ToolInput) => ToolInput
}
```

#### open-claudecode-go: 结构化接口

```go
type HookPayload struct {
    Type      HookType
    ToolName  string
    ToolInput interface{}
    Result    string
    SessionID string
    Message   string
}

type HookResult struct {
    Modified interface{}  // 修改后的值
    Block    bool         // 是否阻止
    Reason   string       // 原因说明
}
```

### 3.3 安全机制

#### Claude Code
- 退出码 2 表示阻止
- 权限模式集成 (deny rules 优先于 hook allow)
- 环境变量隔离 (allowedEnvVars)

#### OpenCode
- 插件兼容性检查 (semver peerDependencies)
- 错误处理不中断其他插件

#### open-claudecode-go (增强)
```go
// HTTP Hook 的 SSRF 防护
func validateHookURL(rawURL string) error {
    // 阻止 localhost 变体
    // 解析 DNS 检查私有 IP
    // RFC 1918 私有地址范围
}

// ssrfSafeTransport 双重检查
type ssrfSafeTransport struct {
    base http.RoundTripper
}
```

---

## 四、关键差异总结

### 4.1 事件模型差异

| 维度 | Claude Code | OpenCode | open-claudecode-go |
|------|-------------|----------|-------------------|
| 事件数量 | 25+ | 14 | 6 (核心) + 扩展 |
| 事件粒度 | 细粒度 (含子 Agent/任务) | 中粒度 (Chat/Tool/Config) | 对齐 Claude Code |
| 触发方式 | 配置文件 + matcher | 插件接口方法 | 函数注册 + 配置 |

### 4.2 执行模型差异

| 维度 | Claude Code | OpenCode | open-claudecode-go |
|------|-------------|----------|-------------------|
| 并行性 | 并行执行，自动去重 | 顺序执行 | 顺序执行 (可短路) |
| 决策聚合 | 最严格优先 | 最后一个覆盖 | 第一个 Block 短路 |
| 超时控制 | 10 分钟默认 | 插件内管理 | 10s/可配置 |

### 4.3 扩展性差异

| 维度 | Claude Code | OpenCode | open-claudecode-go |
|------|-------------|----------|-------------------|
| 自定义工具 | 不支持 | 通过 tool hook | 规划中 |
| LLM 评估 | prompt/agent hook | 插件内处理 | prompt hook |
| HTTP 集成 | http hook | 插件自行实现 | http hook (带 SSRF) |

---

## 五、技术选型建议

### 5.1 各自优势场景

**Claude Code:**
- 成熟稳定，文档完善
- 适合需要精细化控制的场景
- 企业级配置管理

**OpenCode:**
- 插件系统更灵活
- 适合需要深度定制的场景
- 支持 TUI 自定义

**open-claudecode-go:**
- Go 生态集成
- 性能敏感场景
- 与现有 Go 项目集成

### 5.2 本项目定位

`open-claudecode-go` 的 Hook 系统定位为:
1. **协议兼容** - 完全对齐 Claude Code 的配置格式和协议
2. **增强安全** - HTTP Hook 带 SSRF 防护
3. **Go 风格** - 使用接口和结构体而非 JSON 配置
4. **可扩展** - 支持 Plugin 函数注册和 Manifest 配置

---

## 六、文件结构参考

```
internal/hooks/
├── http_hook.go      # HTTP Hook 执行器 + SSRF 防护
├── prompt_hook.go    # Prompt Hook (LLM 评估)
├── session_hooks.go  # 会话级临时 Hook

internal/plugin/
├── hooks.go          # HookEngine 核心实现
├── hooks_integration.go  # 插件 Hook 注册

internal/engine/
├── toolhooks.go      # Pre/Post Tool Hook 链
├── stophooks.go      # Stop Hook 执行器
```