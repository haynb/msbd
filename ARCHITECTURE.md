# 面试助手系统 — 架构设计文档 v2.0

## 1. 目标与边界

### 1.1 核心目标
提供面试场景下的实时/准实时辅助能力，包括：
- 系统声音采集、语音转文字、AI 解析
- 知识库查询、简历分析优化
- 结果在 Web 端呈现
- **防监控检测能力**（防截图、防录屏、防切屏、防屏幕分享）

### 1.2 技术约束
- **云端服务**：使用 Go 1.22+ 开发（高并发、稳定、gRPC/WebSocket支持）
- **客户端**：推荐使用 Go 开发，支持跨平台（Windows/macOS/Linux）
  - 仅负责：用户认证、数据采集（音频、截图）、防检测功能
  - 最小化界面或后台常驻运行
- **Web前端**：所有交互界面、结果展示、配置管理
- **处理后端化**：音频分析、AI处理、知识库检索均在云端完成

### 1.3 非目标（首版不做）
- 计费结算与支付对接（保留使用量统计接口）
- 复杂运营管理后台
- 移动端APP开发

## 2. 总体架构

### 2.1 系统概览
```
┌─────────────────┐    ┌──────────────────────────────────────────────────────┐
│   客户端 (Go)     │    │                   云端服务 (Go)                        │
│                 │    │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐   │
│ • 音频采集       │◄──►│  │  Gateway API │  │ Client Ingress│  │ ASR Service  │   │
│ • 防检测功能      │    │  │   (REST/WS)  │  │   (gRPC)     │  │   (阿里云)    │   │
│ • 后台常驻       │    │  └──────────────┘  └──────────────┘  └──────────────┘   │
│                 │    │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐   │
└─────────────────┘    │  │ AI编排服务    │  │  知识库服务   │  │  用量统计    │   │
          │            │  │ (LLM/RAG)    │  │ (向量数据库)  │  │   服务      │   │
          │            │  └──────────────┘  └──────────────┘  └──────────────┘   │
          ▼            └──────────────────────────────────────────────────────┘
┌─────────────────┐                         │
│  Web前端        │◄────────────────────────┘
│ • 实时会话      │
│ • 知识库管理    │  
│ • 简历分析      │
│ • 用户设置      │
└─────────────────┘
```

### 2.2 通信协议
- **客户端 ↔ 云端**：
  - 主通道：gRPC 双向流（音频分片、实时控制、心跳保活）
  - 备用通道：WebSocket（兼容性考虑）
  - 数据格式：Protobuf（高效） / JSON（调试）

- **Web前端 ↔ 云端**：
  - 管理操作：REST API（用户管理、会话管理、文件上传）
  - 实时推送：WebSocket/SSE（转写结果、AI建议）

### 2.3 服务分层

#### 接入层
- **Gateway API**：HTTP/WebSocket服务，认证、限流、路由转发
- **Client Ingress**：gRPC服务，处理客户端长连接

#### 业务处理层  
- **ASR服务**：语音识别，支持多厂商（阿里云、Whisper等）
- **AI编排服务**：Prompt管理、RAG检索、LLM调用、结果聚合
- **知识库服务**：文档管理、向量检索、嵌入生成

#### 数据服务层
- **用量统计服务**：API调用量、费用计算、配额管理
- **存储服务**：PostgreSQL + pgvector + Redis + OSS

### 2.4 技术选型
- **后端框架**：Go 1.22+，gRPC + Protobuf，Gin/Fiber（REST）
- **数据库**：
  - PostgreSQL 15+（用户/会话/用量/配置数据）
  - pgvector（嵌入向量存储与检索）
  - Redis 7+（会话状态、缓存、速率限制、Pub/Sub推送）
- **存储**：S3兼容对象存储（阿里云OSS）用于截图、简历、文档
- **ASR**：阿里云NLS（主）+ Whisper（备用）
- **LLM/Embedding**：支持多Provider（OpenAI/DeepSeek/阿里云灵积/本地模型）
- **监控**：Prometheus + Grafana（指标）+ Jaeger（链路追踪）+ Loki（日志）

## 2.5 防监控检测架构设计

### 2.5.1 防检测功能概述
客户端需要实现以下防监控检测能力，确保面试辅助过程不被发现：

| 功能 | Windows支持 | macOS支持 | 实现方式 |
|-----|------------|-----------|---------|
| 防截图 | ✅ | ✅ | Window Display Affinity API / 私有API |
| 防录屏 | ✅ | ⚠️ | SetWindowDisplayAffinity / 部分支持 |
| 防切屏检测 | ✅ | ✅ | 窗口属性修改 / 系统级隐藏 |
| 防屏幕分享 | ✅ | ✅ | 同防录屏机制 |

### 2.5.2 技术实现方案

