# 面试宝典 Spec Roadmap

> 参考：`docs/architecture_upgrade.md`
>
> 目标：将“客户端监听 + 云端 AI 服务 + Web UI + 计费监控”完整能力拆解为若干 Specs，指导 `.spec-workflow` 的后续产物编制顺序与范围控制。

## 1. 总览与实施顺序
| 顺序 | Spec（kebab-case） | 对应阶段 | 目的摘要 | 上游依赖 |
| --- | --- | --- | --- | --- |
| 1 | `cloud-access-core` | P0 | 建立账号体系、用户组、鉴权、配置分发 | 无 |
| 2 | `realtime-interview-core` | P0 | 语音流接入、阿里云识别与 AI 编排 | `cloud-access-core` |
| 3 | `desktop-guardian-client` | P0 | 跨平台客户端 + 防检测守护，完成音频/截图上报 | `realtime-interview-core`（服务端接口稳定） |
| 4 | `web-interview-console` | P0 | WebUI 实时展示、登录、面试控制面板 | `cloud-access-core`, `realtime-interview-core` |
| 5 | `knowledge-insights-platform` | P1 | 简历/知识库上传、向量隔离、策略入口 | `cloud-access-core`, `web-interview-console` |
| 6 | `observability-billing-suite` | P1/P2 | 计量、收费策略、监控与告警 | 依赖全部实时事件流 |

> P2 以后可扩展其他 Specs（如多地区、合作方 API 等），但须在上述基础完成后再立项。

## 2. Specs 详情

### 2.1 `cloud-access-core`
- **目的**：提供统一账号、角色、额度策略及 API 网关能力，确保所有后续服务通过 JWT/PASETO + RBAC 受控。
- **关键需求**
  1. 注册/登录（邮箱、第三方 OAuth 占位）、设备绑定、用户组（管理员/特级/高级/普通）。
  2. 额度策略：默认无限但记录真实消耗，支持管理员调整折扣、冻结账号。
  3. API Gateway 控制面（Go 实现），向 Envoy/NGINX 下发路由、限流、证书策略。
  4. 客户端配置下发：音频采样、检测策略、模型策略，需签名验证。
- **非目标 / 限制**
  - 暂不接入真实支付；仅记录消耗与账单草稿。
  - 不实现多租户隔离 UI，但数据库需 tenant 字段。
- **依赖与接口**
  - PostgreSQL（用户、策略），Redis（session），ClickHouse（用量镜像）。
  - 提供 REST (`/auth/*`, `/users/*`) + gRPC (`IAMService`).
- **完成标准**
  - `.spec-workflow/specs/cloud-access-core/` 下 requirements/design/tasks 批准。
  - 单体 Go 服务（Auth, Policy, Config）在 docker compose 中可用；能向 Web、客户端颁发 token。

### 2.2 `realtime-interview-core`
- **目的**：在云端承载实时会话、语音转写、AI 调度，把旧版 `ui/controller.py` 能力迁移到 Go 服务。
- **关键功能**
  1. gRPC 双向流 `StreamAudio`：客户端上传 PCM，服务端推送分段识别结果。（参考 `sound_capture.AudioRecorder`).
  2. Speech Gateway 插件化：默认接阿里云 NLS，储存时长、错误；保留其他 Provider 的接口。
  3. AI Orchestrator：统一 prompt、工具注册，生成“简短回答 + 详细解析 + 提示”，并触发事件到 WebSocket。
  4. 会话状态机：创建/暂停/结束、截图任务、失败回滚；Redis Stream 做事件总线。
  5. 指标与日志：每次识别/LLM 调用写计费流水。
- **限制**
  - OCR/截图题仅调用 LLM，多模态 OCR 暂未接入第三方。
  - 不处理知识库检索（交由后续 spec）。
- **依赖**：`cloud-access-core` 提供认证；MinIO 存储原始音频；Redis + ClickHouse。
- **完成标准**：WebSocket/事件 API 稳定；Go 服务支持水平扩展；具备基础监控。

### 2.3 `desktop-guardian-client`
- **目的**：提供 Windows/macOS 客户端，完成登录、音频 Loopback、截图、检测录屏等动作，最小化运行。
- **需求**
  1. 采用 Electron Shell + Rust/CoreAudio/WASAPI 模块；可后台运行、托盘控制。
  2. 自动选择扬声器，支持手动 override；噪声门限、自动增益。
  3. 防录屏/防切屏：检测常见录屏进程、虚拟机、焦点切换，遇到风险时本地提示 + 上报。
  4. 截图工具（参考 `backup/screenshot`）+ 屏幕共享检测（ScreenCaptureKit/DXGI）。
  5. 本地缓存加密（AES-GCM），崩溃后自动清理。
- **限制**
  - Linux 暂不支持。
  - 不含 UI 面试控制（交由 WebUI）。
- **依赖**
  - `cloud-access-core` 颁发 token & 配置。
  - `realtime-interview-core` gRPC endpoint。
- **出口条件**
  - 安装包（Win/Mac）可连接测试环境，成功上传音频并接收策略；防护策略可远程更新。

