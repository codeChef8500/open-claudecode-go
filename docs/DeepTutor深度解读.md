# DeepTutor 深度解读报告

## 一、项目概述

**DeepTutor** 是由香港大学数据智能实验室 (HKUDS) 开发的一款 **Agent-Native（智能体原生）个性化学习助手**。截至 2026 年 4 月，该项目已在 GitHub 获得 **15.3k Stars** 和 **2k Forks**，成为教育 AI 领域的热门开源项目。

### 核心理念
DeepTutor 不仅仅是一个 AI 聊天工具，而是一个完整的**个性化学习生态系统**。它将 AI 智能体深度融入学习过程的每个环节，从知识管理到主动辅导，从协作写作到深度研究。

---

## 二、技术架构

### 2.1 技术栈

| 层级 | 技术选型 |
|------|----------|
| **后端** | Python 3.11+ / FastAPI |
| **前端** | Next.js 16 / React 19 / TypeScript |
| **Agent 引擎** | nanobot (HKUDS 自研超轻量级智能体框架) |
| **RAG 引擎** | LlamaIndex |
| **部署** | Docker / Docker Compose |

### 2.2 系统架构总览

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                               DeepTutor                                      │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                         用户交互层 (User Interface)                  │    │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐ │    │
│  │  │  Web UI     │  │    CLI      │  │  TutorBot   │  │   API       │ │    │
│  │  │  (Next.js)  │  │  (Python)   │  │ (Multi-chan)│  │  (REST/WS)  │ │    │
│  │  │   :3782     │  │             │  │             │  │   :8001     │ │    │
│  │  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘ │    │
│  └─────────┼────────────────┼────────────────┼────────────────┼────────┘    │
│            │                │                │                │              │
│  ┌─────────┴────────────────┴────────────────┴────────────────┴────────┐    │
│  │                           FastAPI Backend                            │    │
│  │  ┌────────────────────────────────────────────────────────────────┐  │    │
│  │  │                    核心编排层 (Core Orchestration)              │  │    │
│  │  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │  │    │
│  │  │  │   Capability │  │    Plugin    │  │   Context    │         │  │    │
│  │  │  │   Manager    │  │   Registry   │  │   Manager    │         │  │    │
│  │  │  └──────────────┘  └──────────────┘  └──────────────┘         │  │    │
│  │  └────────────────────────────────────────────────────────────────┘  │    │
│  │                                    │                                   │    │
│  │  ┌─────────────────────────────────┴───────────────────────────────┐  │    │
│  │  │                     Capabilities (功能能力)                       │  │    │
│  │  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌───┐ │  │    │
│  │  │  │   Chat   │  │Deep Solve│  │  Quiz    │  │   Deep   │  │Mat│ │  │    │
│  │  │  │          │  │          │  │ Generation│  │ Research │  │hAn│ │  │    │
│  │  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘  └───┘ │  │    │
│  │  └─────────────────────────────────────────────────────────────────┘  │    │
│  │                                    │                                   │    │
│  │  ┌─────────────────────────────────┴───────────────────────────────┐  │    │
│  │  │                        Tools (工具层)                             │  │    │
│  │  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │  │    │
│  │  │  │   RAG    │  │   Web    │  │  Code    │  │  Brain   │        │  │    │
│  │  │  │  Tool    │  │  Search  │  │ Executor │  │ storm    │        │  │    │
│  │  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘        │  │    │
│  │  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐        │  │    │
│  │  │  │ Reason   │  │  Paper   │  │ Vision   │  │ Question │        │  │    │
│  │  │  │          │  │  Search  │  │          │  │          │        │  │    │
│  │  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘        │  │    │
│  │  └─────────────────────────────────────────────────────────────────┘  │    │
│  └───────────────────────────────────────────────────────────────────────┘    │
│                                      │                                        │
│  ┌───────────────────────────────────┴────────────────────────────────────┐    │
│  │                          服务层 (Services)                              │    │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────┐  │    │
│  │  │   LLM    │  │Embedding │  │  Search  │  │   RAG    │  │Memory  │  │    │
│  │  │ Service  │  │ Service  │  │ Service  │  │ Service  │  │Service │  │    │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘  └────────┘  │    │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌────────┐  │    │
│  │  │  Session │  │Notebook  │  │Knowledge │  │TutorBot  │  │Prompt  │  │    │
│  │  │ Service  │  │ Service  │  │ Service  │  │ Service  │  │Service │  │    │
│  │  └──────────┘  └──────────┘  └──────────┘  └──────────┘  └────────┘  │    │
│  └───────────────────────────────────────────────────────────────────────┘    │
│                                      │                                        │
│  ┌───────────────────────────────────┴────────────────────────────────────┐    │
│  │                        AI Providers (多提供商)                          │    │
│  │  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌──────────┐  │    │
│  │  │OpenAI  │ │Anthropic│ │DeepSeek│ │DashScope│ │ Ollama │ │ 20+ More│  │    │
│  │  └────────┘ └────────┘ └────────┘ └────────┘ └────────┘ └──────────┘  │    │
│  └───────────────────────────────────────────────────────────────────────┘    │
│                                                                              │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

