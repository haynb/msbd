# 新版面试助手重构总体方案

## 目标与约束
- 保留旧版（`backup/`）的语音捕获、阿里云识别、AI 截图答题等核心能力，整体迁移至“客户端守护进程 + 云端服务 + WebUI”模式。
- 支持注册登录、多角色分级、账户余额/额度记录，计费开关可后置但数据采集要先落地。
- WebUI 承载所有交互展示，客户端最小化运行、自动选择扬声器（也可手动选择），提供跨平台（Win/macOS）能力。
- 深度利用 AI：知识库、简历解析、截图识别、面试答疑，并对 AI 调用量、语音识别时长进行可追踪监控。
- 加入防截图、防录屏、防切屏等“防检测”机制，确保模拟环境安全。
- 保持架构简单高效，模块化/可扩展，为后续商业化和更多能力预留空间。

## 旧版现状回顾
- **技术栈**：单机 Python（Tkinter UI），通过 `soundcard` 捕获系统音频，`speech_recognition/ali` 调用 NLS WebSocket，`llm/` 模块封装 OpenAI / DeepSeek，截图模块 + 图像识别。
- **优点**：模块划分清晰（音频、识别、LLM、UI），函数调用封装可复用，配置中心 `ConfigLoader` 独立。
- **不足**：
  - UI 与逻辑耦合严重（`ui` 目录同时负责展示与线程调度）。
  - 无用户体系/统计能力；所有调用发生在本地，难以集中管理。
  - Tkinter + Windows API（如 `win32gui`）与系统绑定，迁移 WebUI 困难。
  - 防检测、防截图能力空缺；跨平台支持仅在音频捕获层面有尝试。
  - 无服务端，难以支撑多端协同、商业化、知识库等云能力。

## 总体架构设计
```
┌──────────────────────────────────────────────────────────────┐
│                       云端服务（Go 微服务）                    │
│ ┌──────────────┐  ┌──────────────┐  ┌─────────────────────┐ │
│ │认证与账户中心 │→│计费与监控服务 │→│AI 调度网关(LLM/工具) │ │
│ └──────────────┘  └──────────────┘  └─────────────────────┘ │
│        ↑                   ↑                     ↑          │
│ ┌──────────────┐  ┌──────────────┐  ┌─────────────────────┐ │
│ │知识库&向量检索│  │简历解析服务  │  │会议/会话管理服务     │ │
│ └──────────────┘  └──────────────┘  └─────────────────────┘ │
│                    ↓                            │          │
│               数据层(PostgreSQL + Redis + 对象存储 + 向量DB) │
└──────────────────────────────────────────────────────────────┘
                 ↑                              ↑
┌────────────────┴─────────────┐    ┌───────────┴────────────────┐
│  客户端守护进程（Python/Qt6） │    │       Web 前端（React）     │
│  - 登录/鉴权、策略配置        │    │  - 面试控制台/结果面板        │
│  - 系统音频捕获 + 本地加密上传 │    │  - 简历管理、知识库维护       │
│  - 防检测守护、状态托盘        │    │  - 使用量仪表盘、账号管理     │
└───────────────────────────────┘    └────────────────────────────┘
```

## 核心模块规划
- **客户端守护进程**
  - `auth`：一次性登录后缓存短期 token，定期刷新。
  - `audio_agent`：跨平台采集系统回环音频；对接阿里云 NLS（本地直连或经云端代理）；提供失败重试与带宽控制。
  - `anti_detect`：拦截常见截图快捷键、录屏进程监测；Windows 用 `pywin32/psutil` + 低级键盘钩子，macOS 用 `Quartz` 事件与屏幕录制状态 API。
  - `sync_service`：与云端 WebSocket 保持会话，发送识别片段、接收 AI 结果/指令；支持断线续传。
  - `ui_stub`：最小化系统托盘或状态窗口（Qt/PySide6），用于登录、服务状态切换。
- **云端服务（Go 语言栈）**
  - `user-service`：用 Gin/Fiber + OAuth2/OpenID Connect 组件实现注册登录、角色管理（管理员/特级/高级/普通）、额度字段和折扣策略；统一认证中心发放短期 token。
  - `session-service`：基于 gorilla/websocket 或 nhooyr.io/websocket 管理面试会话、音频片段、识别记录；支持水平扩展和断线重连。
  - `ai-gateway`：封装 OpenAI/DeepSeek/自建工具的调用（函数调用、知识库 Retrieval、简历分析），暴露 gRPC/REST；使用 rate limiter + 中央日志记录 token 消耗。
  - `knowledge-base`：Go worker（Asynq/Temporal）异步处理文档解析、向量化；每用户隔离的向量库（PgVector/Milvus/Qdrant）。
  - `resume-service`：借助 gofpdf/unidoc 解析 PDF/DOCX，结合岗位 JD 与 AI Gateway 输出修改建议。
  - `billing-observer`：收集 AI token、Aliyun 时长、知识库存储量，写入使用流水；后续付费策略可直接接入。
  - `notification/logging`：Zap + OpenTelemetry 记录日志，Prometheus 指标 + Alertmanager 告警。
- **数据与资源层**
  - PostgreSQL：用户、会话、计费、配置、知识库元数据。
  - Redis：会话状态、验证码/令牌缓存、WebSocket 通道。
  - 对象存储（MinIO 或云 OSS）：原始音频片段、简历文件（后续会提供一个S3兼容的云oss平台，不需要minio）。
  - 向量数据库：PgVector（轻量部署）或 Qdrant/Weaviate。
