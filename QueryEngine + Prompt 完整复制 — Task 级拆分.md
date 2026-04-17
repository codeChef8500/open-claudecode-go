# QueryEngine + Prompt 完整复制 — Task 级拆分

将 10 个 Phase 拆为 **60 个中粒度 task**，每 task 一个逻辑完整单元（半天至一天），含 TS 源码锚点、输出文件、验收标准、依赖关系。

> 配套总方案：`queryengine-full-replication-5d1271.md`。Task 编号 `P<phase>.T<seq>`。依赖列 `—` 表示无前置。

## 迁移策略（取代原双路径方案）

**不引入 `CLAUDE_CODE_USE_QUERYENGINE` feature flag**。采用 **Engine 外壳保留 + 内部全量委托** 策略：

- `engine.Engine` 类和 `SubmitMessage(ctx, params) <-chan *StreamEvent` 签名保留（TUI/SDK/command 注入等依赖入口不变）
- `engine.go:SubmitMessage` 在 P8.T6 起内部直接委托给 `queryengine.QueryEngine.SubmitMessage(...)`（返回 `<-chan SDKMessage`）
- 中间加一个 `engine/sdk_to_stream_event.go` 适配器，把 SDKMessage 流转换为现有 `StreamEvent` 流，保证调用方零改动
- 旧的 `queryloop.go` / `recovery.go` / `streaming_executor.go` / `stop_hooks.go` 等文件在 P8.T6 落地后一并删除，后续 P9 直接在 `queryengine/` 内完成 TS `query.ts` 20 步对齐
- `engine.StreamEvent` 作为 SDKMessage 的客户端适配表示长期保留，不做进一步迁移

---

## Phase 1 — Prompt 常量 + Registry + Feature Flag (P0)

| # | Task | TS 锚点 | 输出文件 (Go) | 验收 | 依赖 |
|---|---|---|---|---|---|
| P1.T1 | Feature flag gate 基础 | `bun:bundle feature()` 使用处 | `internal/prompt/feature/flags.go` + `_test.go` | `IsEnabled("PROACTIVE")` 等 17 个 flag 全部可用，env 映射表覆盖 | — |
| P1.T2 | 字符串常量字面复制（cyber/xml/model/boundary/common） | `constants/cyberRiskInstruction.ts`, `constants/xml.ts`, `constants/prompts.ts:L102/L114/L118/L121`, `constants/common.ts` | `internal/prompt/constants/{cyberrisk,xml_tags,model_ids,boundary,common}.go` | 每常量 golden-byte 与 TS 原文一致；含 `SYSTEM_PROMPT_DYNAMIC_BOUNDARY`、`FRONTIER_MODEL_NAME`、`CLAUDE_4_5_OR_4_6_MODEL_IDS`、`TICK_TAG` | — |
| P1.T3 | Section registry + 会话级 cache | `constants/systemPromptSections.ts` (全 69 行) | `internal/prompt/sections/{registry,cache_state}.go` + `_test.go` | `SystemPromptSection`/`DangerousUncachedSystemPromptSection`/`ResolveSections`/`ClearSystemPromptSections` 全可用；cache 命中测试通过 | P1.T1 |
| P1.T4 | `prependBullets` + 小型 helper | `constants/prompts.ts:L167-173` | `internal/prompt/sysprompt/prepend_bullets.go` + `_test.go` | 嵌套数组/字符串混合输入与 TS 输出字节一致 | — |

## Phase 2 — 7 个静态 Prompt 章节 (P0)

