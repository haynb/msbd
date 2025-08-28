# 面试助手系统

一个支持实时/准实时面试辅助的系统：客户端采集系统声音与检测截图，云端完成语音识别与 AI 处理，结果在 Web 端展示。云端使用 Go 实现，客户端语言不限（推荐 Go）。

## 功能特性
- WebUI：浏览器访问，登录/注册、会话实时字幕/要点/建议、知识库管理、简历上传与诊断、用量面板。
- 客户端最小化：只做登录与数据采集（系统声音、截图）、后台常驻与自动设备选择、断线重连。
- 云端处理：ASR（阿里云 NLS）、RAG + LLM 编排（OpenAI/DeepSeek/灵积可切换）、结构化输出。
- 用户与用量：用户体系、私有知识库隔离、ASR 秒数与 LLM tokens 用量统计，预留计费扩展点。
- 稳定可扩展：gRPC 流、WS 推送、Redis/PG、pgvector、可插拔 Provider、完备观测。

## 仓库结构（建议）
```
.
├── server/           # Go 服务（API/gRPC/ASR/AI/KB/Usage）
├── web/              # 前端（Vite + React/Svelte）
├── clients/          # 客户端（Go 优先，亦可多语言）
├── docs/
│   └── ARCHITECTURE.md
├── ARCHITECTURE.md   # 架构文档（根目录副本）
└── README.md
```

## 快速开始（开发）
- 依赖：Go 1.22+、Node 18+、Docker（用于 PG/Redis/MinIO）、Make（可选）。
- 本地环境（建议 docker compose 后续补充）：
  1) 启动 PostgreSQL、Redis、MinIO（OSS 模拟）。
  2) 配置环境变量：
     - `DATABASE_URL`、`REDIS_URL`、`OSS_ENDPOINT/KEY/SECRET/BUCKET`
     - `ASR_ALIYUN_KEY/SECRET`、`LLM_PROVIDER`、`OPENAI_API_KEY` 等
  3) 运行 Go 服务（待实现代码）：`go run ./server/cmd/api`
  4) 启动前端：`cd web && npm install && npm run dev`
  5) 客户端（示例，待实现）：`go run ./clients/go/cmd/agent` 登录并开始会话。

## 关键工作流
- 创建会话：前端/客户端调用 `POST /api/v1/sessions` 获取 `session_id`。
- 客户端推流：gRPC 双向流发送 100–300ms PCM16 分片（16k 单声道）。
- ASR 与 AI：云端聚合转写，触发 RAG+LLM，生成建议与回答。
- 结果推送：前端通过 WebSocket 订阅 `sessions/{id}/events` 实时更新。
- 截图：客户端定时上传 `POST /api/v1/sessions/{id}/screenshots`，返回 oss_url。
- 用量：自动记录 ASR 秒数与 LLM tokens，用于后续计费。

## 安全与隐私
- JWT 认证 + 刷新；全链路 TLS；对象存储签名 URL。
- 用户数据与知识库隔离；数据最小化与可删除。

## 路线图（摘要）
- M1：账户、会话、ASR 流、水位与重连、WS 字幕、ASR 用量。
- M2：RAG+LLM、知识库、简历诊断、LLM 用量、前端完善。
- M3：观测、降级与治理、客户端设备选择与自启、UI 美化与打包。

## 参考
- 详细架构见 ARCHITECTURE.md
- 旧版参考：`backup/` 下的初版实现（ASR/LLM/截图/采集）。

## 许可
- 待定（内部项目可暂不声明）。

