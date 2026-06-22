# WeKnora 本地开发启动清单（Qdrant + MinIO + Docker DocReader + Remote Ollama）

本文档对应当前本地开发标准方案：

- 基础设施使用 Docker
- 后端 `cmd/server` 在宿主机本地运行
- 前端 `frontend` 在宿主机本地运行
- 检索库使用 `Qdrant`
- 文件存储使用 `MinIO`
- 文档解析使用 Docker `DocReader`
- 模型使用远程 Ollama `http://192.168.1.126:11435`

适用目标：

- 在一台新的开发机器上，直接按步骤把 WeKnora 跑起来
- 保持和当前开发环境一致
- 可以直接在 VS Code 中调试后端和前端

## 1. 环境要求

先确认本机安装了以下工具。

```bash
go version
node -v
npm -v
docker compose version
make -v
git --version
curl --version
```

推荐版本：

- Go: `1.26.0`
- Node.js: `22.x`
- npm: 跟随 Node 22
- Docker Compose: `v2`

可选但推荐：

```bash
go install github.com/air-verse/air@latest
```

说明：

- 安装 `air` 后，`make dev-app` 可以自动热重载
- 不安装也能正常开发

## 2. 本地开发架构

本方案启动后，实际运行方式如下：

- Docker 容器
  - `WeKnora-postgres-dev`
  - `WeKnora-redis-dev`
  - `WeKnora-docreader-dev`
  - `WeKnora-qdrant-dev`
  - `WeKnora-minio-dev`
- 宿主机本地进程
  - Go 后端：`http://localhost:8080`
  - Vite 前端：`http://localhost:5173`
- 远程模型服务
  - Ollama：`http://192.168.1.126:11435`

## 3. 获取代码

```bash
git clone <你的仓库地址>
cd WeKnora
```

## 4. 创建本地开发环境变量

优先直接从模板复制：

```bash
cp .env.local-dev.example .env
```

如果你想手动创建，也可以在项目根目录执行：

```bash
cat > .env <<'EOF'
GIN_MODE=debug
LOG_LEVEL=debug
TZ=Asia/Shanghai

DISABLE_REGISTRATION=false

DB_DRIVER=postgres
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres123!@#
DB_NAME=WeKnora

RETRIEVE_DRIVER=qdrant
QDRANT_HOST=localhost
QDRANT_PORT=6334
QDRANT_REST_PORT=6333
QDRANT_COLLECTION=weknora_embeddings
QDRANT_USE_TLS=false

STORAGE_TYPE=minio
MINIO_ENDPOINT=localhost:9000
MINIO_PORT=9000
MINIO_CONSOLE_PORT=9001
MINIO_ACCESS_KEY_ID=minioadmin
MINIO_SECRET_ACCESS_KEY=minioadmin
MINIO_BUCKET_NAME=weknora-files
MINIO_USE_SSL=false

STREAM_MANAGER_TYPE=redis
REDIS_ADDR=localhost:6379
REDIS_PORT=6379
REDIS_PASSWORD=redis123!@#
REDIS_DB=0
REDIS_PREFIX=stream:

DOCREADER_ADDR=localhost:50051
DOCREADER_PORT=50051
DOCREADER_TRANSPORT=grpc

OLLAMA_BASE_URL=http://192.168.1.126:11435
SSRF_WHITELIST_EXTRA=192.168.1.126

TENANT_AES_KEY=weknorarag-api-key-secret-secret
SYSTEM_AES_KEY=weknora-system-aes-key-32bytes!!
JWT_SECRET=weknora-jwt-secret

ENABLE_GRAPH_RAG=false
NEO4J_ENABLE=false
LANGFUSE_ENABLED=false

AUTO_RECOVER_DIRTY=true
CONCURRENCY_POOL_SIZE=5
EOF
```

说明：

- `SSRF_WHITELIST_EXTRA=192.168.1.126` 是必须的
  - 否则 `/initialization/rerank/check` 会因为 SSRF 校验拒绝访问远程 Ollama
