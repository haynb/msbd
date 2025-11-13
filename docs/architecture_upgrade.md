# 面试宝典 2.0 升级改造架构方案

## 1. 背景与设计目标
- 现有版本（见 `backup/`）是 Tkinter + Python 单体应用，功能集中在 `ui/controller.py` 中，直接驱动语音采集(`sound_capture/`)、阿里云实时识别(`speech_recognition/`)与 LLM(`llm/`) 调用，缺少账号体系、云端存储与扩展能力。
- 新版本要保留“系统声音监听 + 面试语义理解 + 截图解析”核心能力，同时满足文档中的 WebUI、账号登录、云端服务、计费监控、知识库扩展、防检测、防录屏等要求。
- 架构目标：**模块化、跨端、云管端一体、利于商业化**。一期以单体部署为主，二期可拆分为独立服务。

## 2. 现状评估（Legacy）
| 模块 | 主要代码 | 痛点 | 必须保留的能力 |
| --- | --- | --- | --- |
| 桌面 UI | `backup/ui/app_ui.py` | 仅 Tkinter，本地状态，缺少账号管理、WebUI | 面试类型选择、截图入口、AI 结果展示 |
| 控制器 | `backup/ui/controller.py` | 业务逻辑集中、耦合音频/LLM/UI | 语音结束回调 -> LLM -> 答案生成流程 |
| 音频捕获 | `backup/sound_capture/sound_capture.py` | Windows 特定 API，线程/错误处理薄弱 | 系统扬声器 loopback 录音、噪声抑制 |
| 语音识别 | `backup/speech_recognition/ali` | 仅支持阿里云，缺少使用量统计 | 句子粒度回调、稳定性 |
| LLM | `backup/llm/*` | 双模（OpenAI/DeepSeek）但只支持问答，不含函数/多工具 | 问答能力、截图分析 |
| 配置 | `backup/config/config_loader.py` | 靠本地 YAML/环境变量 | 需迁移到云端账号配置 |

结论：旧版代码将作为“语音捕获 + LLM 调用逻辑”的技术参考，需要抽离可复用模块（音频采集、流式识别封装、截图工具）并封装为客户端 SDK。

## 3. 总体架构
```
┌────────┐      HTTPS/WebSocket      ┌─────────────────┐      gRPC/Queue      ┌──────────────┐
│ 桌面轻客户端 │◀─────────────────────▶│ 云端 API 网关/鉴权 │◀──────────────────▶│ AI & 任务编排层 │
└────────┘                            └─────────────────┘                      └──────────────┘
      │                                                                                 │
      │本地音频/截图                                                                   │向量检索/LLM
      ▼                                                                                 ▼
┌────────────┐      Event/Metric      ┌──────────────────┐      ETL/BI      ┌──────────────┐
│ 防检测守护进程 │────────────────────▶│ 计费&监控服务 (Kafka) │────────────────────▶│ 报表&后台运营 │
└────────────┘                       └──────────────────┘                      └──────────────┘
```
- **客户端**：负责扬声器检测、音频截取、加密通道上传、前台入口最小化以及防切屏/防录屏检测。
- **Web 前端**：浏览器访问，负责登录、会话管理、实时转写、AI 答案、知识库与账单展示。
- **云端 API**：统一入口，提供 Auth、会话、LLM 编排、Screenshot OCR、日志、计费等 REST/gRPC 接口。
- **AI/数据层**：抽象 LLM Provider、向量数据库、简历解析任务；所有调用在云端统一计量。
- **可观测与计费**：Kafka/ClickHouse 记录调用粒度，用于后续收费、风控。

