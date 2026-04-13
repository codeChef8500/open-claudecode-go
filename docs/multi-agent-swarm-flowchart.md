# 多智能体协作与 Swarm 技术 — 业务流程图

> 本文档基于 `open-claudecode-go` 代码库，详细描述多智能体协作架构、两种协作模式及其业务流程。

---

## 1. 系统整体架构

```mermaid
graph TB
    subgraph 用户层
        USER["👤 用户 / TUI"]
    end

    subgraph 会话层
        SESSION["Session Runner"]
        AGENTOOL["AgentTool<br/>(internal/tool/agentool)"]
    end

    subgraph 协调层
        COORD["CoordinatorMode<br/>coordinator_mode.go"]
        SWARM["SwarmManager<br/>swarm/manager.go"]
    end

    subgraph 生命周期管理
        ASYNC["AsyncLifecycleManager<br/>async_lifecycle.go"]
        TEAM_MGR["TeamManager<br/>团队 CRUD + 持久化"]
    end

    subgraph 后端执行层
        BACKEND_REG["BackendRegistry<br/>swarm/registry.go"]
        INPROCESS["InProcessBackend<br/>goroutine 执行"]
        TMUX["TmuxBackendImpl<br/>tmux 窗格执行"]
    end

    subgraph 通信层
        MAILBOX["HybridMailboxAdapter<br/>swarm/mailbox_adapter.go"]
        MEM_MB["InMemory MailboxRegistry<br/>高性能内存信箱"]
        FILE_MB["FileMailboxRegistry<br/>跨进程文件信箱"]
    end

    subgraph 权限层
        PERM_BRIDGE["LeaderPermissionBridge<br/>进程内权限桥"]
        PERM_SYNC["PermissionSync<br/>邮箱权限同步"]
    end

    subgraph 运行时
        TEAMMATE_REG["TeammateRegistry<br/>活跃 Teammate 追踪"]
        APPSTATE["AppState<br/>全局状态"]
    end

    USER --> SESSION
    SESSION --> AGENTOOL
    AGENTOOL -->|"Coordinator 模式"| COORD
    AGENTOOL -->|"Swarm 团队模式"| SWARM

    COORD --> ASYNC
    SWARM --> BACKEND_REG
    SWARM --> TEAM_MGR
    SWARM --> MAILBOX
    SWARM --> PERM_BRIDGE

    ASYNC -->|"Launch goroutine"| INPROCESS

    BACKEND_REG -->|"auto 检测"| INPROCESS
    BACKEND_REG -->|"auto 检测"| TMUX

    MAILBOX -->|"InProcess agents"| MEM_MB
    MAILBOX -->|"Tmux agents"| FILE_MB

    INPROCESS --> TEAMMATE_REG
    TMUX --> TEAMMATE_REG

    INPROCESS --> APPSTATE
    TMUX --> APPSTATE

    PERM_BRIDGE -.->|"进程内"| INPROCESS
    PERM_SYNC -.->|"跨进程"| FILE_MB
```

---

## 2. Coordinator 模式流程

> **适用场景**: 复杂任务需拆分为多个独立子任务并行执行，每个 Worker 在独立 Git Worktree 中工作。