### 2.3 详细模块架构

#### 2.3.1 后端模块结构 (`deeptutor/`)

```
deeptutor/
├── api/                    # FastAPI 服务层
│   ├── routers/            # API 路由
│   │   ├── chat.py         # 聊天 API
│   │   ├── knowledge.py    # 知识库 API
│   │   ├── session.py      # 会话 API
│   │   └── ...
│   ├── utils/              # API 工具
│   ├── main.py             # FastAPI 应用入口
│   └── run_server.py       # 服务启动脚本
│
├── core/                   # 核心协议与抽象
│   ├── capability_protocol.py   # 能力协议 (Capability 接口)
│   ├── tool_protocol.py         # 工具协议 (Tool 接口)
│   ├── context.py               # 上下文管理
│   ├── stream.py                # 流式响应
│   ├── stream_bus.py            # 事件总线
│   ├── errors.py                # 错误定义
│   └── trace.py                 # 链路追踪
│
├── capabilities/           # 能力实现 (5大模式)
│   ├── chat.py             # 基础对话能力
│   ├── deep_solve.py       # 深度问题解决 (多Agent)
│   ├── deep_question.py    # 题目生成
│   ├── deep_research.py    # 深度研究
│   ├── math_animator.py    # 数学动画生成
│   └── request_contracts.py # 请求契约
│
├── tools/                  # 工具集
│   ├── builtin/            # 内置工具
│   │   ├── __init__.py
│   │   └── ...
│   ├── prompting/          # 提示词工具
│   ├── question/           # 问答相关工具
│   ├── vision/             # 视觉理解工具
│   ├── rag_tool.py         # RAG 检索工具
│   ├── web_search.py       # 网页搜索工具
│   ├── code_executor.py    # 代码执行工具
│   ├── brainstorm.py       # 头脑风暴工具
│   ├── reason.py           # 推理工具
│   ├── paper_search_tool.py # 论文搜索工具
│   ├── tex_chunker.py      # LaTeX 分块工具
│   └── tex_downloader.py   # LaTeX 下载工具
│
├── services/               # 业务服务层
│   ├── llm/                # LLM 服务
│   │   ├── __init__.py
│   │   ├── base.py         # LLM 基类
│   │   ├── openai.py       # OpenAI 提供商
│   │   ├── anthropic.py    # Anthropic 提供商
│   │   └── ...
│   ├── embedding/          # Embedding 服务
│   ├── config/             # 配置服务
│   ├── memory/             # 记忆服务
│   ├── notebook/           # 笔记本服务
│   ├── prompt/             # 提示词管理
│   ├── rag/                # RAG 流水线
│   ├── search/             # 搜索服务
│   ├── session/            # 会话管理
│   ├── settings/           # 设置管理
│   ├── setup/              # 初始化设置
│   ├── tutorbot/           # TutorBot 服务
│   ├── provider_registry.py # 提供商注册表
│   └── path_service.py     # 路径服务
│
├── knowledge/              # 知识管理
│   ├── manager.py          # 知识库管理器
│   ├── initializer.py      # 知识库初始化
│   ├── add_documents.py    # 文档添加
│   └── progress_tracker.py # 进度追踪
│
├── tutorbot/               # TutorBot 核心
│   ├── __init__.py
│   ├── ...
│
├── agents/                 # Agent 相关
│   ├── __init__.py
│   └── ...
│
├── app/                    # 应用入口
│   ├── __init__.py
│   └── ...
│
├── runtime/                # 运行时
│   ├── __init__.py
│   └── ...
│
├── events/                 # 事件系统
│   ├── __init__.py
│   └── ...
│
├── logging/                # 日志系统
│   ├── __init__.py
│   └── ...
│
├── config/                 # 配置
│   ├── __init__.py
│   └── ...
│
├── utils/                  # 工具函数
│   ├── __init__.py
│   └── ...
│
├── __init__.py             # 包初始化
├── __main__.py             # 入口点
```