| # | Task | TS 锚点 | 输出 | 验收 | 依赖 |
|---|---|---|---|---|---|
| P2.T1 | intro + system_section (含 getHooksSection) | `prompts.ts:L127-129, L175-197` | `sysprompt/{intro,system_section,hooks_section}.go` + golden 测试 | 2 章节 golden file 字节对齐 TS；含 CYBER_RISK 叠加 | P1.T2 T4 |
| P2.T2 | doing_tasks（含 ant-only 4 条） | `prompts.ts:L199-253` | `sysprompt/doing_tasks.go` + golden | ant/非 ant 双分支 golden 对齐；嵌套 `userHelpSubitems` 正确 | P1.T1 T4 |
| P2.T3 | actions + tone_style | `prompts.ts:L255-267, L430-442` | `sysprompt/{actions,tone_style}.go` + golden | 2 章节字面 | P1.T4 |
| P2.T4 | using_tools（REPL/embedded/fork-subagent/taskTool 4 分支） | `prompts.ts:L269-320` | `sysprompt/using_tools.go` + golden | 4 分支独立 golden；enabledTools 集合判断正确 | P1.T4 |
| P2.T5 | output_efficiency（ant `# Communicating` + 非 ant `# Output efficiency`） | `prompts.ts:L403-428` | `sysprompt/output_efficiency.go` + golden | 双版本 golden | P1.T1 T4 |

## Phase 3 — 13 个动态 Registry 章节 (P0)

| # | Task | TS 锚点 | 输出 | 验收 | 依赖 |
|---|---|---|---|---|---|
| P3.T1 | agent_tool + discover_skills + reminders | `prompts.ts:L316-341, L131-134` | `sysprompt/{agent_tool,discover_skills,reminders}.go` + golden | fork vs 常规分支字面对齐 | P1.T1 T3 |
| P3.T2 | session_guidance（verifier/explore/skill/ask_denied 多嵌套分支） | `prompts.ts:L352-400` | `sysprompt/session_guidance.go` + golden | 5 种组合分支单独 golden | P1.T3, P3.T1 |
| P3.T3 | language + output_style + ant_override | `prompts.ts:L136-158` | `sysprompt/{language,output_style,ant_override}.go` + golden | 空值分支返回 nil；字面对齐 | P1.T3 |
| P3.T4 | mcp_instructions（DANGEROUS uncached） | `prompts.ts:L160-165, L579-604` | `sysprompt/mcp_instructions.go` + golden | connected vs pending 过滤 + `## <name>` 拼接顺序 | P1.T3 |
| P3.T5 | scratchpad + frc + summarize | `prompts.ts:L797-819, L821-841` | `sysprompt/{scratchpad,frc,summarize}.go` + golden | scratchpad 由 `isScratchpadEnabled` 驱动；FRC 受 CACHED_MICROCOMPACT 门控 | P1.T1 T3 |
| P3.T6 | numeric_anchors + token_budget + brief（feature gated） | `prompts.ts:L529-554, L843-858` | `sysprompt/{numeric_anchors,token_budget,brief}.go` + golden | 对应 env 开启时输出；关闭时返回 nil | P1.T1 T3 |

## Phase 4 — Env Info + Proactive + Default Agent (P1)

| # | Task | TS 锚点 | 输出 | 验收 | 依赖 |
|---|---|---|---|---|---|
| P4.T1 | getKnowledgeCutoff + getShellInfoLine + getUnameSR | `prompts.ts:L713-756` | `sysprompt/env_info.go`（私有函数） + 单测 | 各模型 cutoff 映射对齐；Windows/linux unameSR 跨平台正确 | P1.T2 |
| P4.T2 | computeEnvInfo + computeSimpleEnvInfo（`<env>` / `# Environment`） | `prompts.ts:L606-710` | `sysprompt/env_info.go`（公开函数）+ golden | worktree 检测 / additional dirs / 模型描述 / fast-mode 句 / undercover 抑制 全对齐 | P4.T1 |
| P4.T3 | proactive 完整 autonomous 段（Pacing/First-wake/Subsequent/Responsive/Bias/Concise/Terminal focus + brief 叠加） | `prompts.ts:L860-914` | `sysprompt/proactive.go` + golden | SLEEP_TOOL_NAME/TICK_TAG 引用正确；KAIROS/PROACTIVE 门控 | P1.T1 T2 |
| P4.T4 | DEFAULT_AGENT_PROMPT + enhanceSystemPromptWithEnvDetails | `prompts.ts:L758, L760-791` | `sysprompt/default_agent.go` + 单测 | subagent notes 4 条 + discoverSkillsGuidance 追加 + envInfo 末尾 | P4.T2, P3.T1 |

