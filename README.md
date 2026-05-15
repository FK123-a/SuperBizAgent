# SuperBizAgent

基于 GoFrame + CloudWeGo Eino 的 AI 驱动业务运维智能体平台，支持 RAG 知识库问答、ReAct Agent 工具调用、AIOps 多步骤分析。

## 技术栈

| 层级 | 技术 |
|------|------|
| **后端框架** | Go 1.24 + GoFrame v2.10 |
| **AI Agent 编排** | CloudWeGo Eino v0.6（Graph/ADK） |
| **LLM** | DeepSeek V3（火山引擎 Ark API，OpenAI 兼容） |
| **Embedding** | 阿里云 DashScope text-embedding-v4 |
| **向量数据库** | Milvus 2.5（etcd + MinIO） |
| **前端** | 原生 HTML/CSS/JS SPA |
| **MCP 工具** | Tencent Cloud MCP SSE（日志查询） |

## 项目结构

```
SuperBizAgent/
├── api/                          # API 接口定义（GoFrame 自动路由）
│   └── chat/v1/                  # 请求/响应类型 + 路由标签
├── internal/
│   ├── ai/
│   │   ├── agent/                # Eino Agent 图定义
│   │   │   ├── chat_pipeline/    # RAG + ReAct 对话流水线
│   │   │   ├── knowledge_index_pipeline/  # 文档索引流水线
│   │   │   └── plan_execute_replan/       # AIOps 计划-执行-重规划
│   │   ├── cmd/                  # 独立 CLI 测试入口
│   │   ├── embedder/             # 向量嵌入（DashScope）
│   │   ├── indexer/              # Milvus 索引器
│   │   ├── loader/               # 文档加载器
│   │   ├── models/               # LLM ChatModel 封装
│   │   ├── retriever/            # Milvus 检索器
│   │   └── tools/                # Agent 工具集
│   ├── controller/chat/          # HTTP 控制器
│   └── logic/                    # 业务逻辑层
├── utility/                      # 工具库（Milvus 客户端、中间件、内存存储）
├── manifest/
│   ├── config/config.yaml        # 应用配置（LLM Key/URL、端口等）
│   └── docker/                   # Docker Compose（Milvus 基础设施）
├── SuperBizAgentFrontend/        # 前端 SPA
└── hack/                         # 构建脚本 + GoFrame CLI 配置
```

## 快速开始

### 1. 启动基础设施

```bash
cd manifest/docker
docker compose up -d
```

启动 Milvus、etcd、MinIO，Attu 管理界面在 http://localhost:8000。

### 2. 配置

编辑 `manifest/config/config.yaml`，填入 LLM API Key：

```yaml
ds_think_chat_model:
  api_key: "your-volcano-engine-api-key"
  base_url: "https://ark.cn-beijing.volces.com/api/v3"
  model: "deepseek-v3-2-251201"

doubao_embedding_model:
  api_key: "your-dashscope-api-key"
  base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
  model: "text-embedding-v4"
```

### 3. 启动后端

```bash
go run main.go
# 服务监听 :6872
```

### 4. 启动前端（可选）

```bash
cd SuperBizAgentFrontend
python3 -m http.server 8080
```

## API 端点

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/chat` | 非流式对话（RAG 增强） |
| POST | `/api/chat_stream` | SSE 流式对话 |
| POST | `/api/upload` | 文件上传 → 知识库索引 |
| POST | `/api/ai_ops` | AIOps 智能分析（计划-执行-重规划） |

## Agent 工具

| 工具 | 功能 |
|------|------|
| `get_current_time` | 获取当前时间（多格式） |
| `query_prometheus_alerts` | 查询 Prometheus 告警 |
| `query_internal_docs` | RAG 搜索内部知识库文档 |
| `mysql_crud` | MySQL 数据库 CRUD 操作 |
| MCP 日志工具 | 通过 Tencent Cloud MCP 查询日志 |

## 构建

```bash
make build    # 编译 Go 二进制
make ctrl     # 从 API 定义生成 Controller
make dao      # 从数据库生成 DAO/DO/Entity
make image    # 构建 Docker 镜像
```

## AIOps 工作流

Plan-Execute-Replan 管道支持多步骤 AIOps 分析：

```
用户输入 → Planner（生成计划） → Executor（逐步执行） → Replanner（评估 & 调整） → 循环
                                                                          ↑_______________|
```

任务状态持久化在 `.ai_ops_tasks/` 目录，支持中断恢复。默认最多 20 轮迭代。

## 依赖

- Go 1.24+
- Docker & Docker Compose
- GoFrame CLI (`gf`) — 构建时自动检查安装