- `DB_PORT=5432` 也是必须的
  - 缺失时后端启动会因为数据库配置不完整而失败

## 5. 安装前端依赖

首次在新机器上开发，先安装前端依赖。

```bash
npm --prefix frontend ci
```

如果你更希望用 `npm install` 也可以：

```bash
npm --prefix frontend install
```

## 6. 预拉取 DocReader 镜像

当前开发配置默认使用已发布的 DocReader 镜像，不再本地构建。

首次启动前建议先拉取镜像：

```bash
docker pull wechatopenai/weknora-docreader:latest
```

## 7. 启动 Docker 基础设施

固定使用下面这条命令：

```bash
make dev-start DEV_ARGS="--qdrant --minio --no-langfuse"
```

这条命令会启动：

- PostgreSQL
- Redis
- DocReader
- Qdrant
- MinIO

启动后检查容器状态：

```bash
docker compose -f docker-compose.dev.yml ps
```

你应该看到类似这些服务：

- `WeKnora-postgres-dev`
- `WeKnora-redis-dev`
- `WeKnora-docreader-dev`
- `WeKnora-qdrant-dev`
- `WeKnora-minio-dev`

## 8. 验证基础设施端口

```bash
curl -sS http://localhost:6333/collections
curl -sS http://localhost:9000/minio/health/live
curl -sS http://localhost:9001 >/dev/null && echo "minio-console-ok"
docker compose -f docker-compose.dev.yml ps
```

标准端口：

- PostgreSQL: `localhost:5432`
- Redis: `localhost:6379`
- DocReader: `localhost:50051`
- Qdrant REST: `localhost:6333`
- Qdrant gRPC: `localhost:6334`
- MinIO API: `localhost:9000`
- MinIO Console: `localhost:9001`

## 9. 验证远程 Ollama 和模型

先确认远程 Ollama 可访问：

```bash
curl -sS http://192.168.1.126:11435/api/tags
```

返回的模型列表里应至少包含：

- `qwen3:latest`
- `bge-m3:latest`
- `dengcao/Qwen3-Reranker-4B:Q4_K_M`

可选的快速验证命令：

```bash
curl -sS http://192.168.1.126:11435/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"model":"qwen3:latest","messages":[{"role":"user","content":"Reply with exactly: ok"}],"stream":false}'
```

```bash
curl -sS http://192.168.1.126:11435/api/embed \
  -H 'Content-Type: application/json' \
  -d '{"model":"bge-m3:latest","input":"hello world"}'
```

## 10. 启动后端

在新的终端里执行：

```bash
make dev-app
```

后端健康检查：

```bash
curl -sS http://localhost:8080/health
```

正常返回：

```json
{"status":"ok"}
```

## 11. 启动前端

再开一个新的终端：

```bash
make dev-frontend
```

验证前端：

```bash
curl -I http://localhost:5173
```

前端地址：

- `http://localhost:5173`

后端地址：

- `http://localhost:8080`

## 12. 首次功能验证

建议按下面顺序验证。

### 12.1 注册账号

```bash
curl -sS http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{
    "username": "devuser",
    "email": "devuser@example.com",
    "password": "Secret123!"
  }'
```

说明：

- 响应里会返回用户信息
- 登录后可拿到租户的 `api_key`

### 12.2 登录并拿到 API Key

```bash
curl -sS http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{
    "email": "devuser@example.com",
    "password": "Secret123!"
  }'
```

记下返回里的：

- `token`
- `active_tenant.api_key`

后面所有 `X-API-Key` 示例都替换成你自己的值。

### 12.3 检查内置模型是否已加载

```bash
curl -sS http://localhost:8080/api/v1/models \
  -H 'X-API-Key: <YOUR_API_KEY>'
```

应至少看到：

- `builtin-ollama-chat-default`
- `builtin-ollama-embedding-default`
- `builtin-ollama-rerank-default`

### 12.4 检查 Embedding 模型

