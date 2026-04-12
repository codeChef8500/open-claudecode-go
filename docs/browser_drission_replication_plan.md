# browser_drission.py 深度分析与完整复制执行方案

## 一、架构总览

### 1.1 文件规模
- **总行数**: 5420+ 行（单文件 269KB）
- **类定义**: 6 个核心类
- **Actions**: 100+ 种操作（V1~V6 六代迭代）
- **JS 注入脚本**: 2 段（~300行），反检测 + 控制台劫持

### 1.2 核心架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                    BrowserDrissionTool (主工具类)                 │
│  继承 Tool 基类，实现 execute() → dispatch 100+ actions          │
├─────────┬───────────┬──────────┬──────────────┬─────────────────┤
│ 会话管理 │  元素定位  │ 智能交互 │  网络/认证    │   高级能力       │
│ Session │ Locator   │ Click/  │ Network/     │ CDP/Fetch/      │
│ Manager │ Engine    │ Input   │ Auth/Cookie  │ CF Bypass       │
├─────────┴───────────┴──────────┴──────────────┴─────────────────┤
│                  DrissionBrowserSession (会话状态)                │
│  pages{} / _network_listener / _downloads / _cdp_session ...    │
├─────────────────────────────────────────────────────────────────┤
│                  Playwright Async Engine (底层引擎)               │
│  Browser → Context → Page → CDP Session                         │
└─────────────────────────────────────────────────────────────────┘
```

### 1.3 外部依赖

| 依赖           | 用途                          | 必须/可选 |
|---------------|-------------------------------|---------|
| playwright    | 浏览器自动化引擎                | 必须     |
| pydantic      | 参数模型验证                    | 必须     |
| httpx         | d/s 双模式 HTTP 层              | 可选     |
| curl_cffi     | TLS 指纹模拟（CF 绕过）          | 可选     |
| .base         | Tool/ToolContext/ToolResult 基类 | 必须     |
| .config       | get_config() 配置读取           | 必须     |
| app.agents.utils.time_utils | utc_now()        | 必须     |

---

## 二、6 大核心模块详细分析

### 模块 A：JS 注入脚本层（第 67~306 行）

#### A1. CONSOLE_INIT_SCRIPT（第 67~91 行）
- **功能**: 劫持 `console.log/info/warn/error/debug`，存入 `window.__drission_console_logs__[]`
- **上限**: 最多 500 条日志
- **格式**: `{type, message, timestamp}`
- **幂等**: `window.__drission_console_initialized__` 防重复注入

#### A2. ANTI_DETECT_SCRIPT（第 93~306 行）— 16 项反检测
| # | 反检测项 | 技术手段 |
|---|---------|---------|
| 1 | webdriver 标记 | `navigator.webdriver = undefined` |
| 2 | Chrome 指纹 | 完整伪造 `window.chrome` 对象（app/runtime/csi/loadTimes） |
| 3 | Permissions API | 劫持 `navigator.permissions.query` |
| 4 | Plugins/MimeTypes | 注入 3 个真实插件对象 |
| 5 | Language/Platform | zh-CN 语言链 + Win32 平台 |
| 6 | Canvas 指纹 | `toDataURL` 微噪点（确定性种子） |
| 7 | WebGL Renderer | Intel Iris OpenGL Engine |
| 8 | hasFocus | 始终返回 true |
| 9 | 窗口尺寸 | 修正 outerWidth/outerHeight |
| 10 | screen 属性 | availWidth/colorDepth/pixelDepth |
| 11 | AudioContext | 频率数据微噪点注入 |
| 12 | Battery API | 伪造 getBattery() |
| 13 | Connection API | 伪造 navigator.connection (4g/wifi) |
| 14 | WebRTC 泄露防护 | 过滤 srflx/relay candidate |
| 15 | MediaDevices | 标准化设备列表 |
| 16 | CDP 变量清除 | 删除 cdc_*/__playwright*/__pw_* + 拦截后续注入 |

---

### 模块 B：数据模型层（第 309~398 行）

#### B1. DataPacket（网络数据包）
```python
@dataclass
class DataPacket:
    url: str
    method: str
    request_headers: dict
    request_body: Optional[str]
    status: int
    response_headers: dict
    response_body: Optional[str]  # 截断 2000 字符
    resource_type: str
    timestamp: float