- **Web 前端**
  - React + Vite/Next.js，UI 框架（Ant Design/Chakra）。
  - 主要页面：仪表盘、实时会话、AI 答案视图、简历工坊、知识库管理、使用量统计。
  - WebSocket 或 SSE 展示实时字幕/回答；提供截图解析上传入口。

## 数据与流程要点
- **音频处理链路**
  1. 客户端捕获系统声卡 → 做噪声抑制/分片。
  2. 优先直接调用阿里云 NLS（token 来自云端下发，客户端不做存储，每次登陆获取新的，防止盗用），识别结果回传云端；若因网络策略需中转，则走云端代理。
  3. 云端 Session Service 聚合识别文本 → 调 AI Gateway → 推送前端、同步回客户端。
- **AI 调用链路**
  - 统一在云端发起，便于记录 token & 请求参数。
  - 支持函数调用路由（答题、解析、知识库检索、截图问答等）。
  - 结果持久化，便于后续导出/复盘。
- **知识库**
  - 用户上传文件 → 异步抽取文本 → 拆 chunk → 嵌入向量 → 保存至用户向量空间。
  - 查询时携带用户 ID 做隔离。
- **统计计费**
  - 中央化统计表：`usage_records`（用户、会话、类型、数量、成本、时间）。
  - Aliyun 时长：由客户端/云端上报识别时长，定期聚合。
  - AI token：调用前后记录 prompt/response token、供应商成本。

## 跨平台与安全策略
- **音频捕获**
  - Windows：继续 `soundcard` or `pyaudio` + WASAPI Loopback（需要打包 VC 依赖）。
  - macOS：CoreAudio + `Soundflower/BlackHole` 引导；首次引导用户安装驱动。
  - 统一封装接口，按平台注入实现。
- **防检测**
  - Windows：注册 `SetWindowsHookEx` 监听剪贴板/屏幕截取快捷键，阻断或提示；轮询常见录屏进程（OBS、Teams、Zoom）。禁用全屏共享可选。可以参考老的python平台的实现方法。
  - macOS：监控 `CGShieldingWindowLevel` 变化 + `osascript` 查询录屏权限，必要时自动停止服务或通知。
  - 提供策略开关，自定义白名单进程。
- **安全通信**
  - 全链路 HTTPS/WSS；客户端缓存 token 加密存储（Keyring）。
  - 敏感配置（阿里云 AK）放在云端，客户端通过临时授权获取。

## 技术选型建议
- **客户端**：Python 3.11、PySide6（托盘 UI）、SoundCard/pyobjc（mac）、PyInstaller 打包。
- **服务端**：Go 1.22+（Gin/Fiber + Wire 依赖注入），数据库访问用 `sqlc` + `pgx`，任务调度用 Asynq（Redis），消息/事件可选 NATS；对象存储 MinIO/OSS，向量数据库 PgVector 或 Qdrant。
- **前端**：React + Vite、Ant Design、Zustand/Redux、Socket.IO。
- **AI**：OpenAI/DeepSeek 官方 Go SDK/HTTP API，向量化可调用自建 Embedding 服务或三方 API，知识库检索可选自建 Retrieval 层。
- **监控**：Prometheus + Grafana，OpenTelemetry + Jaeger，Sentry 或 Honeycomb 捕获异常。

## 开发阶段规划
1. **准备期（第 0 阶段）**：完善需求、确定技术栈、搭建 mono repo（`apps/client`, `apps/server`, `apps/web`），整理 CI/CD 雏形。
2. **基础设施期**：
   - 服务端：完成 Go API skeleton（Gin/Fiber）、认证/授权、数据库建模（sqlc 代码生成）、AI 调度基础。
   - 客户端：实现登录、音频捕获、NLS 直连、心跳。
3. **功能扩展期**：
   - WebUI：登录/仪表盘/实时面试界面；与会话服务打通。
   - 简历解析、知识库管理、截图问答；AI 功能模块化。
   - 使用量统计、管理员后台、角色/额度策略。
4. **强化期**：
   - 防检测能力迭代、跨平台兼容调优、打包分发。
   - 指标监控、错误告警、日志清洗。
5. **验收与准备 Spec**：整理测试用例、性能验证、部署脚本；根据评审反馈更新细化 Spec（Requirements → Design → Tasks）。

## 风险与待决策
- **阿里云 NLS 调用位置**：客户端直连可减轻服务器压力，但需处理网络限制；若放云端需考虑带宽成本与延迟。
- **防检测策略的可行性**：不同系统权限差异大，需要确认最小可行集合与是否需要驱动层实现。
- **知识库向量引擎选择**：PgVector 部署简单但扩展性有限，Milvus/Qdrant 需额外运维。
- **Go 团队学习曲线**：需建立统一编码规范（lint、error handling、依赖注入），并补齐向量/AI 相关 SDK 的二次封装。
- **跨平台驱动安装体验**：macOS 回环驱动安装体验需设计完整引导。
- **后续收费模型**：需提前定义哪些指标会真实计费，以便埋点满足需求。

## 下一步
- 待你评审此总体方案后，再进入正式 Spec 流程（Requirements → Design → Tasks → Implementation）。
- 若方案方向确认，将进一步细化：数据模型、接口契约、前端信息架构、客户端守护进程状态机等。
