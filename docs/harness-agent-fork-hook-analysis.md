# Harness Agent 机制深度分析：Hook 与 Fork

> 基于 Claude Code 官方文档与社区实践的技术分析报告

---

## 一、引言

在现代 AI Agent 系统设计中，**Harness（ harness）** 是连接大语言模型（LLM）与真实世界的关键基础设施。它决定了 Agent 如何执行任务、如何与外部系统交互、如何保证安全性和可控性。

本文深入分析 Claude Code 中 **Hook** 和 **Fork** 两大核心机制的架构设计、实现原理和实践方法，并对比它们与传统面向对象设计模式（继承、装饰器）的关系。

---

## 二、Hook 机制（生命周期扩展点）

### 2.1 核心概念

Hook 是 Claude Code 的"神经系统"，提供跨整个 Agent 生命周期的细粒度扩展点。它实现的是 **Observer Pattern** 和 **Chain of Responsibility Pattern** 的混合模式。

```
┌─────────────────────────────────────────────────────────────────────┐
│                      Hook Execution Flow                            │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────────┐  │
│  │  Event   │───▶│ Matcher  │───▶│   If     │───▶│    Hook      │  │
│  │  Fires   │    │  Check   │    │Condition │    │   Handler    │  │
│  └──────────┘    └──────────┘    └──────────┘    └──────────────┘  │
│       │              │              │                │              │
│       ▼              ▼              ▼                ▼              │
│  26 Lifecycle   Tool Name      Permission      Command/HTTP/       │
│  Events         Filter         Syntax          Prompt/Agent        │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### 2.2 五大执行引擎

| 类型 | 执行方式 | 持久化 | 典型用途 |
|------|----------|--------|----------|
| **Command** | Shell 命令 | ✅ JSON | lint/test 自动化 |
| **Prompt** | LLM 评判 | ✅ JSON | 风险代码判定 |
| **Agent** | 子代理多步验证 | ✅ JSON | 复杂条件检查 |
| **HTTP** | HTTP POST | ✅ JSON | 遥测上报/CI 状态 |
| **Function** | TypeScript API | ❌ 运行时 | 内部拦截 |

### 2.3 26 个生命周期事件

#### Session Level（会话级）

| 事件 | 触发时机 |
|------|----------|
| `SessionStart` | 会话开始或恢复 |
| `SessionEnd` | 会话终止 |
| `InstructionsLoaded` | CLAUDE.md 加载 |
| `ConfigChange` | 配置文件变更 |
| `CwdChanged` | 工作目录变更 |

#### Turn Level（轮次级）

| 事件 | 触发时机 |
|------|----------|
| `UserPromptSubmit` | 用户提交提示词 |
| `Stop` | 响应完成 |
| `StopFailure` | API 错误导致失败 |
| `PreCompact` | 上下文压缩前 |
| `PostCompact` | 上下文压缩后 |

#### Tool Level（工具级）— 最常用

| 事件 | 触发时机 |
|------|----------|
| `PreToolUse` | 工具执行前 ⭐ 核心拦截点 |
| `PostToolUse` | 工具执行后 |
| `PostToolUseFailure` | 工具执行失败 |
| `PermissionRequest` | 权限请求显示 |
| `PermissionDenied` | 自动模式拒绝 |

#### Subagent Level（子代理级）

| 事件 | 触发时机 |
|------|----------|
| `SubagentStart` | 子代理启动 |
| `SubagentStop` | 子代理停止 |
| `TeammateIdle` | 队友空闲 |

#### Task Level（任务级）

| 事件 | 触发时机 |
|------|----------|
| `TaskCreated` | 任务创建 |
| `TaskCompleted` | 任务完成 |

#### Async Events（异步事件）

| 事件 | 触发时机 |
|------|----------|
| `Notification` | 通知发送 |
| `FileChanged` | 文件监控触发 |
| `WorktreeCreate` | Git worktree 创建 |
| `WorktreeRemove` | Git worktree 移除 |
| `Elicitation` | MCP 请求用户输入 |

### 2.4 Matcher 与 If 条件过滤

#### Matcher 过滤器组

决定哪个 Hook 组激活：

| Matcher 语法 | 匹配方式 | 示例 |
|-------------|----------|------|
| `*` 或空 | 匹配所有 | 任何工具 |
| 纯字母/数字/`_`/`|` | 精确字符串 | `Bash` 或 `Edit\|Write` |
| 含其他字符 | 正则表达式 | `^Notebook` 或 `mcp__.*` |

#### If 条件

更精细的工具参数过滤：

```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "Bash",
      "hooks": [{
        "type": "command",
        "if": "Bash(git *)",
        "command": "./validate.sh"
      }]
    }]
  }
}
```

### 2.5 决策控制机制

Hook 通过 **Exit Code + JSON Output** 双重机制返回决策。

#### Exit Code 语义

| Exit Code | 语义 | 效果 |
|-----------|------|------|
| **0** | 成功 | 解析 stdout JSON |
| **2** | 阻塞错误 | 阻止操作，stderr 反馈给 LLM |
| **其他** | 非阻塞错误 | 显示错误但继续执行 |

#### JSON 输出协议

```json
// 通用字段
{
  "continue": false,
  "stopReason": "停止原因",
  "systemMessage": "用户警告",
  "additionalContext": "注入上下文"
}