```

#### B2. DownloadMission（下载任务）
```python
@dataclass
class DownloadMission:
    mission_id: str
    url: str
    suggested_filename: str
    state: "pending" | "in_progress" | "done" | "failed" | "canceled"
    save_path: Optional[str]
    error: Optional[str]
```

#### B3. DrissionBrowserSession（会话状态 — 核心）
- **基础字段**: session_id, playwright, browser, context, pages{tab_id→Page}, active_tab_id
- **对话框**: _alert_text, _alert_type, _pending_dialog, _auto_alert
- **网络**: _network_listener, _downloads, _routes{}, _extra_headers{}
- **CDP**: _cdp_session, _fetch_cdp, _fetch_inject_headers, _fetch_block_patterns
- **HTTP 层**: _http_client(httpx), _curl_session(curl_cffi), _real_ua
- **状态**: _iframe_context, _load_mode, _is_persistent, _screencast_*

---

### 模块 C：多策略元素定位器 DrissionLocator（第 401~589 行）

**定位策略优先级**:

| 格式 | 示例 | 解析策略 |
|------|------|---------|
| CSS 快捷 | `#id` / `.class` | → CSS |
| CSS 前缀 | `css=sel` / `c=sel` | → CSS |
| XPath 前缀 | `xpath=expr` / `x=expr` / `//tag` | → XPath |
| 文本精确 | `text=登录` | → XPath `//*[text()="登录"]` |
| 文本包含 | `text:搜索` | → XPath `//*/text()[contains(.,"搜索")]/..` |
| 文本开头 | `text^搜索` | → XPath starts-with |
| 文本结尾 | `text$搜索` | → XPath substring |
| Tag+属性 | `tag:div@class=foo` | → XPath `//div[@class='foo']` |
| 多属性AND | `@@a=v1@@b=v2` | → XPath `@a='v1' and @b='v2'` |
| 多属性OR | `@\|a=v1@@b=v2` | → XPath `@a='v1' or @b='v2'` |
| 属性否定 | `@!a=v1` | → XPath `not(@a='v1')` |
| 单属性 | `@attr=val` | → XPath `//*[@attr='val']` |
| 默认 | 纯文本 | → XPath 模糊包含 |

**关键方法**:
- `resolve(locator) → (strategy, value)` — 解析入口
- `to_playwright(locator) → str` — 转 Playwright 格式
- `_escape(text) → str` — XPath 引号转义（concat 方式）
- `_parse_attr_conditions(attr_str) → str` — 多属性条件解析

---

### 模块 D：网络监听器 NetworkListener（第 592~767 行）

**核心机制**: Playwright `page.on("request"/"response")` + `asyncio.Queue`

| 方法 | 功能 |
|------|------|
| `start(targets, is_regex, methods, res_types)` | 启动监听，注册事件 |
| `stop()` | 停止监听，移除事件 |
| `clear()` | 清空队列 |
| `wait(count, timeout)` | 阻塞等待 N 个包 |
| `wait_silent(timeout)` | 等待网络静默 |
| `get_nowait(max_count)` | 非阻塞获取 |

**数据流**: Request 事件 → _pending{} 暂存 → Response 事件 → 配对 → DataPacket → Queue

---

### 模块 E：会话管理器 DrissionSessionManager（第 850~1120 行）

**生命周期**:
1. `create_session()` — 启动 Playwright → 创建 Browser/Context/Page → 注入反检测 → UA 清洗
2. `get_session()` — 更新 last_used 时间戳
3. `close_session()` — 停止监听器 → detach CDP → 关闭 httpx/curl_cffi → 关闭 context/browser/playwright
4. `_cleanup_idle_sessions()` — 自动清理超时会话

**创建模式（三选一）**:
- **CDP 接管**: `cdp_url` → `pw.chromium.connect_over_cdp()` — 最强（保留扩展+登录态）
- **持久化**: `user_data_dir` → `launch_persistent_context()` — 保留登录态
- **标准**: `pw.chromium.launch()` → `browser.new_context()` — 默认

**注入时序**:
```
创建 Context → add_init_script(ANTI_DETECT) → add_init_script(CONSOLE)
→ 获取 Page → _bind_page_handlers(dialog+console)
→ 动态 UA 检测 → HeadlessChrome → Chrome 替换
→ context.set_extra_http_headers + add_init_script(ua_js)
```

---