```mermaid
flowchart TD
    START(["🚀 用户提交复杂任务"])
    CHECK_ENV{"检查环境变量<br/>CLAUDE_CODE_COORDINATOR_MODE"}

    START --> CHECK_ENV
    CHECK_ENV -->|"= 1 / true"| ENTER_COORD["进入 Coordinator 模式"]
    CHECK_ENV -->|"未设置"| NORMAL["普通单 Agent 模式"]

    ENTER_COORD --> BUILD_PROMPT["生成 Coordinator 系统提示<br/>BuildCoordinatorSystemPrompt()"]
    BUILD_PROMPT --> PLAN["制定工作计划<br/>CoordinatorPlan / WorkItem[]"]
    PLAN --> SCRATCHPAD["创建共享 Scratchpad 目录<br/>EnsureScratchpad()"]

    SCRATCHPAD --> SPAWN_LOOP{"遍历 WorkItem"}

    SPAWN_LOOP -->|"还有未分配任务"| CHECK_LIMIT{"活跃 Worker < MaxWorkers?"}
    CHECK_LIMIT -->|"是"| SPAWN_WORKER["SpawnWorkerForced()<br/>启动异步 Worker"]
    CHECK_LIMIT -->|"否"| WAIT_SLOT["等待 Worker 完成释放位置"]
    WAIT_SLOT --> SPAWN_LOOP

    SPAWN_WORKER --> ASYNC_LAUNCH["AsyncLifecycleManager.Launch()<br/>创建 goroutine"]
    ASYNC_LAUNCH --> WORKTREE["分配独立 Git Worktree<br/>IsolationWorktree"]
    WORKTREE --> RUN_AGENT["AgentRunner.RunAgent()<br/>Worker 执行任务"]

    RUN_AGENT --> WORKER_DONE{"Worker 完成?"}
    WORKER_DONE -->|"成功"| STATUS_DONE["Status = done"]
    WORKER_DONE -->|"失败"| STATUS_FAIL["Status = failed"]
    WORKER_DONE -->|"取消"| STATUS_CANCEL["Status = cancelled"]

    STATUS_DONE --> NOTIFY["推送完成通知<br/>NotificationQueue"]
    STATUS_FAIL --> NOTIFY
    STATUS_CANCEL --> NOTIFY

    SPAWN_LOOP -->|"全部已分配"| POLL_LOOP["PollWorkers()<br/>轮询 Worker 状态"]

    POLL_LOOP --> ALL_DONE{"全部完成?"}
    ALL_DONE -->|"否"| POLL_LOOP
    ALL_DONE -->|"是"| COLLECT["CollectResults()<br/>收集所有结果"]

    COLLECT --> SYNTHESIZE["FormatWorkerResults()<br/>合成最终报告"]
    SYNTHESIZE --> FINISH["Finish()<br/>标记 Coordinator 完成"]
    NOTIFY --> POLL_LOOP

    FINISH --> END(["✅ 返回汇总结果"])

    style START fill:#4CAF50,color:#fff
    style END fill:#4CAF50,color:#fff
    style SPAWN_WORKER fill:#2196F3,color:#fff
    style ASYNC_LAUNCH fill:#2196F3,color:#fff
    style COLLECT fill:#FF9800,color:#fff
```

### Coordinator 模式关键约束

| 约束 | 值 | 来源 |
|------|---|------|
| 最大并发 Worker | `MaxWorkers`（默认 4） | `CoordinatorConfig` |
| 每 Worker 最大轮次 | `MaxTurnsPerWorker`（默认 100） | `CoordinatorConfig` |
| Worker 隔离方式 | Git Worktree | `IsolationWorktree` |
| Worker 可用工具 | Task, Read, Grep, Glob, Bash, Edit... | `CoordinatorAllowedTools` |
| 协调者可用工具 | Task, Read, Grep, Glob, TodoRead, TodoWrite | 仅读取+派发 |

---

## 3. Swarm 团队模式流程

> **适用场景**: 需要持续协作的团队场景，Team Lead 与多个 Teammate 通过消息信箱实时通信。