// PreToolUse 专用 (hookSpecificOutput)
{
  "hookSpecificOutput": {
    "hookEventName": "PreToolUse",
    "permissionDecision": "allow",
    "permissionDecisionReason": "原因",
    "updatedInput": { "command": "..." },
    "additionalContext": "..."
  }
}

// 其他事件 (top-level decision)
{
  "decision": "block",
  "reason": "原因"
}
```

### 2.6 PreToolUse 深度解析

PreToolUse 是最核心的 Hook 事件，执行流程如下：

```
Claude 决定调用工具
        │
        ▼
┌──────────────────┐
│  Event Fires     │ ─── 发送 JSON 到 stdin
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  Matcher Check   │ ─── "Bash" 匹配工具名
└────────┬─────────┘
         │ (匹配)
         ▼
┌──────────────────┐
│  If Condition    │ ─── "Bash(rm *)" 匹配参数
└────────┬─────────┘
         │ (匹配)
         ▼
┌──────────────────┐
│  Handler Executes│ ─── 检查命令、返回决策
└────────┬─────────┘
         │
   ┌──────┴──────┐
   │             │
 Exit 0        Exit 2
   │             │
   ▼             ▼
 解析 JSON    阻塞工具
 执行决策      调用
```

**Decision 优先级**：`deny` > `defer` > `ask` > `allow`

### 2.7 安全模型三层防护

```
Layer 1: Global Disable
┌─────────────────────────────────────────────────────────┐
│  "disableAllHooks": true  ← 关闭所有 Hook               │
└─────────────────────────────────────────────────────────┘

Layer 2: Managed Hooks Only
┌─────────────────────────────────────────────────────────┐
│  allowManagedHooksOnly = true  ← 只运行企业级信任 Hook   │
└─────────────────────────────────────────────────────────┘

Layer 3: Workspace Trust
┌─────────────────────────────────────────────────────────┐
│  新项目 Hook 默认禁用，用户明确 "trust" 后启用           │
└─────────────────────────────────────────────────────────┘
```

### 2.8 配置位置与优先级

```
Priority 1 (Highest)
└── Managed Policy (企业管理员)

Priority 2
└── CLI --agents JSON (当前会话)

Priority 3
└── .claude/agents/ (项目级)

Priority 4
└── ~/.claude/agents/ (用户级)

Priority 5 (Lowest)
└── Plugin hooks/
```

---

## 三、Fork 机制（上下文隔离）

### 3.1 核心概念

Fork 是 Claude Code 中的上下文隔离机制，允许在子代理（sub-agent）中运行任务，而不影响主对话的上下文。它基于 **Strategy Pattern**，实现路由与执行的解耦。

```
┌─────────────────────────────────────────────────────────────────────┐
│                    AgentTool Architecture                          │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐  │
│  │                    Tool Main Entry                           │  │
│  │  - Input Schema Validation                                    │  │
│  │  - Routing Strategy (sync/async/fork)                        │  │
│  └─────────────────────────┬────────────────────────────────────┘  │
│                            │                                       │
│           ┌────────────────┼────────────────┐                     │
│           │                │                │                     │
│           ▼                ▼                ▼                     │
│  ┌────────────┐   ┌────────────┐   ┌────────────────────┐        │
│  │  Sync      │   │  Async     │   │    Fork Pattern    │        │
│  │  Runner    │   │  Runner    │   │  (Context Clone)   │        │
│  └────────────┘   └────────────┘   └────────────────────┘        │
│                                              │                    │
│                                              ▼                    │
│                                   ┌────────────────────┐          │
│                                   │  Cache-Safe        │          │
│                                   │  Message Prefix    │          │
│                                   └────────────────────┘          │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### 3.2 Fork 执行流程