#### 2.3.2 前端模块结构 (`web/`)

```
web/
├── src/
│   ├── app/                # Next.js App Router
│   │   ├── page.tsx        # 首页
│   │   ├── layout.tsx      # 布局
│   │   └── ...
│   ├── components/         # React 组件
│   │   ├── Chat/           # 聊天组件
│   │   ├── Knowledge/      # 知识库组件
│   │   ├── GuidedLearning/ # 引导学习组件
│   │   ├── CoWriter/       # 协作者组件
│   │   └── ...
│   ├── hooks/              # 自定义 Hooks
│   ├── lib/                # 工具库
│   ├── stores/             # 状态管理
│   ├── styles/             # 样式
│   └── ...
├── public/                 # 静态资源
├── package.json
├── next.config.js
└── ...
```

#### 2.3.3 CLI 模块结构 (`deeptutor_cli/`)

```
deeptutor_cli/
├── src/
│   ├── __init__.py
│   ├── main.py             # CLI 入口
│   ├── commands/           # 命令实现
│   │   ├── chat.py         # 聊天命令
│   │   ├── run.py          # 运行命令
│   │   ├── bot.py          # TutorBot 命令
│   │   ├── kb.py           # 知识库命令
│   │   ├── session.py      # 会话命令
│   │   └── ...
│   └── ...
└── ...
```

---

### 2.4 核心模块详解

#### 2.4.1 API 层 (`deeptutor/api/`)

| 文件 | 功能描述 |
|------|----------|
| `main.py` | FastAPI 应用主入口，定义所有路由和中间件 |
| `run_server.py` | 服务启动脚本，支持 uvicorn |
| `routers/chat.py` | 聊天相关 API（发送消息、流式响应） |
| `routers/knowledge.py` | 知识库管理 API（创建、查询、删除） |
| `routers/session.py` | 会话管理 API（创建、恢复、列表） |
| `routers/tutorbot.py` | TutorBot 管理 API |
| `utils/` | API 辅助工具（认证、验证、响应格式化） |

#### 2.4.2 核心协议层 (`deeptutor/core/`)

**Capability Protocol (能力协议)**

```python
# capability_protocol.py
class Capability(ABC):
    """所有能力(Capability)的基类"""
    
    @property
    @abstractmethod
    def name(self) -> str:
        """能力名称"""
        pass
    
    @property
    @abstractmethod
    def description(self) -> str:
        """能力描述"""
        pass
    
    @abstractmethod
    async def execute(self, context: Context, **kwargs) -> AsyncIterator[StreamEvent]:
        """执行能力"""
        pass
    
    @abstractmethod
    def get_tools(self) -> list[Tool]:
        """获取该能力可用的工具列表"""
        pass
```

**Tool Protocol (工具协议)**