#### Windows平台
```go
// 通过CGO调用Win32 API
import "C"

// 防截图/录屏
func EnableAntiCapture(hwnd uintptr) error {
    WDA_EXCLUDEFROMCAPTURE := 0x00000011
    ret := C.SetWindowDisplayAffinity(C.HWND(hwnd), C.DWORD(WDA_EXCLUDEFROMCAPTURE))
    return checkResult(ret)
}

// 防切屏检测  
func EnableAntiSwitchDetection(hwnd uintptr) error {
    // 设置窗口为工具窗口，不在Alt+Tab列表中显示
    WS_EX_TOOLWINDOW := 0x00000080
    WS_EX_NOACTIVATE := 0x08000000
    
    style := C.GetWindowLongW(C.HWND(hwnd), C.GWL_EXSTYLE)
    newStyle := style | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE
    C.SetWindowLongW(C.HWND(hwnd), C.GWL_EXSTYLE, newStyle)
    
    // 设置为顶层窗口
    C.SetWindowPos(C.HWND(hwnd), C.HWND_TOPMOST, 0, 0, 0, 0, 
                   C.SWP_NOMOVE|C.SWP_NOSIZE)
    return nil
}
```

#### macOS平台
```go
// 通过CGO调用Core Foundation
/*
#cgo LDFLAGS: -framework CoreGraphics -framework AppKit
#include <CoreGraphics/CoreGraphics.h>
#include <AppKit/AppKit.h>
*/
import "C"

// 防截图（部分支持）
func EnableAntiCaptureOnMac(windowID uint32) error {
    // 使用私有API或系统级权限控制
    // 注意：macOS限制较多，部分功能需要用户授权
    return setWindowCaptureProtection(windowID)
}
```

### 2.5.3 客户端防检测模块设计
```go
type AntiDetectionManager struct {
    enabled      bool
    platform     string
    windowHandle uintptr
    features     []AntiDetectionFeature
}

type AntiDetectionFeature interface {
    Name() string
    Enable() error  
    Disable() error
    IsSupported() bool
}

// 防检测功能工厂
func NewAntiDetectionManager(platform string) *AntiDetectionManager {
    features := []AntiDetectionFeature{
        NewAntiScreenshotFeature(platform),
        NewAntiRecordingFeature(platform), 
        NewAntiSwitchDetectionFeature(platform),
    }
    
    return &AntiDetectionManager{
        platform: platform,
        features: features,
    }
}
```

## 3. 组件详细说明

### 3.1 云端服务组件

#### Gateway API服务 (gateway-api)
**职责**：作为系统入口，处理所有外部请求
- **REST API**：用户管理、会话管理、文件上传、配置管理
- **WebSocket/SSE**：实时数据推送给Web前端
- **认证授权**：JWT Token签发与验证
- **限流熔断**：API调用频率限制、系统保护
- **路由转发**：将请求转发到对应的内部服务

#### Client Ingress服务 (client-ingress)  
**职责**：专门处理客户端gRPC连接
- **连接管理**：维护客户端长连接、心跳检测、断线重连
- **音频流处理**：接收音频分片、缓冲、VAD分段
- **会话生命周期**：会话创建、状态维护、资源清理
- **负载均衡**：支持水平扩展，客户端智能路由

#### ASR服务 (asr-service)
**职责**：语音识别抽象层
- **多厂商支持**：阿里云NLS、OpenAI Whisper、本地模型
- **实时转写**：流式识别、增量结果返回
- **用量统计**：识别时长统计、成本计算
- **质量优化**：音频预处理、降噪、自动增益

#### AI编排服务 (ai-orchestrator)
**职责**：AI能力的统一调度和编排
- **Prompt管理**：模板化Prompt、动态参数注入
- **RAG检索**：知识库查询、上下文构建、相关性排序
- **LLM调用**：多Provider路由、超时重试、结果缓存
- **结构化输出**：JSON Schema约束、结果验证
- **用量监控**：Token消耗统计、成本分析

#### 知识库服务 (kb-service)
**职责**：用户私有知识库管理
- **文档管理**：上传、解析、预处理（PDF/Word/Markdown/图片OCR）
- **文本切片**：智能分段、重叠窗口、元数据提取
- **向量生成**：嵌入模型调用、向量存储、索引构建
- **检索服务**：语义检索、混合检索、重排序
- **空间隔离**：多租户数据隔离、权限控制

#### 用量统计服务 (usage-service)
**职责**：系统资源使用量统计和计费准备
- **事件收集**：ASR秒数、LLM Token数、API调用次数
- **实时聚合**：用量累计、配额检查、超限告警
- **历史分析**：使用趋势、成本分析、优化建议
- **计费准备**：计费规则、账单生成、余额扣减

### 3.2 客户端组件架构 (Go)