```bash
curl -sS http://localhost:8080/api/v1/initialization/embedding/test \
  -H 'Authorization: Bearer <YOUR_JWT_TOKEN>' \
  -H 'Content-Type: application/json' \
  -d '{
    "source": "local",
    "modelName": "bge-m3:latest",
    "dimension": 1024
  }'
```

### 12.5 检查 Rerank 模型

```bash
curl -sS http://localhost:8080/api/v1/initialization/rerank/check \
  -H 'Authorization: Bearer <YOUR_JWT_TOKEN>' \
  -H 'Content-Type: application/json' \
  -d '{
    "modelName": "dengcao/Qwen3-Reranker-4B:Q4_K_M",
    "baseUrl": "http://192.168.1.126:11435",
    "provider": "generic",
    "interfaceType": "ollama"
  }'
```

说明：

- 该检查接口只验证“连通性和基本能力”
- 当前实现使用单文档诊断样本

## 13. 创建知识库并验证 MinIO / Qdrant / DocReader

### 13.1 创建知识库

```bash
curl -sS http://localhost:8080/api/v1/knowledge-bases \
  -H 'X-API-Key: <YOUR_API_KEY>' \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "dev-kb",
    "description": "local dev smoke test",
    "type": "document",
    "embedding_model_id": "builtin-ollama-embedding-default",
    "summary_model_id": "builtin-ollama-chat-default",
    "storage_provider_config": {
      "provider": "minio"
    }
  }'
```

确认返回里包含：

- `"vector_store_engine_type": "qdrant"`
- `"storage_provider_config": {"provider":"minio"}`

### 13.2 上传测试文件

```bash
cat > /tmp/weknora-smoke.txt <<'EOF'
WeKnora smoke test document.
Qdrant is the vector database for retrieval.
MinIO stores uploaded files.
DocReader parses uploaded documents.
The capital of France is Paris.
EOF
```

```bash
curl -sS -X POST 'http://localhost:8080/api/v1/knowledge-bases/<KB_ID>/knowledge/file' \
  -H 'X-API-Key: <YOUR_API_KEY>' \
  -F 'file=@/tmp/weknora-smoke.txt' \
  -F 'metadata={"source":"dev-smoke"}'
```

确认返回里 `file_path` 形如：

```text
minio://weknora-files/...
```

### 13.3 查看知识处理状态

```bash
curl -sS 'http://localhost:8080/api/v1/knowledge-bases/<KB_ID>/knowledge' \
  -H 'X-API-Key: <YOUR_API_KEY>'
```

理想状态：

- `parse_status = completed`
- `summary_status = completed`
- `enable_status = enabled`

### 13.4 检查 Qdrant 是否写入向量

```bash
curl -sS http://localhost:6333/collections
curl -sS http://localhost:6333/collections/weknora_embeddings_1024
```

你应该能看到：

- collection `weknora_embeddings_1024`
- `points_count > 0`

## 14. 启用租户级 Rerank

重要说明：

- 实际搜索时的 `rerank_model_id` 不在知识库配置里
- 它在租户级 `retrieval-config` 里

给当前租户开启 Rerank：

```bash
curl -sS -X PUT 'http://localhost:8080/api/v1/tenants/kv/retrieval-config' \
  -H 'X-API-Key: <YOUR_API_KEY>' \
  -H 'Content-Type: application/json' \
  -d '{
    "embedding_top_k": 10,
    "vector_threshold": 0.0,
    "keyword_threshold": 0.0,
    "rerank_top_k": 5,
    "rerank_threshold": 0.0,
    "rerank_model_id": "builtin-ollama-rerank-default"
  }'
```

查看当前配置：

```bash
curl -sS 'http://localhost:8080/api/v1/tenants/kv/retrieval-config' \
  -H 'X-API-Key: <YOUR_API_KEY>'
```

## 15. 验证实际检索已启用 Rerank