```python
# tool_protocol.py
class Tool(ABC):
    """所有工具(Tool)的基类"""
    
    @property
    @abstractmethod
    def name(self) -> str:
        """工具名称"""
        pass
    
    @property
    @abstractmethod
    def description(self) -> str:
        """工具描述"""
        pass
    
    @property
    @abstractmethod
    def parameters(self) -> dict:
        """JSON Schema 格式的参数定义"""
        pass
    
    @abstractmethod
    async def execute(self, **kwargs) -> Any:
        """执行工具"""
        pass
```

**Context Manager (上下文管理)**

```python
# context.py
class Context:
    """统一的上下文管理"""
    
    def __init__(self):
        self.messages: list[Message] = []      # 消息历史
        self.tools: list[Tool] = []             # 可用工具
        self.capability: str = "chat"          # 当前能力
        self.knowledge_bases: list[str] = []   # 知识库列表
        self.memory: Memory = None              # 记忆引用
        self.notebook: Notebook = None          # 笔记本引用
```

#### 2.4.3 Capabilities 层 (功能能力)

| 能力 | 文件 | 功能 |
|------|------|------|
| **Chat** | `chat.py` | 基础对话，支持 RAG、搜索、代码执行等工具 |
| **Deep Solve** | `deep_solve.py` | 多Agent问题解决：Plan → Investigate → Solve → Verify |
| **Quiz Generation** | `deep_question.py` | 基于知识库生成测验题 |
| **Deep Research** | `deep_research.py` | 主题分解 + 并行研究 + 报告生成 |
| **Math Animator** | `math_animator.py` | 使用 Manim 生成数学动画 |

#### 2.4.4 Tools 层 (工具集)

| 工具 | 文件 | 功能描述 |
|------|------|----------|
| **RAG Tool** | `rag_tool.py` | 知识库检索，返回相关文档片段 |
| **Web Search** | `web_search.py` | 网页搜索（支持 Brave/Tavily/Jina/DuckDuckGo） |
| **Code Executor** | `code_executor.py` | Python 代码沙箱执行 |
| **Reason** | `reason.py` | 深度推理工具 |
| **Brainstorm** | `brainstorm.py` | 头脑风暴工具 |
| **Paper Search** | `paper_search_tool.py` | 学术论文搜索 |
| **Vision** | `vision/` | 图像理解与描述 |
| **Question** | `question/` | 问答相关工具 |
| **Tex Chunker** | `tex_chunker.py` | LaTeX 文档分块 |
| **Tex Downloader** | `tex_downloader.py` | LaTeX 论文下载 |

#### 2.4.5 Services 层 (业务服务)

| 服务 | 目录 | 功能 |
|------|------|------|
| **LLM Service** | `services/llm/` | 统一 LLM 调用接口，支持 20+ 提供商 |
| **Embedding Service** | `services/embedding/` | 向量化服务 |
| **Search Service** | `services/search/` | 搜索服务封装 |
| **RAG Service** | `services/rag/` | RAG 流水线（文档加载 → 分块 → 索引 → 检索） |
| **Memory Service** | `services/memory/` | 用户记忆管理（Summary + Profile） |
| **Session Service** | `services/session/` | 会话持久化 |
| **Notebook Service** | `services/notebook/` | 笔记本管理 |
| **Knowledge Service** | `services/knowledge/` | 知识库 CRUD |
| **TutorBot Service** | `services/tutorbot/` | TutorBot 生命周期管理 |
| **Prompt Service** | `services/prompt/` | 提示词模板管理 |
| **Provider Registry** | `provider_registry.py` | AI 提供商注册表 |

---

## 三、核心功能详解

### 3.1 统一聊天工作区 (Unified Chat Workspace)

DeepTutor 的核心是一个 **5 种模式共存** 的统一工作区，所有模式共享上下文：