## Phase 5 — GetSystemPrompt 总装 + BuildEffectiveSystemPrompt (P1)

| # | Task | TS 锚点 | 输出 | 验收 | 依赖 |
|---|---|---|---|---|---|
| P5.T1 | `GetSystemPrompt` 4 分支主装配 | `prompts.ts:L444-577` | `sysprompt/builder.go` + 集成测试 | SIMPLE / Proactive-simple / 默认 / boundary 插入位置 4 分支分别 golden；dynamic sections 通过 registry 解析 | 全 P2+P3+P4 |
| P5.T2 | BuildEffectiveSystemPrompt 5 优先级 | `utils/systemPrompt.ts` | `internal/prompt/effective/effective.go` + 测试 | override → coordinator → agent → custom → default + append 5 档分派测试通过 | P5.T1 |
| P5.T3 | 旧 `internal/prompt/system.go` 适配层迁移 | — | 改造 `prompt/system.go` 委托至新实现 | 旧 `BuildEffectiveSystemPrompt(BuildOptions)` API 保持不变但内部走新链；现有 engine 单测全绿 | P5.T2 |

## Phase 6 — Context (system/user/git) 对齐 (P1)

| # | Task | TS 锚点 | 输出 | 验收 | 依赖 |
|---|---|---|---|---|---|
| P6.T1 | git_status（5 并发 + MAX_STATUS_CHARS 截断 + memoize） | `context.ts:L36-111` | `internal/prompt/context_go/git_status.go` + 测试 | 截断行为/非 git 仓库返回 nil/并发安全 | — |
| P6.T2 | getSystemContext + systemPromptInjection（BREAK_CACHE_COMMAND） | `context.ts:L22-34, L116-150` | `internal/prompt/context_go/{system_context,injection}.go` + 测试 | memoize + SetSystemPromptInjection 清 cache；cacheBreaker 门控 | P1.T1, P6.T1 |
| P6.T3 | getUserContext（claudeMd + currentDate，bare/disable 开关） | `context.ts:L155-190` | `internal/prompt/context_go/user_context.go` + 测试 | `CLAUDE_CODE_DISABLE_CLAUDE_MDS` / bare 分支覆盖 | — |
| P6.T4 | 统一 `ClearContextCaches()` + 接入 /clear /compact | — | 失效钩子 + 命令注册点改造 | `/clear` 后下一次 submit 重算 context | P6.T1 T2 T3, P1.T3 |

## Phase 7 — SDKMessage 类型族 + Helpers (P0)

| # | Task | TS 锚点 | 输出 | 验收 | 依赖 |
|---|---|---|---|---|---|
| P7.T1 | SDKMessage 类型族 | `entrypoints/agentSdkTypes.ts` + `QueryEngine.ts` | `internal/engine/queryengine/sdk_messages.go` + 序列化测试 | 所有 subtype（init/compact_boundary/api_retry/assistant/user/user-replay/tool_use_summary/stream_event/result.success + 4 error）JSON 往返字段齐全 | — |
| P7.T2 | Usage accumulate + permission denials 追踪 | `QueryEngine.ts:L812-825, permission 相关` | `queryengine/{usage,permission_denials}.go` + 测试 | message_start/delta/stop 三事件累加；wrappedCanUseTool 记录 denial | P7.T1 |
| P7.T3 | buildSystemInitMessage | `utils/messages/systemInit.ts` | `queryengine/system_init.go` + 测试 | 所有字段与 TS 一一对齐；Task↔Agent 转义；UDS_INBOX 门控 | P7.T1, P1.T1 |
| P7.T4 | fetchSystemPromptParts | `utils/queryContext.ts` | `queryengine/fetch_prompt_parts.go` + 测试 | 并行返回 `{defaultSystemPrompt,userContext,systemContext}`；customPrompt 时跳过 default | P5.T1, P6.T2 T3 |
| P7.T5 | orphanedPermission + queryHelpers（IsResultSuccessful/NormalizeMessage） | `utils/queryHelpers.ts` | `queryengine/{orphaned_permission,query_helpers}.go` + 测试 | 孤儿权限只 yield 一次；IsResultSuccessful 覆盖 10+ 输入组合 | P7.T1 |
| P7.T6 | fileStateCache + headlessProfilerCheckpoint + structured_output 注册骨架 | `QueryEngine.ts:L205, L327` | `queryengine/{file_state_cache,headless_profiler,structured_output}.go` | Clone 保证深拷贝；checkpoint 默认 no-op；structured output hook 注册/解注册对称 | — |

