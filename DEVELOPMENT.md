# 面试助手系统 - 开发配置文档

## 1. 开发环境要求

### 1.1 基础环境
- **Go**: 1.22+ （云端服务 + 客户端开发）
- **Node.js**: 18+ （Web前端开发）
- **PostgreSQL**: 15+ （主数据库）
- **Redis**: 7+ （缓存和消息队列）
- **Docker**: 24+ （容器化部署）

### 1.2 开发工具
- **IDE**: VS Code / GoLand / WebStorm
- **API测试**: Postman / Insomnia
- **数据库工具**: TablePlus / pgAdmin
- **版本控制**: Git

### 1.3 平台特定要求

#### Windows开发环境
```bash
# 安装Go CGO依赖
# MinGW-w64 或者 Visual Studio Build Tools
choco install mingw-w64  # 使用Chocolatey

# 或者安装Visual Studio 2022 Community（包含MSVC）
```

#### macOS开发环境
```bash
# 安装Xcode Command Line Tools
xcode-select --install

# 安装Homebrew
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"

# 安装必要工具
brew install go node postgresql redis
```

#### Linux开发环境
```bash
# Ubuntu/Debian
sudo apt update
sudo apt install golang-go nodejs npm postgresql postgresql-contrib redis-server build-essential

# CentOS/RHEL
sudo dnf install golang nodejs npm postgresql postgresql-server redis gcc gcc-c++
```

## 2. 项目结构

```
msbd/
├── README.md                    # 项目说明
├── ARCHITECTURE.md              # 架构文档  
├── DEVELOPMENT.md               # 本开发文档
├── CLIENT_DESIGN.md             # 客户端设计文档
├── docker-compose.yml           # 开发环境容器
├── Makefile                     # 构建脚本
├── .env.example                 # 环境变量模板
├── .gitignore                   # Git忽略文件
│
├── server/                      # 云端服务(Go)
│   ├── cmd/                     # 应用入口
│   │   ├── gateway/             # Gateway API服务
│   │   ├── client-ingress/      # 客户端接入服务
│   │   ├── asr/                 # 语音识别服务
│   │   ├── ai/                  # AI编排服务
│   │   ├── kb/                  # 知识库服务
│   │   └── usage/               # 用量统计服务
│   ├── internal/                # 内部包
│   │   ├── auth/                # 认证授权
│   │   ├── config/              # 配置管理
│   │   ├── database/            # 数据库连接
│   │   ├── grpc/                # gRPC服务实现
│   │   ├── handlers/            # HTTP处理器
│   │   ├── middleware/          # 中间件
│   │   ├── models/              # 数据模型
│   │   ├── providers/           # 第三方服务封装
│   │   ├── services/            # 业务逻辑
│   │   └── utils/               # 工具函数
│   ├── pkg/                     # 可导出包
│   │   ├── events/              # 事件总线
│   │   ├── logger/              # 日志组件
│   │   └── validator/           # 数据验证
│   ├── proto/                   # Protobuf定义
│   │   ├── auth/
│   │   ├── session/
│   │   ├── asr/
│   │   └── common/
│   ├── migrations/              # 数据库迁移
│   ├── go.mod                   # Go模块定义
│   ├── go.sum                   # Go依赖锁定
│   └── Dockerfile               # 容器构建文件
│
├── client/                      # 客户端(Go)
│   ├── cmd/
│   │   └── main.go              # 客户端入口
│   ├── internal/
│   │   ├── anti_detection/      # 防检测模块
│   │   │   ├── manager.go
│   │   │   ├── windows.go       # Windows平台实现
│   │   │   ├── darwin.go        # macOS平台实现
│   │   │   └── linux.go         # Linux平台实现
│   │   ├── audio/               # 音频采集
│   │   ├── auth/                # 认证模块
│   │   ├── config/              # 配置管理
│   │   ├── grpc/                # gRPC客户端
│   │   ├── gui/                 # 简单GUI界面
│   │   └── tray/                # 系统托盘
│   ├── pkg/
│   │   └── proto/               # 共享Protobuf定义
│   ├── assets/                  # 静态资源
│   ├── go.mod
│   ├── go.sum
│   └── build/                   # 构建脚本
│       ├── Makefile
│       ├── windows.sh
│       ├── darwin.sh
│       └── linux.sh
│
├── web/                         # Web前端
│   ├── public/                  # 静态文件
│   ├── src/                     # 源码
│   │   ├── components/          # 通用组件
│   │   ├── pages/               # 页面组件
│   │   ├── services/            # API服务
│   │   ├── stores/              # 状态管理
│   │   ├── utils/               # 工具函数
│   │   └── styles/              # 样式文件
│   ├── package.json
│   ├── vite.config.js
│   └── Dockerfile
│
├── docs/                        # 项目文档
│   ├── api/                     # API文档
│   ├── deployment/              # 部署文档
│   └── development/             # 开发文档
│
├── scripts/                     # 自动化脚本
│   ├── build.sh                 # 构建脚本
│   ├── deploy.sh                # 部署脚本
│   ├── migrate.sh               # 数据库迁移
│   └── proto-gen.sh             # Protobuf生成
│
├── configs/                     # 配置文件
│   ├── development/             # 开发环境配置
│   ├── production/              # 生产环境配置
│   └── local/                   # 本地配置
│
└── backup/                      # 原版本备份
    └── ...                      # 原Python版本代码
```