```bash
curl -sS -X POST 'http://localhost:8080/api/v1/knowledge-search' \
  -H 'X-API-Key: <YOUR_API_KEY>' \
  -H 'Content-Type: application/json' \
  -d '{
    "query": "Which component stores uploaded files?",
    "knowledge_base_id": "<KB_ID>"
  }'
```

判断依据：

- 后端日志里会进入 `chunk_rerank`
- 搜索结果 `metadata` 中会出现：
  - `base_score`
  - `model_score`

## 16. 常用命令

启动基础设施：

```bash
make dev-start DEV_ARGS="--qdrant --minio --no-langfuse"
```

停止基础设施：

```bash
make dev-stop
```

查看容器状态：

```bash
make dev-status
```

查看容器日志：

```bash
make dev-logs
```

启动后端：

```bash
make dev-app
```

启动前端：

```bash
make dev-frontend
```

## 17. VS Code 调试

仓库已经提供：

- `.vscode/launch.json`
- `.vscode/tasks.json`

可以直接使用。

建议先安装 VS Code 扩展：

```bash
code --install-extension golang.Go
code --install-extension ms-vscode.js-debug-nightly
```

调试前先做一件事：

```bash
make dev-start DEV_ARGS="--qdrant --minio --no-langfuse"
```

然后在 VS Code 中使用以下调试项：

- `WeKnora: Backend`
  - 直接用 Delve 调试 `cmd/server`
- `WeKnora: Frontend Dev Server`
  - 在 VS Code 终端里启动 `npm run dev`
- `WeKnora: Frontend Chrome`
  - 打开 `http://localhost:5173`
- `WeKnora: Full Stack`
  - 同时启动后端、前端 dev server、Chrome

也可以直接运行以下任务：

- `WeKnora: Infra Start`
- `WeKnora: Infra Stop`
- `WeKnora: Infra Restart`
- `WeKnora: Infra Status`
- `WeKnora: Infra Logs`
- `WeKnora: Backend Dev`
- `WeKnora: Frontend Dev`
- `WeKnora: Health Check`

说明：

- `launch.json` 已经注入了本地开发所需的主机地址覆盖
- 调试模式依赖项目根目录下的 `.env`
- 前端浏览器调试默认使用 Chrome
- 后端调试项已经绑定 `preLaunchTask = WeKnora: Infra Start`
- `WeKnora: Full Stack` 首次启动时，如果 Chrome 早于 Vite 就绪而打开空白页，等终端出现 `VITE ... ready` 后再手动运行一次 `WeKnora: Frontend Chrome`

## 18. 常见问题

### 18.1 `rerank/check` 报 SSRF 校验失败

检查 `.env` 是否包含：

```bash
SSRF_WHITELIST_EXTRA=192.168.1.126
```

修改后重启后端：

```bash
make dev-app
```

### 18.2 后端启动时报数据库配置错误

确认 `.env` 中有：

```bash
DB_PORT=5432
```

### 18.3 DocReader 首次拉取很慢

先手动拉镜像：

```bash
docker pull wechatopenai/weknora-docreader:latest
```

### 18.4 搜索没有实际走 Rerank

先检查租户级检索配置：

```bash
curl -sS 'http://localhost:8080/api/v1/tenants/kv/retrieval-config' \
  -H 'X-API-Key: <YOUR_API_KEY>'
```

确认：

- `rerank_model_id` 不是空

注意：

- `rerank_model_id` 是租户级配置
- 不是知识库字段

### 18.5 Qdrant collection 已创建但没有 points

通常表示：

- 文档上传成功了
- 但 Embedding、摘要或解析链路中途失败

优先检查：

```bash
curl -sS http://192.168.1.126:11435/api/tags
curl -sS http://localhost:8080/health
docker compose -f docker-compose.dev.yml ps
```

再检查知识状态：

```bash
curl -sS 'http://localhost:8080/api/v1/knowledge-bases/<KB_ID>/knowledge' \
  -H 'X-API-Key: <YOUR_API_KEY>'
```