## Phase 8 — QueryEngine 主类 + submitMessage (P0)

| # | Task | TS 锚点 | 输出 | 验收 | 依赖 |
|---|---|---|---|---|---|
| P8.T1 | QueryEngine + Config 骨架 + 字段 | `QueryEngine.ts:L130-208` | `queryengine/queryengine.go`（struct + NewQueryEngine） + 测试 | 字段全量对齐；ctor 参数校验 | 全 P7 |
| P8.T2 | submitMessage — 预处理段（steps 1-9） | `QueryEngine.ts:L284-551` | `queryengine/queryengine.go:submitPreamble` + 集成测试 | yield `system/init` 之前的所有副作用顺序对齐；含 orphanedPermission | P8.T1, P7.T3 T4 T5 |
| P8.T3 | submitMessage — shouldQuery=false 分支 | `QueryEngine.ts:L556-639` | `queryengine/queryengine.go:handleLocalCommand` + 集成测试 | 本地 slash 命令走本路径，yield `result.success` 并 return | P8.T2 |
| P8.T4 | submitMessage — 主循环消息分派 | `QueryEngine.ts:L675-968` | `queryengine/queryengine.go:dispatchQueryMessage` + 测试 | 10 种 msg.type 分派顺序正确；compact_boundary/api_retry/snipReplay/tool_use_summary 覆盖 | P8.T2 |
| P8.T5 | submitMessage — 终局 + 预算/重试超限 | `QueryEngine.ts:L972-1155` | `queryengine/queryengine.go:finalizeResult` + 测试 | 5 种 result 分支 + ede_diagnostic 水印 + maxBudgetUsd 触发 | P8.T4 |
| P8.T6 | Ask 便捷函数 + Engine 外壳委托 + SDKMessage→StreamEvent 适配器 | `QueryEngine.ts:L1186-1295`, `engine.go:SubmitMessage` | `queryengine/ask.go` + 改造 `engine/engine.go` + 新增 `engine/sdk_to_stream_event.go` | `Engine.SubmitMessage` 签名不变；内部调用 `queryengine.QueryEngine`；旧 `queryloop.go`/`recovery.go`/`streaming_executor.go`/`stop_hooks.go` 删除；所有现有 engine 集成测试全绿 | P8.T5 |

## Phase 9 — Query Loop 重构对齐 TS (P1)

> 所有 P9 task 在 `internal/engine/queryengine/queryloop/` 子包内完成。P8.T6 删除旧 `engine/{queryloop,recovery,streaming_executor,stop_hooks}.go` 后，`queryengine.SubmitMessage` 直接调用本阶段实现的新 queryLoop。