### 2.4 `web-interview-console`
- **目的**：提供面试操控台，支持实时字幕、AI 回答、截图解析展示、账户管理与基本仪表。
- **主要需求**
  1. Next.js + Tailwind + Zustand，SSO 登录，拉取用户信息、额度、权限。
  2. 会话视图：通过 WebSocket 订阅识别结果、AI 答案、截图解析；支持 quick feedback。
  3. 面试配置：岗位类型、模型选择、提示词预设。
  4. Usage 面板：展示语音时长、LLM token、截图次数（来自 `observability-billing-suite` 数据）。
  5. 管理员控台：用户搜索、策略下发、客户端状态列表。
- **限制**
  - 不直接上传音频（全部经客户端）。
  - 报表功能展示为基础图表，高级分析由后续 spec 承担。
- **依赖**
  - `cloud-access-core` Auth APIs。
  - `realtime-interview-core` WebSocket & REST。
  - `observability-billing-suite` 提供统计 API（若未完成，临时 Mock）。
- **完成标准**
  - SSR 页面部署于 Vercel/容器；具备 E2E 测试；可作为主要操控入口。

### 2.5 `knowledge-insights-platform`
- **目的**：为简历、岗位 JD、个人知识库提供上传、解析、向量化与隔离能力，支撑 AI 的拓展功能。
- **范围**
  1. 上传渠道（Web/API），支持 PDF/Doc/Markdown；触发异步解析（Temporal/Kafka）。
  2. Text Splitter + Embedding 管道；Milvus/Qdrant/PgVector 以 `tenant_id`+`user_id` 隔离。
  3. 向量索引管理 API：增删改查、标签、配额。
  4. Prompt/Tool 占位：`ResumeTailor`, `KnowledgeBaseQuery`。
  5. 数据治理：加密存储、脱敏日志。
- **限制**
  - 暂不提供 UI 级别的深入编辑，可先展示上传状态与基础建议。
  - 对外共享、协同编辑不在本期。
- **依赖**：`cloud-access-core`（权限）、`web-interview-console`（入口）、`realtime-interview-core`（AI 调用 Hook）。
- **完成标准**：API 文档齐备、可运行 docker compose 版本（含向量库），具备至少一个可调用的 Tool stub。

### 2.6 `observability-billing-suite`
- **目的**：统一计量语音、LLM、截图等用量，提供监控、告警、账单草稿与运营面板。
- **功能**
  1. 消费 Redis Stream/Kafka 事件，写入 ClickHouse/Timescale；生成 per-user/day 指标。
  2. 计费引擎（Go）：按用户组折扣/免费额度策略计算费用，当前额度默认无限但需记录真实应付。
  3. Prometheus exporter + Grafana dashboards（QPS、延迟、识别成功率、AI 成本）。
  4. 告警模块：阈值 + 异常检测，钉钉/飞书 Webhook；记录防录屏警告。
  5. 对 WebUI 提供 `/usage`, `/billing`, `/alerts` API；对管理员提供 CSV 导出。
- **限制**
  - 真正扣费流水、对账与支付网关暂不实现。
  - 多区域复制、长周期归档留待未来 spec。
- **依赖**：所有实时事件源（`realtime-interview-core`, `desktop-guardian-client`）、`cloud-access-core`（用户信息）。
- **完成标准**：监控仪表上线、告警生效，WebUI 可查看真实使用量，计量结果与事件源对账误差 < 1%。

## 3. Spec 执行流程（对应 MCP 工具）
1. **启动前**：确认是否需要更新 `.spec-workflow/steering/`；若有修改，需先维护 steering 文档。
2. **每个 Spec**：
   - 在 `.spec-workflow/specs/<spec-name>/` 依次创建 `requirements.md` → `design.md` → `tasks.md`，名称使用上表的 kebab-case。
   - 每个阶段编写后必须使用 `approvals` 工具：`request` → `status` → `delete`，确保审核通过。
   - `tasks.md` 发布后，执行实现阶段：`spec-status` 查看整体进度，更新任务勾选状态，完成开发后调用 `log-implementation` 记录代码改动。
3. **并行/依赖规则**：
   - `cloud-access-core` 与 `realtime-interview-core` 可并行起草 requirements，但 design 阶段需先确认统一协议。
   - `desktop-guardian-client` requirements 可在服务端 design 进入后期时开始，但 tasks 必须引用已经冻结的 gRPC proto。
   - 所有 P1 Specs 需在 P0 的 requirements 批准后才能进入 design，避免接口反复。
4. **文档引用**：在各 spec 的 requirements/design 中引用 `docs/architecture_upgrade.md` 与本 roadmap，以保持架构一致性。

## 4. 里程碑映射
| 里程碑 | 涉及 Specs | 说明 |
| --- | --- | --- |
| P0 - 端到端 MVP | `cloud-access-core`, `realtime-interview-core`, `desktop-guardian-client`, `web-interview-console` | 客户端可登录上传音频，Web 实时展示 AI 输出，计量数据至少记录原始事件。 |
| P1 - 功能补齐 | `knowledge-insights-platform`, `observability-billing-suite` | 开启知识库、简历入口；计费与监控对管理面开放。 |
| P2 - 商业化准备 | （可新立 `security-partner-integration`, `payment-clearing` 等 spec） | 以本 roadmap 为基准扩展，需额外审批。 |

---
该 roadmap 用于指导 `.spec-workflow` 目录下的 spec 建立顺序、范围和交付标准，确保与 Go 架构方案一致、可持续扩展。