#### 核心模块
```go
type ClientApplication struct {
    // 核心组件
    authManager     *AuthManager           // 认证管理
    audioCapture    *AudioCaptureManager   // 音频采集
    antiDetection   *AntiDetectionManager  // 防检测
    grpcClient      *GRPCClient           // gRPC通信
    configManager   *ConfigManager         // 配置管理
    
    // 运行时状态
    sessionID       string                 // 当前会话ID
    isRecording     bool                   // 录制状态
    connectionState ConnectionState        // 连接状态
}
```

#### AudioCaptureManager - 音频采集
**职责**：跨平台系统音频采集
- **设备枚举**：自动发现系统默认扬声器/麦克风
- **音频采集**：实时PCM音频流采集（16kHz/16bit/mono）
- **预处理**：音量调节、降噪、VAD检测
- **流控制**：采集开始/暂停/停止、实时监控

```go
type AudioCaptureManager struct {
    device       audio.Device
    stream       audio.Stream  
    sampleRate   int           // 16000Hz
    channels     int           // 1 (mono)
    bitDepth     int           // 16bit
    chunkSize    int           // 100-300ms chunks
}
```

#### AntiDetectionManager - 防检测功能
**职责**：实现防监控检测能力
- **平台适配**：Windows/macOS不同实现方式
- **功能开关**：用户可配置启用/禁用特定功能
- **权限申请**：自动申请必要的系统权限
- **兼容性检查**：系统版本兼容性验证

#### GRPCClient - 云端通信
**职责**：与云端服务的可靠通信
- **连接管理**：自动重连、连接池、负载均衡
- **流式传输**：音频分片上传、实时控制指令
- **压缩传输**：gzip压缩、带宽优化
- **错误处理**：网络异常、服务降级

### 3.3 Web前端组件

#### 技术栈推荐
- **框架**：React 18+ / Vue 3+ 
- **构建工具**：Vite
- **UI库**：Ant Design / Element Plus
- **状态管理**：Zustand / Pinia
- **实时通信**：Socket.io-client

#### 核心页面模块
- **登录注册页**：用户认证、找回密码
- **会话中心**：实时转写显示、AI建议展示、会话历史
- **知识库管理**：文档上传、搜索、分类管理
- **简历分析**：简历上传、AI诊断、优化建议
- **用量统计**：使用量监控、成本分析
- **系统设置**：偏好配置、API密钥管理

## 4. 数据流与时序
- 会话时序（简化）：
  1) 客户端登录获取 token；StartSession（metadata：岗位、语言、采样率等）。
  2) 客户端通过 gRPC 流按 100–300ms 大小发送音频分片（16k/16bit/mono 推荐）。
  3) client-ingress 进行 VAD/时长切片与缓冲，推送到 asr-service（并行队列）。
  4) asr-service 返回增量/最终转写；事件发布：TranscriptPartial/Final。
  5) ai-orchestrator 监听 Final/窗口聚合事件，做 RAG+LLM，发布 Suggestions/Answers 事件。
  6) gateway-api 将事件通过 WebSocket/SSE 推送给 Web 前端会话页面；持久化到 PG。
  7) 客户端定时上传检测截图（例如 2–5s 一次）到对象存储，服务仅保存 URL 与时间戳。
  8) 会话结束，汇总用量（ASR 秒数、LLM tokens），写 usage-service。

- 事件总线（进程内/Redis PubSub）：
  - Topics：session.<id>.transcript.partial/final、session.<id>.ai.suggestions、session.<id>.system.alert。

## 5. 接口设计（摘要）
- 认证
  - POST /api/v1/auth/register、/auth/login（返回 `access_token`，JWT，短期）
  - GET /api/v1/auth/me
- 会话管理
  - POST /api/v1/sessions（创建，返回 `session_id` 与 gRPC 端点）
  - POST /api/v1/sessions/{id}/stop
  - GET /api/v1/sessions/{id}（详情与结果摘要）
  - WS /api/v1/sessions/{id}/events（前端订阅）
- 客户端 gRPC（proto 摘要）
  - service Ingress { rpc Start(stream AudioChunk) returns (stream ServerEvent); }
  - message AudioChunk { bytes pcm16le = 1; int32 sample_rate = 2; int64 seq = 3; string session_id = 4; }
  - message ServerEvent { oneof payload { Ack ack=1; Transcript tr=2; Control ctl=3; } }
- 截图上传
  - POST /api/v1/sessions/{id}/screenshots（multipart，返回 oss_url）
- 知识库
  - POST /api/v1/kb/docs（上传文档）
  - GET /api/v1/kb/search?q=...（检索）
- 简历诊断
  - POST /api/v1/resume/analyze（表单：岗位描述、简历文件/文本）