```mermaid
flowchart TD
    START(["🚀 创建 Swarm 团队"])

    START --> CREATE_SWARM["NewSwarmManager(cfg)<br/>初始化所有子系统"]

    CREATE_SWARM --> INIT_REG["NewTeammateRegistry()<br/>创建 Teammate 注册表"]
    CREATE_SWARM --> INIT_PERM["NewLeaderPermissionBridge()<br/>创建权限桥"]
    CREATE_SWARM --> INIT_BACKEND["NewBackendRegistry(mode)<br/>后端注册表"]
    CREATE_SWARM --> INIT_MAILBOX["NewHybridMailboxAdapter()<br/>混合消息适配器"]

    INIT_BACKEND --> DETECT{"detectBackend()<br/>自动检测后端"}
    DETECT -->|"$TMUX 存在"| REG_TMUX["注册 TmuxBackendImpl"]
    DETECT -->|"tmux 可用(非 Windows)"| REG_TMUX
    DETECT -->|"Windows / 无 tmux"| REG_INPROCESS["注册 InProcessBackend"]
    REG_TMUX --> REG_INPROCESS

    INIT_MAILBOX --> READY(["✅ SwarmManager 就绪"])

    READY --> SPAWN_TM["SpawnTeammate(ctx, cfg)<br/>创建新 Teammate"]

    SPAWN_TM --> RESOLVE["BackendRegistry.ResolveExecutor()<br/>选择执行后端"]
    RESOLVE --> EXEC_SPAWN["executor.Spawn(ctx, config)"]

    EXEC_SPAWN -->|"InProcess"| GOROUTINE["启动 goroutine<br/>InProcessBackend.Spawn()"]
    EXEC_SPAWN -->|"Tmux"| TMUX_PANE["创建 tmux 窗格<br/>TmuxBackendImpl.Spawn()"]

    GOROUTINE --> LIFECYCLE["RunInProcessTeammate()<br/>进程内生命周期"]
    TMUX_PANE --> FILE_COMM["通过文件信箱通信"]

    LIFECYCLE --> INIT_PROMPT["执行初始 Prompt<br/>RunAgent(ctx, prompt)"]
    INIT_PROMPT --> POLL_LOOP{"Mailbox 轮询循环<br/>每 1s 检查信箱"}

    POLL_LOOP -->|"有消息"| MSG_DISPATCH{"消息分发"}
    POLL_LOOP -->|"无消息"| IDLE["发送 idle_notification<br/>通知 Team Lead"]
    IDLE --> POLL_LOOP

    MSG_DISPATCH -->|"shutdown_request"| SHUTDOWN["发送 shutdown_approved<br/>退出循环"]
    MSG_DISPATCH -->|"permission_response"| PERM_RESOLVE["PermBridge.Resolve()<br/>解锁工具权限"]
    MSG_DISPATCH -->|"task_assignment"| RUN_TASK["RunAgent(ctx, task)<br/>执行分配的任务"]
    MSG_DISPATCH -->|"plain_text"| RUN_TEXT["RunAgent(ctx, text)<br/>处理文本消息"]
    MSG_DISPATCH -->|"mode_set_request"| SET_MODE["更新 PermissionMode"]
    MSG_DISPATCH -->|"team_permission_update"| UPDATE_PERM["更新工具过滤规则"]

    PERM_RESOLVE --> POLL_LOOP
    RUN_TASK --> POLL_LOOP
    RUN_TEXT --> POLL_LOOP
    SET_MODE --> POLL_LOOP
    UPDATE_PERM --> POLL_LOOP

    SHUTDOWN --> CLEANUP["从 AppState 注销<br/>清理资源"]
    CLEANUP --> TM_EXIT(["🏁 Teammate 退出"])

    style START fill:#9C27B0,color:#fff
    style READY fill:#4CAF50,color:#fff
    style SPAWN_TM fill:#2196F3,color:#fff
    style TM_EXIT fill:#607D8B,color:#fff
    style MSG_DISPATCH fill:#FF9800,color:#fff
```

---

## 4. 消息通信时序图

> 展示 Team Lead 与 Teammate 之间的典型消息交互流程。