| # | Task | TS 锚点 | 输出 | 验收 | 依赖 |
|---|---|---|---|---|---|
| P9.T1 | queryLoop 骨架 + queryTracking + skill prefetch + checkpoint | `query.ts:L241-355` | `queryengine/queryloop/loop.go` + `tracking.go` + `prefetch.go` | chainId/depth 跨 subagent 继承；skill prefetch 默认 no-op；checkpoint 点齐全 | P8.T6 |
| P9.T2 | applyToolResultBudget 完整 + contentReplacementState 持久化 | `query.ts:L369-394, utils/toolResultStorage.ts` | `queryengine/queryloop/toolresult_budget.go` + 测试 | 超预算替换注入、按 agentId 路由持久化 | P9.T1 |
| P9.T3 | HISTORY_SNIP 骨架 | `query.ts:L400-410, services/compact/snipCompact.ts` | `queryengine/compact/snip.go` + 测试 | feature off 时 no-op；on 时 tokensFreed 传入 autocompact | P1.T1, P9.T1 |
| P9.T4 | microcompact + CACHED_MICROCOMPACT 延迟 boundary | `query.ts:L412-426, services/compact/microCompact.ts` | `queryengine/compact/microcompact.go` + 测试 | 延迟 boundary 到 API 后；与 autocompact 组合不冲突 | P1.T1, P9.T1 |
| P9.T5 | CONTEXT_COLLAPSE 骨架 | `query.ts:L440-447, services/contextCollapse/` | `queryengine/compact/collapse.go` + 测试 | feature off 时透传；on 时 applyCollapsesIfNeeded 链路 | P1.T1, P9.T1 |
| P9.T6 | autocompact 结构对齐（compactionResult/buildPostCompactMessages/tracking） | `query.ts:L453-543` | `queryengine/compact/autocompact.go` + 测试 | preCompact/postCompact token 返回、summary+attachments+hookResults 拼接 | P9.T3 T4 |
| P9.T7 | task_budget.remaining 跨 compact 追踪 | `query.ts:L508-515` | `queryengine/queryloop/task_budget.go` + 测试 | 连续 compact 累减正确 | P9.T6 |
| P9.T8 | isAtBlockingLimit 预检 + PTL 门控（reactive/collapse 互斥） | `query.ts:L615-648` | `queryengine/queryloop/blocking_limit.go` + 测试 | PROMPT_TOO_LONG yield 时机与 TS 一致 | P9.T5 |
| P9.T9 | callModel 参数全量传递 + provider 接口扩展 | `query.ts:L659-707` | `queryengine/queryloop/call_model.go` + provider 接口扩展 | 所有 options 字段（fastMode/fetchOverride/queryTracking/taskBudget/...）直通 provider | P9.T1 |
| P9.T10 | streaming fallback + tombstone + backfill observable input | `query.ts:L708-787` | `queryengine/queryloop/stream_adapter.go` | fallback 触发时清空 pending；backfill 仅在新增字段时克隆 | P9.T9 |
| P9.T11 | withheld 三链完整恢复（PTL/MOT/MEDIA） | `query.ts` 恢复分支 | `queryengine/queryloop/recovery.go` + 测试 | 3 类错误 recovery order 与 TS 一致；MOT 走 ESCALATED_MAX_TOKENS；MEDIA 仅 reactive-compact | P9.T5 T8 |
| P9.T12 | StreamingToolExecutor 对齐 | `services/tools/StreamingToolExecutor.ts` | `queryengine/queryloop/streaming_executor.go` + 测试 | 并发安全、discard 幂等、streaming 下 tool_result 顺序正确 | P9.T9 |
| P9.T13 | runTools 非流式回退对齐 | `services/tools/toolOrchestration.ts` | `queryengine/queryloop/tool_orchestration.go` | 并发/串行分组规则、tombstone 行为与 TS 一致 | P9.T12 |
| P9.T14 | handleStopHooks 完整链 + stopFailure + tool_use_summary 异步 | `query/stopHooks.ts`, `services/toolUseSummary/` | `queryengine/queryloop/stop_hooks.go` + `tool_use_summary.go` | stop hook block → retry；summary 在下一 iteration 开头 yield | P9.T1 |
| P9.T15 | auto-continue（task_budget + token_budget）+ maxTurns attachment | `query.ts` 对应分支 | `queryengine/queryloop/continuation.go` + 测试 | 耗尽时注入 nudge 并 continue；maxTurns → attachment.max_turns_reached | P9.T7 |

## Phase 10 — Tool Prompts + Coordinator + Compact Prompt (P2)