### 模块 F：主工具类 BrowserDrissionTool（第 1491~6143 行）

#### F1. Actions 全量清单（100+ actions，按版本分组）

**V1 基础操作 (40 个)**:
- 会话: create_session, close_session, list_sessions, setup_download
- 导航: navigate, back, forward, reload, wait_for_load
- 元素: find_element, find_elements, get_element_info, get_element_state
- 交互: smart_click, hover, double_click, right_click, drag_drop, select_option, upload_file, execute_js, key_press, clear_input
- 等待: wait_for_element, wait_for_url, wait_for_title, wait_for_network_idle, wait_for_alert
- 网络: network_listen_start/wait/get/stop/clear
- 对话框: handle_alert, get_alert_text, set_auto_alert
- IFrame: list_iframes, enter_iframe, exit_iframe
- 标签: new_tab, list_tabs, switch_tab, close_tab
- 数据: get_cookies, set_cookies, get_storage, get_console_logs
- 截图: screenshot, screenshot_element, pdf
- 下载: wait_download_start/done, list_downloads
- 录制: screencast_start/stop
- 快照: snapshot

**V2 认证绕过 (16 个)**:
- set_extra_headers, set_user_agent, set_http_auth
- route_add, route_remove, route_list, set_blocked_urls
- inject_cookies_string, inject_auth_token
- save_mhtml, get_blob_url, set_load_mode

**P2 补全 (8 个)**:
- scroll, input, get_html, wait_for_any_element
- click_for_new_tab, click_for_url_change, find_child, network_listen_steps

**P3 Actions API (10 个)**:
- action_move_to, action_move, action_click_at, action_hold, action_release
- action_scroll_at, action_type, action_key_down, action_key_up, action_drag_in

**V3 CDP 扩展 (10 个)**:
- set_storage, clear_storage, cdp_send
- fetch_intercept_start/stop
- navigate_with_headers, extract_auth_from_network
- clear_cookies, set_geolocation, set_timezone

**V4 双模式 (9 个)**:
- clear_extra_headers
- cookies_to_http, http_get, http_post, http_close, http_to_browser_cookies
- find_element_shadow, clear_cache, get_navigation_history

**V5 CDP 能力 (7 个)**:
- connect_existing
- get_performance_metrics, get_response_body, set_device_metrics
- get_full_ax_tree, enable_browser_log, get_browser_logs

**V6 Cloudflare 绕过 (4 个)**:
- wait_cloudflare_challenge, extract_cf_clearance, verify_cf_clearance, cookies_to_http_cffi

#### F2. 关键技术实现

**智能点击三级降级** (smart_click):
```
Level 1: wait_for(attached) → wait_stop_moving（动画稳定检测）
Level 2: is_visible → scroll_into_view_if_needed
Level 3: try click() → except → JS el.click() fallback
```

**贝塞尔鼠标轨迹** (_bezier_mouse_path):
```
三阶贝塞尔: P0 → CP1(±15%) → CP2(±15%) → P1
步频: ~50fps
速度抖动: ±20% random jitter
```

**人类打字模拟** (action_type):
```
input_delay=0 → 正态分布 WPM 60~80
avg_delay = 60/(wpm*5) 秒/字符
每字符: gauss(avg, avg*0.35)，下限 20ms
```

**CDPSession 池化** (_get_or_create_cdp):
```
缓存 session._cdp_session
健康检查: Target.getTargetInfo
失败自动重建
```

**Cloudflare 自动处理** (wait_cloudflare_challenge):
```
检测: title 关键词 + DOM 特征 + Turnstile iframe
5s/Managed: 等待 JS 自动完成
Turnstile: 进入 iframe → 点击 checkbox（模拟人类延迟）
轮询: 前 5s 每 1s，之后每 2s
成功标准: title 不含 CF 关键词 + 无 challenge DOM
```

---

## 三、完整复制执行方案

### 阶段 0：环境准备（0.5 天）

| 步骤 | 操作 | 验证 |
|------|------|------|
| 0.1 | 确认目标项目的 Tool 基类接口（Tool/ToolContext/ToolResult） | 读取 .base 模块定义 |
| 0.2 | 确认 config 模块的 `browser_max_sessions` 和 `browser_idle_timeout` 字段 | 读取 .config |
| 0.3 | 确认 `utc_now()` 工具函数位置 | 读取 time_utils |
| 0.4 | 安装依赖: `pip install playwright pydantic httpx curl-cffi` | `playwright install chromium` |