## 4. 客户端（Win/Mac）
| 子模块 | 功能 | 技术/实现要点 |
| --- | --- | --- |
| 引导器 & 登录 | Electron/Qt + 内嵌 WebView，复用 Web 登录，成功后下发 JWT + 客户端配置 | 支持设备码登录，断线重连 |
| 音频采集服务 | 参考 `sound_capture.AudioRecorder`，升级为 Rust/Python CEF 模块，支持 CoreAudio/WASAPI loopback 自动选择，手动 fallback | 配置噪声门限、自动增益；可切换到麦克风 |
| 语音流上传 | gRPC 双向流，将 PCM chunk + 元数据推送到云端 Speech Gateway，失败自动重试 | chunk 中记录 userId、会话Id、当次 token |
| 屏幕监控与截图 | 防检测 Hook：监听系统录屏事件、虚拟摄像头、剪贴板；截图由云端保存路径，客户端只传二进制 | Windows 用 DXGI + Keyboard hook；macOS 用 ScreenCaptureKit |
| 防录屏/防切屏 | 提供 `DetectionGuard` 子进程，检测常见录屏进程、虚拟机、焦点切换，触发时通知云端 | 支持白名单和策略热更新 |
| 本地缓存 | 仅缓存 30s PCM/日志，全部 AES-GCM 加密，磁盘落地可选 | 避免明文敏感信息 |

## 5. Web 前端（WebUI）
- 技术栈：Next.js + Tailwind + Zustand/RTK Query，SSR 便于 SEO，自带 SSO。
- 核心页面：
  1. **面试大厅**：实时显示语音转写、AI 答案、截图解析；通过 WebSocket 订阅会话事件。
  2. **任务排期**：选择岗位/题库，触发自定义面试流程。
  3. **知识库 & 简历管理**：上传简历、岗位 JD、个人知识卡片，后端异步写入向量库。
  4. **账单与使用量**：展示 LLM Token、语音时长、截图次数；管理员可调配额度。
  5. **后台管理**：用户组、策略、风控警报、日志查询。
- UI 规范：保留老版的“语音识别/AI回答/截图”分栏，加入暗色主题、状态气泡、loading skeleton。

## 6. 云端服务设计（Go 语言版）
| 服务 | 说明 | 接口/职责 | Go 实现要点 |
| --- | --- | --- | --- |
| API Gateway | NGINX/Envoy 仍负责 L4/L7 转发，控制面由 Go 服务下发配置 | `/auth/*`, `/sessions/*`, `/billing/*`, `/files/*` | 使用 Go 写配置中心（Gin + gRPC），通过 xDS/Envoy API 推送路由 & 限流策略 |
| Auth & Account | 用户/用户组/权限、OAuth、令牌刷新、设备绑定 | RBAC、额度配置、策略下发 | Go 1.22 + Gin/Fiber，采用 Clean Architecture；SQLC 生成 PostgreSQL DAO，JWT & PASETO 双 token |
| Interview Session Service | 管理实时面试会话，接受音频片段，调用语音识别 & AI，推送 WebSocket | 会话生命周期、转写缓存、AI 答案、截图任务 | Go gRPC 双向流 + goroutine worker pool，Redis Stream 作为内存总线，使用 proto/gogopb 定义消息 |
| Speech Gateway | 抽象多家语音识别，统一流式接口，记录识别时长 | 对接阿里云 SDK，回放与计量 | Go 插件模式（HashiCorp go-plugin），每个 Provider 实现接口；内置 Circuit Breaker（sonyflake + hystrix-go） |
| AI Orchestrator | 统一 LLM/工具调用：问答、截图 OCR、Prompt 选择、函数调用 | 多模型 fallback、工具路由 | Go Micro/kratos-style service，内部用 OpenTelemetry 追踪，透出 REST+gRPC；LLM 调用采用 go-resty + 重试策略 |
| Resume & Knowledge Worker | 处理简历 ingestion、JD 匹配、embedding | 异步 API，下游向量库 | Go + Temporal/Cadence workflow，任务通过 Kafka/Redis Stream 触发；向量入库调 Qdrant gRPC |
| Billing & Telemetry | 汇总语音时长、LLM token、功能调用次数 | 写入 ClickHouse/TimescaleDB | Go 写流式消费器（Sarama/Kafka-go），统一计费 SDK，支持多币种策略计算 |
| File/Media Service | 管理截图、音频、log 存储，S3/OSS 兼容 | 生成一次性下载 URL | Go + MinIO SDK，实现预签名、生命周期管理，支持 ClamAV 扫描 |