```mermaid
sequenceDiagram
    participant User as 👤 用户
    participant Lead as 🎯 Team Lead
    participant MB as 📬 HybridMailbox
    participant TM1 as 🤖 Teammate-A
    participant TM2 as 🤖 Teammate-B

    User->>Lead: 提交复杂任务
    Lead->>Lead: 分解任务为子任务

    Note over Lead,TM1: == 阶段1: 创建团队 ==
    Lead->>MB: CreateTeam("my-team")
    Lead->>TM1: SpawnTeammate(task_a)
    Lead->>TM2: SpawnTeammate(task_b)

    Note over Lead,TM2: == 阶段2: 任务分配 ==
    Lead->>MB: Send(task_assignment → TM1)
    MB->>TM1: ReadPending() → task_assignment
    TM1->>TM1: RunAgent(task_a)

    Lead->>MB: Send(task_assignment → TM2)
    MB->>TM2: ReadPending() → task_assignment
    TM2->>TM2: RunAgent(task_b)

    Note over TM1,MB: == 阶段3: 权限请求 ==
    TM1->>MB: permission_request(bash tool)
    MB->>Lead: ReadPending() → permission_request
    Lead->>User: 显示权限请求 UI
    User->>Lead: 批准
    Lead->>MB: permission_response(allow)
    MB->>TM1: ReadPending() → permission_response
    TM1->>TM1: 继续执行

    Note over TM2,MB: == 阶段4: 任务完成 ==
    TM2->>MB: idle_notification(completed)
    MB->>Lead: ReadPending() → idle_notification

    TM1->>MB: idle_notification(completed)
    MB->>Lead: ReadPending() → idle_notification

    Note over Lead,TM2: == 阶段5: 关闭团队 ==
    Lead->>MB: shutdown_request → TM1
    MB->>TM1: ReadPending() → shutdown_request
    TM1->>MB: shutdown_approved
    TM1->>TM1: 退出

    Lead->>MB: shutdown_request → TM2
    MB->>TM2: ReadPending() → shutdown_request
    TM2->>MB: shutdown_approved
    TM2->>TM2: 退出

    Lead->>User: 返回汇总结果
```

---

## 5. 消息类型一览

> 14 种结构化消息类型，定义于 `swarm/message_types.go`

```mermaid
graph LR
    subgraph 基础消息
        T1["text<br/>纯文本消息"]
        T2["idle_notification<br/>空闲通知"]
    end

    subgraph 权限流
        T3["permission_request<br/>工具权限请求"]
        T4["permission_response<br/>权限应答"]
        T5["team_permission_update<br/>团队权限更新"]
        T12["sandbox_permission_request<br/>沙箱权限请求"]
        T13["sandbox_permission_response<br/>沙箱权限应答"]
    end

    subgraph 生命周期
        T6["shutdown_request<br/>关闭请求"]
        T7["shutdown_approved<br/>确认关闭"]
        T8["shutdown_rejected<br/>拒绝关闭"]
    end

    subgraph 任务与模式
        T9["task_assignment<br/>任务分配"]
        T10["plan_approval_request<br/>计划审批请求"]
        T11["plan_approval_response<br/>计划审批应答"]
        T14["mode_set_request<br/>模式切换"]
    end

    style T1 fill:#E3F2FD
    style T2 fill:#E3F2FD
    style T3 fill:#FFF3E0
    style T4 fill:#FFF3E0
    style T5 fill:#FFF3E0
    style T6 fill:#FFEBEE
    style T7 fill:#FFEBEE
    style T8 fill:#FFEBEE
    style T9 fill:#E8F5E9
    style T10 fill:#E8F5E9
    style T11 fill:#E8F5E9
    style T12 fill:#FFF3E0
    style T13 fill:#FFF3E0
    style T14 fill:#E8F5E9
```

---

## 6. 权限同步流程

> 两条路径：InProcess 通过 Bridge (低延迟)，Tmux 通过 Mailbox (跨进程)。