### 阶段 1：基础框架（1 天）

**Step 1.1 — 常量 & JS 脚本**
- 复制 `CONSOLE_INIT_SCRIPT`（第 67~91 行）
- 复制 `ANTI_DETECT_SCRIPT`（第 93~306 行，16 项反检测）
- 注意: JS 中的模板字符串和转义，逐项验证

**Step 1.2 — 数据模型**
- 复制 `DataPacket` dataclass（第 311~335 行）
- 复制 `DownloadMission` dataclass（第 338~348 行）
- 复制 `DrissionBrowserSession` dataclass（第 351~398 行）
- 关键: 所有 V2~V6 新增字段都必须保留（_routes, _extra_headers, _http_auth, _load_mode, _cdp_session, _http_client, _curl_session 等）

**Step 1.3 — 参数模型 DrissionActionParams**
- 复制 Pydantic BaseModel（第 1125~1489 行，365 行）
- 包含所有 100+ action 的 Literal 枚举
- 所有字段的 Field 定义、默认值、描述

### 阶段 2：定位引擎 & 辅助类（1 天）

**Step 2.1 — DrissionLocator**（第 401~589 行）
- `resolve()` — 核心解析（16 种格式判定）
- `to_playwright()` — Playwright 转换
- `_escape()` — XPath 引号 concat 转义
- `_parse_attr_conditions()` — 多属性 AND/OR/NOT
- `_single_attr_xpath()` / `_single_attr_condition()` — 单属性解析
- **测试重点**: 边界用例（含引号的文本、多级属性组合、空字符串）

**Step 2.2 — NetworkListener**（第 594~767 行）
- 事件绑定: `page.on("request"/"response")`
- 过滤逻辑: URL(含正则) + method + resource_type
- 异步队列: asyncio.Queue
- B8 fix: 每个字段独立 try/except 保护

**Step 2.3 — DownloadTracker**（第 771~847 行）
- `page.on("download")` 事件
- `download.save_as()` 持久化
- `wait_for_start()` / `wait_for_done()` 异步等待

### 阶段 3：会话管理器（0.5 天）

**Step 3.1 — DrissionSessionManager**（第 852~1120 行）
- `create_session()` — 三种创建模式（CDP/持久化/标准）
- 反检测注入时序（必须在 Page 创建前通过 `add_init_script`）
- UA 动态清洗（HeadlessChrome → Chrome）
- `_bind_page_handlers()` — dialog/console 事件绑定
- `close_session()` — 全资源释放（CDP/httpx/curl_cffi/context/browser/playwright）
- `_cleanup_idle_sessions()` — 超时清理

**Step 3.2 — 全局单例**
```python
_drission_manager = _get_drission_manager()
```

### 阶段 4：主工具类 — 基础操作（2 天）

**Step 4.1 — 工具框架**
- `BrowserDrissionTool(Tool)` 类定义
- `id` / `description` / `parameters` / `metadata` 属性
- `execute()` dispatch 大表（100+ if-elif）

**Step 4.2 — 内部辅助方法**
- `_get_session()` — 会话获取 + 错误处理
- `_resolve_locator()` — 定位转换
- `_page_or_frame()` — iframe 上下文切换
- `_get_locator()` — Playwright Locator 获取
- `_element_states()` — 六维状态检测（JS evaluate）

**Step 4.3 — 会话管理 Actions**（第 2045~2127 行）
- `_create_session` / `_close_session` / `_list_sessions` / `_setup_download`

**Step 4.4 — 导航 Actions**（第 2133~2225 行）
- `_navigate` — 含 load_mode 映射 + 重试 + CF 首次延迟
- `_nav_go` — back/forward/reload
- `_wait_for_load`

**Step 4.5 — 元素操作 Actions**（第 2231~2609 行）
- find_element / find_elements / get_element_info / get_element_state
- smart_click（三级降级 + wait_stop_moving）
- hover / double_click / right_click / drag_drop
- select_option / upload_file / execute_js / key_press / clear_input

**Step 4.6 — 等待系统 Actions**（第 2615~2752 行）
- wait_for_element（6 种状态: visible/hidden/present/absent/enabled/clickable）
- wait_for_url / wait_for_title（含反向等待 wait_exclude）
- wait_for_network_idle / wait_for_alert