### Go 技术栈与实现原则
- **统一语言**：Go 1.22+，模块按 `cmd/<service>` + `internal/<domain>` 划分，采用 Wire/Fx 做依赖注入。
- **传输协议**：REST（Gin/Fiber）用于面向前端的 JSON API；gRPC/Connect 用于客户端音频流与服务间通信；GraphQL 可选用于后台聚合查询。
- **并发模型**：goroutine + channel 实现音频 chunk 管线和计费流水；对外暴露的流式接口均提供 backpressure。
- **数据访问**：SQLC/GORM 生成数据访问层，配合 Ent schemas 管理迁移；Redis 通过 go-redis，向量库通过官方 gRPC/HTTP 客户端。
- **配置 & 可观测**：Viper 统一加载配置，配合 Consul/Nacos；OpenTelemetry + Prometheus Go client；zap/logrus 做结构化日志。
- **测试与质量**：每个服务提供 table-driven test、gRPC contract test、k6 压测脚本；GitHub Actions 运行 `go test ./...` + golangci-lint。

> 一期将 `Auth + Session + AI Orchestrator + Billing` 合并为 **Go 单体（多模块 + 内嵌 gRPC Gateway）**，事件流使用 Redis Stream/Kafka（dev）。二期再按上表拆分为独立 Go 微服务，通过 Kafka/Envoy Mesh 解耦。

## 7. AI、工具与知识库
- **LLM Provider 抽象**：保留 OpenAI / DeepSeek，增加阿里灵积、私有模型接口。提供策略：按用户组匹配默认模型，可在管理后台配置最大 token。
- **Prompt 与函数**：将 `functions.register_answer_interview_question_function` 升级为 Tool Registry，包含：
  - `AnswerInterviewQuestion`（语音问答）
  - `ExplainScreenshot`（截图题目解析）
  - `ResumeTailor`（根据岗位+简历生成建议，暂留空实现）
  - `KnowledgeBaseQuery`（向量检索，暂预留）
- **知识库**：采用 Milvus / Qdrant / PgVector，按 `tenant_id + user_id` 分片，支持私有 embedding key。上传文档 -> Text Splitter -> Embedding -> 向量库。
- **AI 调度策略**：
  1. 语音识别完成 -> 结构化意图识别（QA/闲聊/系统）；
  2. QA -> 生成 `简短回答 + 详细解析 + 提示`；
  3. 记录 token、模型、延迟 -> 写入 Telemetry。

## 8. 数据与存储设计
| 数据域 | 存储 | 用途 |
| --- | --- | --- |
| 用户/权限/计费 | PostgreSQL | 用户、角色、套餐、额度、订单 |
| 会话/日志 | PostgreSQL + Redis | 会话元数据、实时转写缓存、事件队列 |
| 媒体文件 | MinIO/OSS | 音频、截图、录屏警告证据，生命周期策略 |
| 监控 & 计量 | ClickHouse/Timescale | 语音时长、LLM token、截图次数、错误码 |
| 向量数据 | Milvus/Qdrant/PgVector | 每个用户隔离库，支持知识库/简历检索 |

数据治理：所有写操作带 `tenant_id`，后台查询需 RBAC；敏感字段（access key 等）进入 Vault。

## 9. 计费、监控与运营
- **计费项**：语音识别时长（按秒）、LLM Token、截图 OCR 次数、自定义任务；账号余额目前默认无限，但系统记录真实消耗，便于未来启用扣费。
- **仪表盘**：Prometheus + Grafana 展示 QPS、延迟、队列堆积、识别成功率、AI 成本；管理员 WebUI 显示 per-user 使用量。
- **告警**：阈值告警（token 异常飙升、防录屏触发）、账单异常（超过预估）通过飞书/钉钉 Webhook。

