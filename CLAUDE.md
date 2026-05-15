# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development

```bash
# Build the Go binary
make build          # builds with gf build -ew

# Regenerate controller/sdk from API definitions
make ctrl           # gf gen ctrl

# Generate DAO/DO/Entity from database
make dao            # gf gen dao

# Docker image build
make image          # build Docker image
make image.push     # build and push to registry
```

**Note:** The build system requires the GoFrame CLI (`gf`). `make` targets check/install it automatically.

## Running

```bash
# Start Milvus infrastructure (vector DB, etcd, MinIO, Attu GUI)
cd manifest/docker && docker compose up -d

# Start the Go backend (listens on port 6872)
go run main.go

# Start frontend dev server (listens on port 8080)
cd SuperBizAgentFrontend && python3 -m http.server 8080
```

Attu (Milvus GUI) is available at `http://localhost:8000` when Docker services are up.

## Architecture

### Tech Stack
- **Backend:** Go 1.24 + GoFrame v2 (HTTP framework) + CloudWeGo Eino (AI agent orchestration)
- **Frontend:** Vanilla JS SPA served via simple HTTP server
- **Vector DB:** Milvus 2.5 (with etcd + MinIO for storage)
- **LLM:** DeepSeek V3 via Volcano Engine ark API (OpenAI-compatible endpoint); also supports Alibaba DashScope for embeddings

### Request Flow

```
HTTP request → GoFrame router → CORSMiddleware → ResponseMiddleware → Controller → Eino Agent Pipeline → LLM + Tools
```

**Controllers** (`internal/controller/chat/`) implement the API interface defined in `api/chat/`. GoFrame auto-binds routes from struct tags in `api/chat/v1/chat.go`.

**ResponseMiddleware** wraps all responses in `{"message": "...", "data": ...}` format.

### AI Agent Pipelines (Eino Graphs)

All agent logic lives under `internal/ai/agent/` and uses Eino's graph composition pattern (`compose.NewGraph` → `AddNode` → `AddEdge` → `Compile`).

1. **Chat Pipeline** (`chat_pipeline/`): RAG + ReAct Agent
   - Flow: `UserMessage → [RAG branch (embed → Milvus retrieve)] + [Chat branch] → ChatTemplate → ReAct Agent → Response`
   - The ReAct agent has 5 tools: log query (MCP), Prometheus alerts, MySQL CRUD, current time, internal docs search
   - Supports both invoke (non-streaming) and stream modes

2. **Knowledge Index Pipeline** (`knowledge_index_pipeline/`): Document ingestion
   - Flow: `FileLoader → MarkdownSplitter → MilvusIndexer`
   - Triggered on file upload; de-duplicates by `_source` metadata before indexing

3. **Plan-Execute-Replan** (`plan_execute_replan/`): Multi-step AIOps analysis
   - Planner → Executor → Replanner loop (max 20 iterations)
   - Persistent execution via file-based `TaskStore` (stored in `.ai_ops_tasks/`)
   - `BuildPlanAgent()` uses the Eino ADK's `planexecute` prebuilt; `BuildPersistentPlanAgent()` uses a custom runner with file-backed state supporting resume

### Tools (available to the ReAct agent)

| Tool | File | Description |
|------|------|-------------|
| `get_current_time` | `tools/get_current_time.go` | Returns current Unix time in multiple formats |
| `query_prometheus_alerts` | `tools/query_metrics_alerts.go` | Queries Prometheus Alerts API (currently returns empty — API call is short-circuited with `return ... nil` on line 54) |
| `query_internal_docs` | `tools/query_internal_docs.go` | RAG search via Milvus retriever over indexed documents |
| `mysql_crud` | `tools/mysql_crud.go` | Executes SQL against MySQL via GORM (requires human confirmation via stdin) |
| MCP log tools | `tools/query_log.go` | Dynamically loaded from Tencent Cloud MCP SSE endpoint |

### Configuration

- `manifest/config/config.yaml` — server port, LLM API keys/URLs/models, `file_dir` (knowledge doc path), `mcp_url`
- `hack/config.yaml` — GoFrame CLI config (code generation settings, Docker image prefix)
- LLM model configs use GoFrame config paths: `ds_think_chat_model.*`, `ds_quick_chat_model.*`, `doubao_embedding_model.*`

### In-Memory Conversation Store

`utility/mem/mem.go` — keeps conversation history per client ID with a sliding window (default 6 messages). Both user and assistant messages are stored for context in subsequent requests.

### API Endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/chat` | Non-streaming chat with RAG |
| POST | `/api/chat_stream` | Streaming chat via SSE |
| POST | `/api/upload` | File upload → knowledge base indexing |
| POST | `/api/ai_ops` | AIOps analysis (plan-execute-replan) |

### Directory Layout

```
api/                    # API interface + request/response types (GoFrame auto-routing tags)
internal/
  ai/
    agent/              # Eino agent graph definitions (chat, indexing, plan-execute)
    cmd/                # Standalone CLI entry points for testing agents
    embedder/           # Embedding model (DashScope)
    indexer/            # Milvus indexer
    loader/             # Document loader (PDF, Markdown, etc.)
    models/             # LLM chat model wrappers (DeepSeek)
    retriever/          # Milvus retriever
    tools/              # AI tools (see table above)
  controller/           # HTTP request handlers
  logic/                # Business logic (chat, SSE service)
utility/                # Shared utilities (Milvus client, middleware, mem store, consts)
manifest/
  config/config.yaml    # Application config
  docker/               # Docker Compose + Dockerfile for Milvus infrastructure
hack/                   # Makefile + GoFrame CLI config
SuperBizAgentFrontend/  # Frontend (vanilla HTML/CSS/JS, served separately)
```

### Key Dependency: CloudWeGo Eino

Eino is the Go equivalent of LangChain. Key patterns:
- **Graph composition:** `compose.NewGraph[Input, Output]()` with typed nodes and edges
- **Node types:** Lambda, ChatTemplate, ChatModel, Retriever, Loader, Indexer, DocumentTransformer
- **Callbacks:** `compose.WithCallbacks(log_call_back.LogCallback(nil))` for logging
- **Streaming:** `runner.Stream(ctx, input)` returns a `StreamReader`
- **ADK:** Higher-level agent constructs (`planexecute`, `adk.Runner`, `adk.Message`)