```mermaid
flowchart LR
    subgraph InProcess 路径
        TM_IP["Teammate<br/>(goroutine)"]
        BRIDGE["LeaderPermissionBridge<br/>.Request(ctx, req)"]
        LEADER_CB["Leader UI Callback<br/>onRequest()"]
        TM_IP -->|"1. 提交请求"| BRIDGE
        BRIDGE -->|"2. 通知 UI"| LEADER_CB
        LEADER_CB -->|"3. 用户决策"| BRIDGE
        BRIDGE -->|"4. Resolve(granted)"| TM_IP
    end

    subgraph Tmux / Mailbox 路径
        TM_TMUX["Teammate<br/>(tmux pane)"]
        MB_OUT["Mailbox<br/>(文件信箱)"]
        LEADER_POLL["Leader<br/>轮询信箱"]
        TM_TMUX -->|"1. SendEnvelope<br/>permission_request"| MB_OUT
        MB_OUT -->|"2. ReadPending()"| LEADER_POLL
        LEADER_POLL -->|"3. 用户决策"| MB_OUT
        MB_OUT -->|"4. ReadPending()<br/>permission_response"| TM_TMUX
    end

    style TM_IP fill:#E3F2FD
    style TM_TMUX fill:#FFF3E0
    style BRIDGE fill:#C8E6C9
    style MB_OUT fill:#C8E6C9
```

---

## 7. Backend 选择流程

> `BackendRegistry.ResolveExecutor()` 的决策逻辑。

```mermaid
flowchart TD
    START(["ResolveExecutor()"])

    START --> CHECK_MODE{"BackendMode?"}

    CHECK_MODE -->|"in-process"| FORCE_IP["返回 InProcessBackend"]
    CHECK_MODE -->|"tmux"| FORCE_TMUX["返回 TmuxBackendImpl"]
    CHECK_MODE -->|"auto (默认)"| AUTO_DETECT["detectBackend()"]

    AUTO_DETECT --> CHECK_TMUX_ENV{"$TMUX 环境变量?"}
    CHECK_TMUX_ENV -->|"存在"| TMUX_INSIDE["✅ BackendTmux<br/>IsInsideTmux=true"]
    CHECK_TMUX_ENV -->|"不存在"| CHECK_OS{"runtime.GOOS?"}

    CHECK_OS -->|"!= windows"| CHECK_TMUX_BIN{"tmux 在 $PATH 中?"}
    CHECK_OS -->|"== windows"| FALLBACK_IP["⚠️ BackendInProcess<br/>'tmux not available on Windows'"]

    CHECK_TMUX_BIN -->|"找到"| TMUX_EXTERNAL["✅ BackendTmux<br/>IsInsideTmux=false"]
    CHECK_TMUX_BIN -->|"未找到"| FALLBACK_IP

    TMUX_INSIDE --> TRY_TMUX_EXEC{"tmux executor 已注册?"}
    TMUX_EXTERNAL --> TRY_TMUX_EXEC
    TRY_TMUX_EXEC -->|"是"| RETURN_TMUX["返回 TmuxBackendImpl"]
    TRY_TMUX_EXEC -->|"否"| TRY_IP{"in-process executor 已注册?"}

    FALLBACK_IP --> TRY_IP
    TRY_IP -->|"是"| RETURN_IP["返回 InProcessBackend"]
    TRY_IP -->|"否"| ERROR["❌ 错误: no executor available"]

    style START fill:#9C27B0,color:#fff
    style RETURN_TMUX fill:#4CAF50,color:#fff
    style RETURN_IP fill:#2196F3,color:#fff
    style ERROR fill:#F44336,color:#fff
```

---

## 8. InProcess Teammate 完整生命周期

> `RunInProcessTeammate()` 的完整状态机。