| # | Task | TS 锚点 | 输出 | 验收 | 依赖 |
|---|---|---|---|---|---|
| P10.T1 | File 系工具 prompt 对齐（FileRead/FileWrite/FileEdit/NotebookEdit） | `tools/File*/prompt.ts`, `tools/NotebookEditTool/prompt.ts` | 4 个 `internal/tool/<t>/prompt.go` + `Description()` 改造 | 4 tools DESCRIPTION 字节对齐 TS | — |
| P10.T2 | 搜索/Shell 系（Grep/Glob/Bash/PowerShell/LSP） | `tools/{Grep,Glob,Bash,PowerShell,LSP}Tool/prompt.ts` | 5 个 prompt.go | 字节对齐 | — |
| P10.T3 | 任务/Todo/Skill（TaskCreate/Get/List/Stop/Update/TodoWrite/Skill/Sleep） | 对应 prompt.ts × 8 | 8 个 prompt.go | 字节对齐 | — |
| P10.T4 | Agent/AskUserQuestion/ExitPlanMode/EnterPlanMode/Worktree | 对应 prompt.ts × 5 | 5 个 prompt.go | 字节对齐 | — |
| P10.T5 | MCP 系（MCPTool/ListMcpResources/ReadMcpResource/SendMessage/RemoteTrigger/ScheduleCron/TeamCreate/TeamDelete/Config） | 对应 prompt.ts × 9 | 9 个 prompt.go | 字节对齐 | — |
| P10.T6 | 可选/实验（Brief/DiscoverSkills/SyntheticOutput 等 feature-gated 工具） | 对应 prompt.ts 剩余 | 剩余 prompt.go + feature 门控 | feature off 时 tool 不注册；on 时 DESCRIPTION 对齐 | P1.T1 |
| P10.T7 | Coordinator system prompt + userContext 最终对齐 | `coordinator/coordinatorMode.ts` | 改造 `internal/agent/coordinator_mode.go` + `internal/prompt/coordinator*.go` | 6 章节 + `{worker_tools, mcp_pr_activity_instructions}` 字节对齐；ant/SIMPLE/KAIROS 分支 | P5.T1 |
| P10.T8 | Compact prompt 对齐（system + user 模板） | `services/compact/prompt.ts` (16652 bytes) | `internal/engine/compact_prompt.go` 新增 / 改造 | compact 触发的 fork agent 输入字节对齐；集成测试验证 summaryMessages 结构 | P9.T6 |
| P10.T9 | E2E 集成测试：TS CLI vs Go CLI prompt 输出一致性 | — | `test/e2e/prompt_parity_test.go` + 比对脚本 | 同 prompt 输入，剥离会话变量后 system prompt 字节一致 | 全 P1-P10 |

---

## 汇总

- **总 task 数**：60（P1=4, P2=5, P3=6, P4=4, P5=3, P6=4, P7=6, P8=6, P9=15, P10=9）
- **总代码规模**：~9400 行 Go + 对应单测/集成测试
- **推荐节奏**：每 task 0.5-1 天；P0 阶段（P1/P2/P3/P7/P8）19 task 优先推进；P9 规模最大（15 task）需分段稳定
- **commit 策略**：每 task 1 个独立 commit，message 前缀 `[P<n>.T<seq>] ...`；Phase 结束后 tag `phase-N-done`
- **回归保障**：每 task 末保证旧测试 + 新增测试全绿；P8.T6 落地即完成切换（无双路径），旧 `queryloop/recovery/streaming_executor/stop_hooks` 删除
- **并行机会**：P1-P6（prompt 链）与 P7-P8 前期（SDKMessage 类型 + helpers）可并行；P9 必须在 P8.T6 切换完成后推进（在新 `queryengine/queryloop/` 子包内落地）
- **风险热点**：P8.T6（切换瞬间 engine 集成测试必须全绿）/ P9.T11（withheld 三链）/ P9.T12（StreamingToolExecutor）/ P10.T9（E2E 字节一致），提前预留缓冲 buffer

---

## 验收通用门槛

每 task 完成需满足：
1. TS 源文件 + 行号在 PR 描述中引用
2. 字面字符串章节：golden file `test/golden/<name>.txt` 与 TS 原文字节一致
3. 函数级单测 / 集成测试覆盖率 ≥ 80%
4. `go vet` / `go build ./...` / `go test ./...` 全绿
5. `progress.txt` 追加 `[P<n>.T<seq>] done <ISO date>`
6. 引入新 env/flag 时更新 `README.md` 环境变量表