### 阶段 5：主工具类 — 网络/对话框/多标签/数据（1 天）

**Step 5.1 — 网络监听 Actions**（第 2758~2862 行）
**Step 5.2 — 对话框 Actions**（第 2868~2921 行）
**Step 5.3 — IFrame Actions**（第 2927~2979 行）
**Step 5.4 — 多标签页 Actions**（第 2985~3069 行）
**Step 5.5 — 数据提取 Actions**（第 3075~3176 行）
- cookies / storage / console_logs
**Step 5.6 — 截图/PDF/下载/录制/快照**（第 3182~3504 行）
- screenshot / screenshot_element / pdf
- wait_download_start/done / list_downloads
- screencast_start/stop（CDP Page.startScreencast 帧模式）
- snapshot（AI 快照: 主文本 + 交互元素）

### 阶段 6：主工具类 — V2 认证绕过（1 天）

**Step 6.1 — Headers/Auth**（第 3510~3599 行）
- set_extra_headers（context 级，所有 tab 继承）
- set_user_agent（CDP Emulation + JS init_script 双保险）
- set_http_auth（Playwright context.set_http_credentials）

**Step 6.2 — 请求路由**（第 3605~3755 行）
- route_add（abort/add_headers/mock_response/continue 四种模式）
- route_remove / route_list
- set_blocked_urls（预设 ads/trackers/media + 自定义）

**Step 6.3 — Cookie/Token 注入**（第 3761~3902 行）
- inject_cookies_string（多子域智能策略: hostname → .hostname → parent）
- inject_auth_token（context 级 header + cookie 组合注入）

**Step 6.4 — 页面存档/加载模式**（第 3908~4019 行）
- save_mhtml（CDP Page.captureSnapshot）
- get_blob_url（JS fetch + base64）
- set_load_mode（normal/eager/none）

### 阶段 7：主工具类 — P2/P3 补全 + Actions API（1 天）

**Step 7.1 — P2 补全**（第 4391~4712 行）
- scroll（7 方向 + 元素/页面级）
- input / get_html / wait_for_any_element
- click_for_new_tab / click_for_url_change
- find_child / network_listen_steps

**Step 7.2 — P3 Actions API**（第 4027~4385 行）
- 状态管理: `_actions_state()` — {x, y, holding}
- 贝塞尔鼠标: `_bezier_mouse_path()` + `_smooth_move()`
- 10 个 action 方法（move_to/move/click_at/hold/release/scroll_at/type/key_down/key_up/drag_in）

### 阶段 8：主工具类 — V3~V6 高级能力（2 天）

**Step 8.1 — V3 存储 + CDP**（第 4718~4932 行）
- set_storage / clear_storage（localStorage/sessionStorage）
- cdp_send（通用 CDP 接口 + session 池化）
- fetch_intercept_start/stop（CDP Fetch.enable 层拦截）

**Step 8.2 — V3 认证辅助**（第 4938~5128 行）
- navigate_with_headers / extract_auth_from_network
- clear_cookies / set_geolocation / set_timezone

**Step 8.3 — V4 双模式 HTTP**（第 5134~5489 行）
- clear_extra_headers
- cookies_to_http（httpx.AsyncClient 初始化）
- http_get / http_post（curl_cffi 优先 → httpx 降级）
- http_close / http_to_browser_cookies
- find_element_shadow（CDP DOM.performSearch + Shadow DOM）
- clear_cache / get_navigation_history

**Step 8.4 — V5 CDP 能力**（第 5495~5719 行）
- get_performance_metrics / get_response_body
- set_device_metrics / get_full_ax_tree
- enable_browser_log / get_browser_logs

**Step 8.5 — V6 Cloudflare 绕过**（第 5726~6126 行）
- wait_cloudflare_challenge（检测 + 自动处理 5s/Managed/Turnstile）
- extract_cf_clearance（提取 CF cookies + 有效期估算）
- verify_cf_clearance（导航验证）
- cookies_to_http_cffi（curl_cffi TLS 指纹精确模拟）

### 阶段 9：模块导出 & 集成（0.5 天）

**Step 9.1 — 导出**
```python
browser_drission_tool = BrowserDrissionTool()
__all__ = [
    "BrowserDrissionTool", "DrissionLocator", "NetworkListener",
    "DownloadTracker", "DrissionSessionManager", "DrissionActionParams",
    "DataPacket", "DrissionBrowserSession", "browser_drission_tool",
]
```