```mermaid
stateDiagram-v2
    [*] --> Registering : RunInProcessTeammate() 启动

    Registering --> RunningInitPrompt : 注册到 AppState.InProcessTeammates
    RunningInitPrompt --> PollingMailbox : 初始 Prompt 执行完成

    PollingMailbox --> ProcessingMessage : ReadPending() 返回消息
    PollingMailbox --> SendingIdle : 无新消息 & 未发送过 idle
    PollingMailbox --> PollingMailbox : 无新消息 & 已发送 idle (等待 1s)

    SendingIdle --> PollingMailbox : idle_notification 已发送

    ProcessingMessage --> HandleShutdown : 消息类型 = shutdown_request
    ProcessingMessage --> HandlePermission : 消息类型 = permission_response
    ProcessingMessage --> HandleTask : 消息类型 = task_assignment
    ProcessingMessage --> HandleText : 消息类型 = plain_text
    ProcessingMessage --> HandleMode : 消息类型 = mode_set_request
    ProcessingMessage --> HandlePermUpdate : 消息类型 = team_permission_update

    HandlePermission --> PollingMailbox : Bridge.Resolve()
    HandleTask --> RunningAgent : RunAgent(ctx, task)
    HandleText --> RunningAgent : RunAgent(ctx, text)
    HandleMode --> PollingMailbox : 更新 context PermissionMode
    HandlePermUpdate --> PollingMailbox : 存储权限更新

    RunningAgent --> PollingMailbox : Agent 执行完成

    HandleShutdown --> ShuttingDown : 发送 shutdown_approved

    ShuttingDown --> [*] : 从 AppState 注销, 退出

    note right of PollingMailbox
        每 1s 轮询一次
        每 30s 执行一次 compaction
        (更新 turnCount, totalPausedMs)
    end note
```

---

## 9. 两种模式对比

| 维度 | Coordinator 模式 | Swarm 团队模式 |
|------|-----------------|---------------|
| **入口** | `CoordinatorMode.SpawnWorkerForced()` | `SwarmManager.SpawnTeammate()` |
| **通信方式** | 无直接通信，通过 Scratchpad 共享文件 | 结构化 Mailbox 消息（14 种类型） |
| **隔离级别** | Git Worktree（文件系统级隔离） | Context 隔离 / Tmux Pane 隔离 |
| **生命周期** | 一次性任务：启动 → 执行 → 完成 | 持续运行：轮询信箱直到收到 shutdown |
| **并发管理** | `AsyncLifecycleManager`（goroutine + 状态轮询） | `TeammateExecutor`（InProcess / Tmux） |
| **权限模型** | Worker 继承 Coordinator 权限 | Leader 实时审批（Bridge / Mailbox） |
| **状态追踪** | `CoordinatorWorker.Status` | `AppState.InProcessTeammates` + `TeammateRegistry` |
| **适用场景** | 大规模并行代码修改 | 需要实时协作、权限管控的团队任务 |

---

## 10. 快速使用指南

### 启用 Swarm 功能
```bash
export AGENT_SWARMS_ENABLED=1
```

### 启用 Coordinator 模式
```bash
export CLAUDE_CODE_COORDINATOR_MODE=1
```

### 核心 API 调用链

```
# Swarm 团队模式
SwarmManagerConfig → NewSwarmManager() → SwarmManager
  ├── .SpawnTeammate(ctx, TeammateSpawnConfig)  → SpawnOutput
  ├── .SendMessage(from, to, text, priority)    → msgID
  ├── .BroadcastMessage(from, team, text)       → error
  ├── .ShutdownTeammate(agentID, reason)        → error
  └── .ShutdownAll(reason)                      → void

# Coordinator 模式
CoordinatorConfig → NewCoordinatorMode(runner, asyncMgr, loader, cfg) → CoordinatorMode
  ├── .SetPlan(CoordinatorPlan)
  ├── .SpawnWorkerForced(ctx, WorkItem, parentCtx) → agentID
  ├── .PollWorkers()                               → []completedIDs
  ├── .WaitAll(timeout)                            → error
  ├── .CollectResults()                            → map[agentID]*AgentRunResult
  └── .Finish()
```