| 模式 | 能力名 | 功能描述 |
|------|--------|----------|
| **Chat** | `chat` | 流畅的工具增强对话，支持 RAG 检索、网页搜索、代码执行、深度推理等 |
| **Deep Solve** | `deep_solve` | 多智能体问题解决：计划 → 调查 → 解决 → 验证，每步都有精确的引用来源 |
| **Quiz Generation** | `deep_question` | 基于知识库生成测验题，支持内置验证 |
| **Deep Research** | `deep_research` | 将主题分解为子主题，并行调度研究智能体，生成完整引用报告 |
| **Math Animator** | `math_animator` | 使用 Manim 将数学概念转化为可视化动画和故事板 |

**工作流程示例**：
```
提问 → 升级到 Deep Solve（变难）→ 生成 Quiz（自测）→ 启动 Deep Research（深入）
```

### 3.2 TutorBot 个性化 AI 导师

TutorBot 是 DeepTutor 的核心创新 —— **不是聊天机器人，而是持久自治的智能体导师**。

**架构特点**：
- **独立工作空间**：每个 Bot 有自己的目录，包含独立记忆、会话、技能和配置
- **Soul Templates**：通过可编辑的 Soul 文件定义人格、语气、教学理念
- **Proactive Heartbeat**：主动心跳系统，支持定期学习检查、复习提醒、定时任务
- **多渠道接入**：支持 Telegram、Discord、Slack、飞书、企业微信、钉钉、邮件等
- **Skill Learning**：通过添加技能文件让 Bot 学习新能力
- **团队与子智能体**：可生成后台子智能体或多智能体团队

```bash
# 创建示例
deeptutor bot create math-tutor --persona "苏格拉底式数学老师"
deeptutor bot create writing-coach --persona "耐心的写作导师"
```

### 3.3 AI Co-Writer 协作者

- 完整的 Markdown 编辑器，AI 是一等公民
- 选中文本 → 重写/扩展/缩短
- 可选从知识库或网络获取上下文
- 非破坏性编辑，支持撤销/重做
- 内容可直接保存到笔记本

### 3.4 Guided Learning 引导式学习

将个人材料转化为结构化、可视化的学习旅程：

1. **设计学习计划** - 从材料中识别 3-5 个递进知识点
2. **生成交互页面** - 每个知识点变成丰富的 HTML 页面
3. **Contextual Q&A** - 每步都可讨论深入
4. **总结进度** - 完成时接收学习总结

### 3.5 Knowledge Hub 知识管理

- **知识库**：上传 PDF、TXT、Markdown 文件，创建可搜索的 RAG 集合
- **笔记本**：跨会话组织学习记录，按颜色分类

### 3.6 Persistent Memory 持久记忆

两个维度：
- **Summary**：学习进度的运行摘要
- **Profile**：学习者身份（偏好、知识水平、目标、沟通风格）

跨所有功能和 TutorBot 共享。

---

## 四、CLI 深度使用

### 4.1 一键执行

```bash
deeptutor run chat "Explain the Fourier transform" -t rag --kb textbook
deeptutor run deep_solve "Prove that √2 is irrational" -t reason
deeptutor run deep_question "Linear algebra" --config num_questions=5
deeptutor run deep_research "Attention mechanisms in transformers"
```

### 4.2 交互式 REPL

```bash
deeptutor chat --capability deep_solve --kb my-kb
# 内部命令: /cap, /tool, /kb, /history, /notebook, /config
```

### 4.3 知识库管理

```bash
deeptutor kb create my-kb --doc textbook.pdf
deeptutor kb add my-kb --docs-dir ./papers/
deeptutor kb search my-kb "gradient descent"
deeptutor kb set-default my-kb
```

---

## 五、LLM 提供商支持

DeepTutor 支持 **20+** LLM/Embedding 提供商：