## 10. 安全与防检测设计
- **通信安全**：客户端与云端 gRPC over TLS 1.3，证书指纹校验；本地缓存 AES-GCM；截图/音频上传使用一次性签名。
- **防录屏/防切屏**：
  - Windows：注入低权限 Hook，检测常见录屏进程（OBS、QQ录屏等），检测系统屏幕共享 API 调用。
  - macOS：利用 ScreenCaptureKit/CGSessionCopyCurrentDictionary 监听共享事件。
  - 前端 UI 最小化后进入护眼模式，禁止全屏截图；如检测到录屏，客户端发出本地提醒并通知云端记录。
- **反检测配置**：策略通过后台编辑，下发到客户端守护进程（签名 JSON），支持热更新、灰度。
- **权限**：管理员拥有最高权限和无限额度，特级用户无限额度但无管理权限，高级用户折扣、普通用户按量计费，均在 Auth 服务统一管理。

## 11. 部署与交付
### Docker Compose（1-Click）
```
services:
  api:
    image: registry/msbd-api
    env_file: .env
    depends_on: [postgres, redis, orchestrator]
  orchestrator:
    image: registry/msbd-orchestrator
    depends_on: [redis]
  web:
    image: registry/msbd-web
    env_file: .env
  postgres:
    image: postgres:16
  redis:
    image: redis:7
  minio:
    image: minio/minio
  vector:
    image: qdrant/qdrant
  clickhouse:
    image: clickhouse/clickhouse-server
```
- 所有服务通过 `.env` 注入云端依赖（阿里云 NLS key、LLM key 等）。
- Go 服务镜像采用多阶段构建：`golang:1.22` 编译 -> `distroless/static` 运行，默认启用 `GOEXPERIMENT=arenas` 提升内存效率。
- 未来拆分：将 `api`、`orchestrator`、`billing`、`speech-gateway` 拆成独立 Go 服务，接入 Service Mesh。

### 持续交付
- GitHub Actions/阿里云流水线：`go test ./...`、`golangci-lint` -> 构建镜像 -> 安全扫描 -> 推送 registry -> 触发部署。
- 前端使用 Vercel/阿里云 SFC 托管；客户端安装包通过 Electron Builder/QtIFW 产出。

## 12. 升级与迁移路线
| 阶段 | 目标 | 关键动作 |
| --- | --- | --- |
| P0（2 周） | 端到端 MVP：客户端可登录、上传音频、Web 实时展示 | 抽象音频 SDK、实现 Auth + Session + WebSocket、迁移旧版语音/LLM 逻辑至云端 |
| P1（4 周） | 功能补齐：截图解析、知识库入口、使用量统计 | 加入 Screenshot API、计量服务、知识库占位接口、管理员后台 |
| P2（4 周） | 商业化准备：防录屏策略、计费策略、Docker 一键包 | 完成防护守护进程、账单导出、Docker compose、监控告警 |
| P3（可选） | 微服务化 + 多地区 | 拆分语音/AI/计费服务，接入多云，支持多租户数据法规 |

## 13. 模块化与扩展原则
1. **接口优先**：所有核心功能以 API/SDK 形式暴露，客户端与 WebUI 不直连第三方服务。
2. **事件驱动**：语音、截图、AI 结果一律发布事件，订阅方可为计费、知识库、BI 等模块。
3. **可拔插**：语音服务、LLM Provider、向量库均通过 Adapter 注入，便于未来替换。
4. **配置中心**：策略、模型、限流全部交由云端配置中心（如 Nacos/Consul）管理。
5. **跨平台一致性**：桌面客户端核心逻辑用 Rust/Go 实现，UI 外壳可根据平台切换，但服务协议一致。

---
本方案兼顾现有代码可复用性、商业化路线与后续微服务演进，为后续实现简历分析、知识库、计费等提供清晰扩展口。