## 3. 环境配置

### 3.1 创建环境变量文件
```bash
# 复制环境变量模板
cp .env.example .env
```

### 3.2 .env 配置示例
```bash
# 基础配置
ENV=development
PORT=8080
GRPC_PORT=9090

# 数据库配置
DB_HOST=localhost
DB_PORT=5432
DB_NAME=msbd_dev
DB_USER=msbd
DB_PASSWORD=your_password
DB_SSL_MODE=disable

# Redis配置
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# JWT配置
JWT_SECRET=your-super-secret-jwt-key-here
JWT_EXPIRES_IN=24h

# 对象存储配置 (阿里云OSS)
OSS_ENDPOINT=https://oss-cn-hangzhou.aliyuncs.com
OSS_ACCESS_KEY_ID=your-access-key-id
OSS_ACCESS_KEY_SECRET=your-access-key-secret
OSS_BUCKET_NAME=msbd-bucket

# 阿里云语音识别配置
ALIBABA_ACCESS_KEY_ID=your-nls-access-key-id  
ALIBABA_ACCESS_KEY_SECRET=your-nls-access-key-secret
ALIBABA_APP_KEY=your-nls-app-key
ALIBABA_REGION_ID=cn-shanghai

# LLM配置 - OpenAI
OPENAI_API_KEY=your-openai-api-key
OPENAI_BASE_URL=https://api.openai.com/v1

# LLM配置 - DeepSeek
DEEPSEEK_API_KEY=your-deepseek-api-key
DEEPSEEK_BASE_URL=https://api.deepseek.com/v1

# 监控配置
ENABLE_PROMETHEUS=true
PROMETHEUS_PORT=9091

# 日志配置
LOG_LEVEL=debug
LOG_FORMAT=json
```

## 4. 快速开始

### 4.1 启动开发环境
```bash
# 1. 启动数据库和Redis
docker-compose up -d postgres redis

# 2. 创建数据库和运行迁移
make migrate

# 3. 生成Protobuf文件
make proto

# 4. 启动后端服务
make run-server

# 5. 启动Web前端 (新终端窗口)
cd web && npm install && npm run dev

# 6. 构建客户端 (新终端窗口) 
cd client && make build
```

### 4.2 验证安装
```bash
# 检查服务健康状态
curl http://localhost:8080/api/v1/health

# 检查gRPC服务
grpcurl -plaintext localhost:9090 list

# 检查前端页面
open http://localhost:3000
```

## 5. 开发工作流