| 提供商 | Binding | 特点 |
|--------|---------|------|
| OpenAI | `openai` | 默认 |
| Anthropic | `anthropic` | Claude 系列 |
| DeepSeek | `deepseek` | 国产高性价比 |
| Ollama | `ollama` | 本地部署 |
| DashScope | `dashscope` | 阿里 Qwen |
| Azure OpenAI | `azure_openai` | 企业级 |
| Groq | `groq` | 高速推理 |
| MiniMax | `minimax` | 国产 |
| Moonshot | `moonshot` | Kimi |
| Zhipu AI | `zhipu` | 智谱 GLM |
| ... | ... | ... |

---

## 六、部署方式

### 6.1 本地开发 (推荐)

```bash
git clone https://github.com/HKUDS/DeepTutor.git
cd DeepTutor
conda create -n deeptutor python=3.11 && conda activate deeptutor

# 启动引导 tour
python scripts/start_tour.py
```

### 6.2 Docker 部署

```bash
# 方式一：拉取官方镜像
docker compose -f docker-compose.ghcr.yml up -d

# 方式二：本地构建
docker compose up -d
```

### 6.3 自定义端口

```bash
# .env 中配置
BACKEND_PORT=9001
FRONTEND_PORT=4000
```

---

## 七、数据流与交互流程

### 7.1 典型对话流程

```
┌─────────┐     ┌──────────┐     ┌─────────────┐     ┌──────────┐     ┌─────────┐
│  User   │────▶│  Web/CLI │────▶│   FastAPI   │────▶│Capability│────▶│  LLM    │
│ Input   │     │  Layer   │     │   Backend   │     │  Layer   │     │ Provider│
└─────────┘     └──────────┘     └─────────────┘     └──────────┘     └─────────┘
                                              │                              │
                                              ▼                              │
                                      ┌─────────────┐                       │
                                      │    Tools    │◀──────────────────────┘
                                      │   Layer     │
                                      │ ┌─────────┐ │
                                      │ │   RAG   │ │
                                      │ │ Search  │ │
                                      │ │  Code   │ │
                                      │ │  ...    │ │
                                      │ └─────────┘ │
                                      └─────────────┘
                                              │
                                              ▼
                                      ┌─────────────┐
                                      │  Response   │
                                      │   Stream    │
                                      └─────────────┘
```

### 7.2 RAG 流水线

```
┌──────────┐     ┌─────────────┐     ┌────────────┐     ┌─────────────┐
│  Upload  │────▶│   Parser    │────▶│  Chunker   │────▶│  Embedding  │
│ Document │     │  (PDF/MD)   │     │ (split)    │     │  (Vector)   │
└──────────┘     └─────────────┘     └────────────┘     └─────────────┘
                                                              │
                                                              ▼
┌──────────┐     ┌─────────────┐     ┌────────────┐     ┌─────────────┐
│  Answer  │◀────│   Merge     │◀────│  Rerank    │◀────│   Search    │
│  Generate│     │   Context   │     │  (Optional)│     │  (Vector DB)│
└──────────┘     └─────────────┘     └────────────┘     └─────────────┘
```

---

## 八、Roadmap (未来规划)

| 状态 | 功能 |
|------|------|
| 🔜 | 认证与登录 - 公共部署的多用户支持 |
| 🔜 | 主题与外观 - 多样化主题选项 |
| 🔜 | LightRAG 集成 - 高级知识库引擎 |
| 🔜 | 文档站点 - 完整使用指南和 API 参考 |

---

## 九、评价与总结

### 优点
1. **Agent 原生设计**：从架构层面支持智能体，区别于简单 chatbot
2. **功能完整**：覆盖学习全流程（知识管理 → 学习 → 练习 → 评估）
3. **多模态输出**：Chat、Quiz、Research、Math Animation 多种形式
4. **多渠道 TutorBot**：真正可部署的个性化 AI 导师
5. **优秀的技术架构**：插件化设计、上下文共享、多提供商支持
6. **代码结构清晰**：模块化设计，易于扩展和维护

### 适用场景
- 个人学习助手
- 教育机构 AI 辅导
- 企业内部知识管理
- AI Agent 开发参考

---

*文档生成时间: 2026-04-10*
*项目地址: https://github.com/HKUDS/DeepTutor*