```
Parent Agent
       │
       │ (N tokens in context)
       ▼
┌─────────────────────────────────────────┐
│  Agent Tool Called with fork: true      │
└─────────────────┬───────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────┐
│  Fork Pattern Module                    │
│  - Extract message prefix               │
│  - Ensure byte-level alignment          │
└─────────────────┬───────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────┐
│  Sub-agent Spawned                      │
│  - Fresh context window                 │
│  - Inherits parent prefix (cache hit!)  │
│  - Custom system prompt                 │
│  - Tool restrictions                    │
└─────────────────┬───────────────────────┘
                  │
         ┌──────────┴──────────┐
         │                     │
    Isolation Mode         Normal Mode
         │                     │
         ▼                     ▼
   - No env vars           - Inherits env
   - Restricted dir        - Full access
   - omitClaude.md         - CLAUDE.md loaded
         │                     │
         └──────────┬──────────┘
                    │
                    ▼
           Only result returns
           to parent context
```

### 3.3 字节级缓存共享（Byte-Level Cache Sharing）

这是 Fork 机制的性能核心。子代理的初始提示与父代理的历史消息在字节级精确匹配，触发 LLM 提供商的 Prompt Caching。

```typescript
// 伪代码：Fork Pattern 实现原理
function forkAgent(parentContext, subAgentConfig) {
  // 1. 获取父代理的当前消息前缀
  const parentPrefix = parentContext.getMessagePrefix();
  
  // 2. 计算精确的字节边界
  const byteBoundary = calculateByteBoundary(parentPrefix);
  
  // 3. 构建子代理的初始消息
  //    确保与父代理的前缀字节级相同
  const subAgentInitialMessages = [
    ...parentPrefix.slice(0, byteBoundary),
    // 添加子代理特定指令
    subAgentConfig.systemPrompt
  ];
  
  // 4. 当 LLM 收到请求时：
  //    - 检测到与之前相同的提示词前缀
  //    - 触发 Prompt Caching
  //    - 只处理新增的子代理指令
  
  return subAgentInitialMessages;
}
```

**效果**：
- 父代理：100,000 tokens
- Fork 后子代理：只需处理新增的 ~1,000 tokens
- 缓存命中 = 减少延迟 + 降低费用

### 3.4 四大内置代理类型

| Agent | Model | Tools | Philosophy |
|-------|-------|-------|------------|
| **Explore** | Haiku | Read-only | 快速代码探索 |
| **Plan** | Inherit | Read-only | 计划模式研究 |
| **Verification** | Inherit | Full | 对抗性测试 |
| **General-Purpose** | Inherit | Full | 通用任务 |

#### Explore 代理
- **Model**: Haiku（快速、低延迟）
- **Tools**: Read-only（禁止 Write/Edit）
- **用途**: 文件发现、代码搜索、代码库探索

#### Plan 代理
- **Model**: 继承主对话
- **Tools**: Read-only
- **用途**: 计划模式下的代码库研究

#### General-Purpose 代理
- **Model**: 继承主对话
- **Tools**: 所有工具
- **用途**: 复杂研究、多步操作、代码修改

#### Verification 代理（对抗性设计）
- 设计为**对抗性**角色
- 尝试找到解决方案的边界情况和失败点
- 使用 `run_terminal_command` 执行测试

### 3.5 隔离模式（Isolation Mode）

```yaml
---
name: safe-executor
description: 安全执行器
isolation: worktree   # 使用独立的 git worktree
---

# 子代理获得仓库的独立副本
# 所有文件修改在独立的 worktree 中
# 完成后自动清理（如无修改）
```

**可选值**：
- `worktree`: 使用独立 git worktree
- 未设置: 共享主对话环境

### 3.6 递归保护机制

```typescript
// 伪代码：递归保护实现
interface AgentDeps {
  depth: number;           // 当前递归深度
  agentTypeStack: string[]; // 代理类型栈
}

const MAX_DEPTH = 5;

function spawnSubAgent(config: AgentConfig, deps: AgentDeps) {
  // 1. 深度检查
  if (deps.depth >= MAX_DEPTH) {
    throw new Error("Max subagent depth exceeded");
  }
  
  // 2. 循环检测
  const lastAgentType = deps.agentTypeStack[deps.agentTypeStack.length - 1];
  if (lastAgentType === config.agentType) {
    console.warn(`Potential loop detected: ${config.agentType}`);
  }
  
  // 3. 更新深度计数器
  const newDeps = {
    ...deps,
    depth: deps.depth + 1,
    agentTypeStack: [...deps.agentTypeStack, config.agentType]
  };
  
  return executeAgent(config, newDeps);
}
```