### 5.1 代码提交规范
```bash
# 提交信息格式
<type>(<scope>): <subject>

# 示例
feat(client): add anti-detection module for windows
fix(server): resolve audio streaming connection issue
docs(arch): update architecture design document
```

### 5.2 分支管理
- `main`: 主分支，稳定版本
- `develop`: 开发分支，集成最新功能
- `feature/*`: 功能分支
- `hotfix/*`: 热修复分支
- `release/*`: 发布分支

### 5.3 测试策略
```bash
# 单元测试
make test

# 集成测试
make test-integration

# 端到端测试
make test-e2e

# 性能测试
make test-performance
```

### 5.4 代码质量检查
```bash
# 代码格式化
make fmt

# 代码检查
make lint

# 安全漏洞扫描
make security-check

# 依赖更新检查
make deps-update
```

## 6. 调试配置

### 6.1 VS Code调试配置 (.vscode/launch.json)
```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Debug Server Gateway",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "${workspaceFolder}/server/cmd/gateway",
            "env": {
                "ENV": "development"
            },
            "args": []
        },
        {
            "name": "Debug Client",
            "type": "go", 
            "request": "launch",
            "mode": "auto",
            "program": "${workspaceFolder}/client/cmd",
            "env": {
                "ENV": "development"
            }
        }
    ]
}
```

### 6.2 日志配置
```yaml
# config/development/logging.yml
logging:
  level: debug
  format: json
  output: stdout
  fields:
    service: msbd
    version: v1.0.0
```

## 7. 部署配置

### 7.1 Docker构建
```bash
# 构建服务端镜像
docker build -f server/Dockerfile -t msbd-server:latest .

# 构建Web前端镜像  
docker build -f web/Dockerfile -t msbd-web:latest .

# 构建所有服务
make docker-build
```

### 7.2 生产环境部署
```bash
# 使用Docker Compose部署
docker-compose -f docker-compose.prod.yml up -d

# 或者使用Kubernetes
kubectl apply -f k8s/
```

## 8. 问题排查

### 8.1 常见问题

**问题1**: gRPC连接失败
```bash
# 检查端口是否被占用
netstat -an | grep 9090
# 检查防火墙设置
# 查看服务日志
```

**问题2**: 数据库连接失败
```bash
# 检查PostgreSQL状态
systemctl status postgresql
# 测试连接
psql -h localhost -U msbd -d msbd_dev
```

**问题3**: 客户端防检测功能不工作
```bash
# Windows: 检查UAC权限
# macOS: 检查系统权限设置
# 查看客户端日志
```

### 8.2 性能监控
- Prometheus指标: `http://localhost:9091/metrics`
- 应用日志: `docker logs <container_name>`
- 数据库性能: `SELECT * FROM pg_stat_activity;`

## 9. API文档

### 9.1 自动生成API文档
```bash
# 使用swag生成Swagger文档
make swagger

# 访问文档
open http://localhost:8080/swagger/index.html
```

### 9.2 gRPC文档
```bash
# 生成gRPC文档  
make grpc-docs
```

## 10. 最佳实践

### 10.1 代码规范
- 遵循Go官方代码风格
- 使用有意义的变量和函数命名
- 添加充分的注释和文档
- 错误处理要完整

### 10.2 安全考虑
- 敏感信息不要硬编码
- 使用环境变量管理配置
- API需要认证和授权
- 输入数据要验证和过滤

### 10.3 性能优化
- 使用连接池
- 合理设置超时时间
- 避免N+1查询问题
- 使用缓存提升性能

---

## 开发提示

1. **首次开发建议**：先熟悉整体架构，然后从Gateway API开始开发
2. **并行开发**：服务端和客户端可以并行开发，使用mock数据
3. **渐进开发**：先实现核心功能，再完善细节特性
4. **测试驱动**：边开发边写测试，保证代码质量

更多详细信息请参考：
- [架构设计文档](./ARCHITECTURE.md)
- [客户端设计文档](./CLIENT_DESIGN.md)  
- [API文档](./docs/api/)