**Step 9.2 — 注册到工具系统**
- 在目标项目的工具注册表中添加 `browser_drission_tool`
- 确认 `.base.Tool` 的注册机制

### 阶段 10：测试 & 验证（1 天）

| 测试用例 | 验证点 |
|---------|--------|
| 会话创建/销毁 | Playwright 启动 + 反检测注入 |
| 元素定位 | 16 种格式全覆盖 |
| 智能点击 | 三级降级 + wait_stop_moving |
| 网络监听 | Queue + 过滤 + wait_silent |
| 多标签页 | 创建/切换/关闭 + dialog 绑定 |
| Cookie 注入 | 多子域策略 + context 级 |
| CDP 命令 | session 池化 + 自动重建 |
| Fetch 拦截 | headers 注入 + URL 阻断 |
| HTTP 双模式 | httpx + curl_cffi 切换 |
| CF 绕过 | Turnstile 检测 + checkbox 点击 |

---

## 四、关键注意事项

### 4.1 必须保留的 Bug 修复标记
文件中标注了多处历史 Bug 修复（B1~B8, Phase 1~7），**必须全部保留**：

| 标记 | 位置 | 修复内容 |
|------|------|---------|
| B1 | _element_states | CSS vs XPath 策略分别传递给 JS |
| B2 | _page_or_frame | iframe 上下文返回 FrameLocator |
| B4 | smart_click | 不可见时只滚动一次 |
| B5 | _navigate | 读取 session._load_mode 映射 wait_until |
| B6 | close_session | persistent context 无独立 browser 对象 |
| B8 | NetworkListener | 每个字段独立 try/except |
| Phase 1-A | inject_auth_token | context 级 header 注入 |
| Phase 1-B | switch_tab/new_tab | 重置 iframe 上下文 |
| Phase 1-C | create_session | 动态 UA 检测 |
| Phase 1-D | _bind_page_handlers | 统一 dialog 绑定 |
| Phase 3-B | _get_or_create_cdp | CDP session 池化 |
| Phase 3-D | _navigate | 导航失败清理 Fetch 拦截器 |

### 4.2 安全性考虑
- `ANTI_DETECT_SCRIPT` 中 `Object.defineProperty` 覆写是**全局性的**，可能影响页面自身功能
- CDP Fetch 拦截器如果未正确停止会导致页面加载挂起
- httpx/curl_cffi `verify=False` 禁用了 SSL 校验

### 4.3 性能考虑
- `DrissionSessionManager` 默认 max_sessions=5，可根据需要调整
- CDP session 池化避免了高频创建/销毁开销
- NetworkListener 队列最大 500 条响应体（截断 2000 字符）

---

## 五、文件结构建议

```
target_project/
├── tools/
│   ├── __init__.py
│   ├── base.py              ← Tool/ToolContext/ToolResult
│   ├── config.py            ← get_config()
│   └── browser_drission.py  ← 完整复制（5420 行）
├── utils/
│   └── time_utils.py        ← utc_now()
└── requirements.txt         ← playwright, pydantic, httpx, curl-cffi
```

---

## 六、工期估算

| 阶段 | 工期 | 累计 |
|------|------|------|
| 阶段 0: 环境准备 | 0.5 天 | 0.5 天 |
| 阶段 1: 基础框架 | 1 天 | 1.5 天 |
| 阶段 2: 定位引擎 & 辅助类 | 1 天 | 2.5 天 |
| 阶段 3: 会话管理器 | 0.5 天 | 3 天 |
| 阶段 4: 基础操作 | 2 天 | 5 天 |
| 阶段 5: 网络/标签/数据 | 1 天 | 6 天 |
| 阶段 6: V2 认证绕过 | 1 天 | 7 天 |
| 阶段 7: P2/P3 Actions API | 1 天 | 8 天 |
| 阶段 8: V3~V6 高级能力 | 2 天 | 10 天 |
| 阶段 9: 导出 & 集成 | 0.5 天 | 10.5 天 |
| 阶段 10: 测试 & 验证 | 1 天 | **11.5 天** |

**总计约 12 个工作日**（如果是直接文件复制+适配导入路径，可压缩到 2~3 天）。