### 3.7 持久化能力：Memory

子代理可以拥有持久化记忆：

```yaml
---
name: code-reviewer
description: 代码审查专家
memory: user    # 跨会话积累知识
---

# 可选值：
# - user:    ~/.claude/agent-memory/<name>/     (跨项目)
# - project: .claude/agent-memory/<name>/       (项目级，可版本控制)
# - local:   .claude/agent-memory-local/<name>/ (项目级，不入版本控制)
```

**Memory 机制**：
- 存储位置：`~/.claude/agent-memory/<agent-name>/`
- 内容：`MEMORY.md`（自动管理前 200 行/25KB）
- 用途：记录代码模式、架构决策、常见问题

### 3.8 Skills 预加载

```yaml
---
name: api-developer
description: API 开发专家
skills:
  - api-conventions
  - error-handling-patterns
---

# 子代理启动时，skills 内容被完整注入上下文
# 不只是可用，而是完整内容注入
# 子代理不继承父对话的 skills，必须显式列出
```

---

## 四、Hook vs Fork 对比深化

### 4.1 核心差异

| 维度 | Hook | Fork |
|------|------|------|
| **本质** | 生命周期拦截 | 任务执行隔离 |
| **粒度** | 单个事件/工具 | 整个会话/任务 |
| **执行方式** | 同步回调 | 异步子进程 |
| **上下文** | 共享主上下文 | 独立上下文窗口 |
| **触发** | 事件驱动 | 显式调用 |
| **返回值** | 决策 + 上下文 | 执行结果 |
| **阻塞能力** | 可阻止操作 | 无法阻止 |

### 4.2 组合使用

Hook 和 Fork 可以组合实现复杂工作流：

```yaml
# Skill with Fork + Hook
---
name: secure-review
description: 安全代码审查
context: fork         # Fork 到子代理
agent: Security-Review
hooks:
  PreToolUse:
    - matcher: "Bash"
      hooks:
        - type: command
          command: "./validate-command.sh"
---

# 子代理在隔离环境中运行
# 每个 Bash 命令前都经过安全验证
```

### 4.3 决策控制对比

| 事件类型 | Hook 可用决策 | Fork 上下文继承 |
|----------|--------------|-----------------|
| PreToolUse | allow/deny/ask/defer | N/A |
| PostToolUse | block + context | N/A |
| SubagentStart | inject context | 可注入 |
| SubagentStop | block | 返回结果 |

---

## 五、与传统设计模式的对比

### 5.1 类继承 vs Fork

```
传统继承:
BaseAgent
    └── CodingAgent (extends)
            └── SecurityAgent (extends)
```

| 特性 | 类继承 | Fork |
|------|--------|------|
| **关系** | "Is-a" 静态编译时 | "Run-in" 运行时动态 |
| **扩展方式** | 继承父类全部行为 | 选择性继承上下文 |
| **上下文** | 共享父类全部状态 | 可选择性隔离 |
| **灵活性** | 编译时固定 | 运行时可配置 |
| **复用** | 类级别 | 任务级别 |

**核心区别**：
- 传统继承："我是什么"
- Fork："我在哪里执行"

### 5.2 装饰器 vs Hook

```
传统装饰器:
Agent
    └── LoggingDecorator(Agent)
            └── CachingDecorator(Agent)
                    └── RetryDecorator(Agent)
```

| 特性 | 装饰器模式 | Hook |
|------|------------|------|
| **包装方式** | 层层包裹 | 链式拦截 |
| **执行顺序** | 外层→内层 | 优先级排序 |
| **修改点** | 修改输入/输出 | 可阻塞、修改、注入 |
| **关注点** | 横向扩展 | 纵向扩展 |
| **运行时** | 编译/初始化时组装 | 事件触发时执行 |

**核心区别**：
- 装饰器："我如何包装"
- Hook："我何时干预"

### 5.3 模式对应表

| 传统 OOP | Hook | Fork |
|----------|------|------|
| Decorator | ✅ 层层拦截 | - |
| Chain of Responsibility | ✅ 链式决策 | - |
| Strategy | - | ✅ 路由策略 |
| Factory | - | ✅ 子代理创建 |
| Template Method | - | ✅ 执行模板 |
| Observer | ✅ 事件监听 | - |