备注：REST 返回 JSON；流式结果经 WS 推送；客户端走 gRPC（可回退 WebSocket）。

## 6. 数据模型（PG + pgvector）
- users(id, email, password_hash, created_at, ...)
- sessions(id, user_id, status, lang, sample_rate, started_at, ended_at, meta JSONB)
- transcripts(id, session_id, t0_ms, t1_ms, text, is_final)
- suggestions(id, session_id, source_transcript_id, content JSONB, created_at)
- screenshots(id, session_id, oss_url, captured_at)
- kb_spaces(id, user_id, name)
- kb_docs(id, space_id, title, oss_url, mime, tokens, created_at)
- kb_chunks(id, doc_id, chunk_idx, text, embedding vector)
- usage_events(id, user_id, session_id, kind ENUM(asr, llm, api), amount, unit, meta JSONB, at)

索引：sessions(user_id, started_at)、transcripts(session_id, t0_ms)、kb_chunks(embedding vector ivfflat)

## 7. AI 与 RAG 策略
- Prompt 模板：系统指令 + 场景（面试问答/要点提取/改写建议）。
- RAG：基于用户私有 kb_spaces 检索 top-k；使用 rerank（可选）提升相关性。
- Provider 选择：
  - LLM：OpenAI/DeepSeek/灵积；配置按场景路由（质量/成本/延迟）。
  - Embedding：与 LLM 同源或跨源；维度与 pgvector 匹配。
- 结构化输出：JSON schema（要点、建议、置信度、引用片段）。
- 低延迟：对 Final 段落触发主推理；Partial 仅用于前端字幕展示。

## 8. 客户端设计（最小化）
- 语言可选（Go/Rust/C#/Python），推荐 Go（跨平台无依赖）
- 能力：
  - 登录注册（保存刷新 token）；
  - 自动选择系统默认扬声器，采样 16k PCM；
  - gRPC 流式发送音频分片；
  - 定时截图（可开关与频率配置）；
  - 断线重连、序号递增、去重与重试；
  - 后台常驻与开机自启（用户可控）。
- 配置：本地加密保存（系统凭据库/加密文件）。

## 9. 可靠性、可观测性与治理
- 健康检查：/healthz、/readyz；gRPC 健康探针。
- 速率限制：IP+用户+会话级；音频分片大小/速率校验。
- 重试与幂等：chunk seq + ack；服务侧任务去重。
- 监控：
  - Prometheus 指标：qps、ASR 秒数、LLM tokens、延迟分位；
  - Tracing：会话/请求穿透；
  - 日志：结构化 JSON，关联 session_id。
- 异常处理：溢出策略（丢弃/降级）、断路器、超时预算。

## 10. 安全与合规
- 鉴权：JWT（短期）+ 刷新 Token；WS/gRPC 临时会话令牌。
- 传输安全：全链路 TLS；对象存储使用签名 URL。
- 数据保护：用户数据隔离；知识库按用户空间隔离；最小化保留策略与数据删除接口。
- 审计：关键操作写审计日志；
- 配置密钥：集中在 Secret 管理（env/密钥管理服务）。

## 11. 部署方案
- 本地开发：docker compose 启动 pg、redis、minio（OSS 模拟）；go services 本地运行。
- 生产：容器化（K8s/ACK 或 ECS + supervisord）；灰度与弹性扩缩容（asr/ai 编排可水平扩展）。
- 配置：env + 配置中心（可选）。

## 12. 目录结构建议
```
.
├── server/                 # Go 单体/模块化服务（可拆分）
│   ├── cmd/
│   ├── internal/
│   │   ├── api/           # REST/WS
│   │   ├── grpc/          # gRPC 服务实现
│   │   ├── asr/
│   │   ├── ai/
│   │   ├── kb/
│   │   ├── usage/
│   │   ├── store/         # pg, redis, oss
│   │   └── pkg/
│   └── proto/
├── web/                    # 前端（Vite + React/Svelte）
├── clients/
│   ├── go/
│   └── ...
├── docs/
│   └── ARCHITECTURE.md     # 本文件（也可置于根目录）
└── README.md
```

## 13. 里程碑
- M1 基础闭环（2–3 周）
  - 账户系统、会话创建、gRPC 音频上行、ASR 返回、前端 WS 字幕、用量统计（ASR 秒数）。
- M2 AI 与 KB（2–3 周）
  - RAG 接入、建议生成、简历上传与诊断、LLM 用量统计。
- M3 稳定性与体验（2 周）
  - 观测体系、降级策略、客户端自启与自动选择设备、UI 美化。

## 14. 与旧版兼容
- 复用 backup 中的 ASR/LLM 经验与 Prompt 模板；逐步替换为云端统一 Provider。
- 客户端仅保留采集逻辑；UI 迁移到 Web；功能开关通过服务端下发。