---

## 六、实战配置示例

### 6.1 企业级安全 Hook 配置

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "if": "Bash(rm *|mv *|chmod *)",
            "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/block-destructive.sh",
            "timeout": 10
          }
        ]
      },
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "agent",
            "prompt": "检查以下代码是否包含安全漏洞：$ARGUMENTS\n如有漏洞返回 {decision: block, reason: ...}",
            "timeout": 60
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "matcher": "Write|Edit",
        "hooks": [
          {
            "type": "command",
            "command": "$CLAUDE_PROJECT_DIR/.claude/hooks/run-linter.sh",
            "async": true
          }
        ]
      }
    ]
  }
}
```

### 6.2 阻止破坏性命令的 Hook 脚本

```bash
#!/bin/bash
# .claude/hooks/block-destructive.sh

COMMAND=$(jq -r '.tool_input.command')

if echo "$COMMAND" | grep -q 'rm -rf\|mv.*\/$'; then
  jq -n '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: "Destructive command blocked by hook"
    }
  }'
else
  exit 0  # allow
fi
```

### 6.3 自定义 Subagent 配置

```markdown
---
name: code-reviewer
description: 专家代码审查员。主动审查代码的质量、安全性和可维护性。
tools: Read, Grep, Glob, Bash
model: sonnet
memory: project
---

# 代码审查系统提示词

你是高级代码审查员，确保代码质量和安全的高标准。

审查清单：
- 代码清晰可读
- 函数和变量命名良好
- 无重复代码
- 正确的错误处理
- 无暴露的密钥或 API 密钥
- 实现输入验证
- 有良好的测试覆盖
- 考虑性能

按优先级提供反馈：
- 关键问题（必须修复）
- 警告（应该修复）
- 建议（考虑改进）

提供具体的修复示例。
```

### 6.4 多代理编排

```markdown
---
name: coordinator
description: 协调多个专业代理完成复杂任务
tools: Agent(code-reviewer), Agent(security-reviewer), Agent(test-runner), Read, Bash
model: sonnet
---

你是任务协调员。当收到复杂任务时：
1. 分析任务需求
2. 将任务分解为子任务
3. 依次调用相应专业代理
4. 汇总结果返回
```

### 6.5 Skill with Fork 配置

```yaml
---
name: analyze-codebase
description: 分析代码库架构和模式
context: fork
agent: Explore
---

探索代码库并回答关于其架构的问题。
只返回分析结果，不要包含详细的文件内容。
```

---

## 七、总结

### 7.1 机制职责划分

| 机制 | 职责 | 关键特性 |
|------|------|----------|
| **Hook** | 生命周期干预 | 26 事件、5 种执行引擎、精细过滤、三层安全 |
| **Fork** | 上下文隔离 | 字节级缓存、递归保护、Isolation Mode、Memory |
| **组合** | 完整工作流 | Fork 内嵌 Hook、Skill + Fork + Hook |

### 7.2 核心理念

- **Hook** = "何时干预"（When）→ 事件驱动、精细控制
- **Fork** = "在哪运行"（Where）→ 隔离执行、资源优化

### 7.3 选型建议

**使用 Hook 当**：
- 需要在特定操作前后进行验证
- 需要阻止或修改工具调用
- 需要注入额外上下文
- 需要日志记录和审计

**使用 Fork 当**：
- 任务产生大量中间输出
- 需要隔离执行避免污染主上下文
- 需要限制工具访问
- 需要不同模型或配置运行

**组合使用当**：
- 需要在隔离环境中进行安全验证
- 需要复杂的代理编排加上细粒度控制

---

## 参考资料

- [Claude Code Hooks 官方文档](https://code.claude.com/docs/en/hooks)
- [Claude Code Subagents 官方文档](https://code.claude.com/docs/en/sub-agents)
- [Harness Engineering - Martin Fowler](https://martinfowler.com/articles/harness-engineering.html)
- [Claude Code Fork and Agent Arguments](https://shiqimei.github.io/posts/claude-code-fork-agent-subagents)
- [Sub-Agents and the Fork Pattern - DeepWiki](https://deepwiki.com/lintsinghua/claude-code-book/4.1-sub-agents-and-the-fork-pattern)

---

*本文档由 AI 生成，基于 Claude Code 官方文档和社区实践。*