# 📈 TDX通达信股票数据查询系统

> 基于通达信协议的股票数据获取库 + Web可视化界面 + RESTful API

[![Go Version](https://img.shields.io/badge/Go-1.26+-00ADD8?style=flat&logo=go)](https://golang.org)
[![Docker](https://img.shields.io/badge/Docker-支持-2496ED?style=flat&logo=docker)](https://www.docker.com)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**感谢源作者 [injoyai](https://github.com/injoyai/tdx)，请支持原作者！**

> **v2 更新（2026-06-02，对应 PLAN_v2）**：
> - Docker 镜像版本固定（Go 1.26 / alpine 3.20），支持 `VERSION` 标签回滚
> - 资源限制（2 CPU / 1G）+ 日志轮转（10m × 3）
> - `/api/health` 增强版（gbbq_cache_size / goroutines / memory_mb）+ 新增 `/api/ready`
> - gbbq 改为按需拉取（**启动时不再自动拉**，需手动 `POST /api/gbbq/refresh` 触发）
> - 36 个 HTTP 端点（v1 32 个，新增 §3 gbbq/refresh + §4 market-snapshot 等）

---

## ✨ 功能特性

| 分类 | 功能 |
|-----|------|
| **📊 核心功能** | 实时行情（五档盘口）、K线数据（10种周期）、分时数据、股票搜索、批量查询 |
| **🌐 Web界面** | 现代化UI、ECharts图表、智能搜索、实时刷新 |
| **🔌 RESTful API** | 32个接口、完整文档、多语言示例、高性能 |
| **🐳 Docker部署** | 开箱即用、国内镜像加速、跨平台支持 |

---

## 🚀 快速开始

### 方式一：Docker部署（推荐）⭐

```bash
# 克隆项目
git clone https://github.com/oficcejo/tdx-api.git
cd tdx-api

# 启动服务（v2 已固定 Go 1.26 / alpine 3.20，已配置国内镜像加速）
docker-compose up -d

# 访问 http://localhost:8080
# 等待 30-60 秒首次冷启动 (codes / workday 初始化)
```

**v2 关键变更：gbbq 缓存按需拉取**

v2 之后，服务**启动时不再自动拉取 gbbq 数据**（避免 9-15 分钟的启动阻塞）。
需要时手动触发：

```bash
# 全量刷新（11000+ 只，约 9-15 分钟，客户端要 -m 900）
curl -X POST http://localhost:8080/api/gbbq/refresh \
  -H "Content-Type: application/json" -d '{}' -m 900

# 单只刷新（几秒）
curl -X POST http://localhost:8080/api/gbbq/refresh \
  -H "Content-Type: application/json" -d '{"codes":["sh600000"]}'

# 查看当前缓存大小
curl http://localhost:8080/api/health | grep gbbq_cache_size
```

**版本管理（v2 新增）**：

```bash
# 部署特定版本
VERSION=v1.2.3 docker-compose up -d
# 回滚
VERSION=v1.2.2 docker-compose up -d
```

**一键启动脚本：**
- Windows: 双击 `docker-start.bat`
- Linux/Mac: `chmod +x docker-start.sh && ./docker-start.sh`

### 方式二：源码运行

```bash
# 前置要求: Go 1.22+

# 1. 下载依赖
go mod download

# 2. 进入web目录并运行
cd web
go run .

# 3. 访问 http://localhost:8080
```

> ⚠️ **注意**: 必须使用 `go run .` 编译所有Go文件，不能使用 `go run server.go`

---

## � API接口列表

### 核心接口

| 接口 | 说明 | 示例 |
|-----|------|------|
| `/api/quote` | 五档行情 | `?code=000001` |
| `/api/kline` | K线数据 | `?code=000001&type=day` |
| `/api/minute` | 分时数据 | `?code=000001` |
| `/api/trade` | 分时成交 | `?code=000001` |
| `/api/search` | 搜索股票 | `?keyword=平安` |
| `/api/stock-info` | 综合信息 | `?code=000001` |

### 扩展接口

| 接口 | 说明 |
|-----|------|
| `/api/codes` | 获取股票代码列表 |
| `/api/batch-quote` | 批量获取行情 |
| `/api/kline-history` | 历史K线数据（**v2 恢复同花顺前复权语义**） |
| `/api/kline-history-tdx` | 历史K线（v2 新增，TDX 原始不复权，`Amount` 有真实值） |
| `/api/kline-history-ths` | 历史K线（v2 显式命名别名，同 `kline-history`） |
| `/api/kline-index-history` | 指数/板块历史K线 |
| `/api/kline-all` | 完整K线数据 |
| `/api/kline-all/tdx` | TDX源K线数据 |
| `/api/kline-all/ths` | 同花顺源K线数据（含前复权） |
| `/api/index` | 指数数据 |
| `/api/index/all` | 全部指数数据 |
| `/api/market-stats` | 市场统计 |
| `/api/market-count` | 市场数量统计 |
| `/api/market-snapshot` | **v2-§4 新增**全市场当日 OHLCV 断面（5300+ 只，4-15 分钟） |
| `/api/stock-codes` | 股票代码 |
| `/api/etf-codes` | ETF代码 |
| `/api/etf` | ETF列表 |
| `/api/trade-history` | 历史成交 |
| `/api/trade-history/full` | 完整历史成交 |
| `/api/minute-trade-all` | 全部分时成交 |
| `/api/workday` | 交易日查询 |
| `/api/workday/range` | 交易日范围 |
| `/api/income` | 收益数据 |
| `/api/turnover` | **v2 新增**个股换手率序列（依赖 gbbq 缓存） |
| `/api/gbbq` | **v2 新增**股本变迁/除权除息（依赖 gbbq 缓存） |
| `/api/gbbq/refresh` | **v2-§3 新增**主动刷新 gbbq 缓存（POST，同步阻塞） |
| `/api/tasks/pull-kline` | 创建K线拉取任务 |
| `/api/tasks/pull-trade` | 创建成交拉取任务 |
| `/api/tasks` | 任务列表 |
| `/api/server-status` | 服务器状态 |
| `/api/health` | **v2 增强**健康检查（含 gbbq_cache_size/goroutines/memory_mb） |
| `/api/ready` | **v2 新增**就绪检查 |

**完整API文档**: [API_接口文档.md](API_接口文档.md)

---

## � 使用示例

### API调用

```bash
# 获取实时行情
curl "http://localhost:8080/api/quote?code=000001"

# 获取日K线
curl "http://localhost:8080/api/kline?code=000001&type=day"

# 搜索股票
curl "http://localhost:8080/api/search?keyword=平安"

# 健康检查
curl "http://localhost:8080/api/health"
```

### Go库使用

```go
import "github.com/injoyai/tdx"

// 连接服务器
c, _ := tdx.DialDefault(tdx.WithDebug(false))

// 获取行情
quotes, _ := c.GetQuote("000001", "600519")

// 获取日K线
kline, _ := c.GetKlineDayAll("000001")
```

---

## 🐳 Docker配置说明

### 国内镜像加速

Docker配置已使用国内镜像源，加速构建：

| 组件 | 镜像源 |
|-----|-------|
| Go基础镜像 | `registry.cn-hangzhou.aliyuncs.com/library/golang` |
| Alpine镜像 | `registry.cn-hangzhou.aliyuncs.com/library/alpine` |
| Alpine APK | `mirrors.aliyun.com` |
| Go Proxy | `goproxy.cn` + `mirrors.aliyun.com/goproxy` |

### v2 版本固定（替代 latest）

| 阶段 | 镜像 | 备注 |
|------|------|------|
| 构建 | `golang:1.26-alpine` | 固定大版本，1.22 缺 `time.DateOnly` 等新特性 |
| 运行 | `alpine:3.20` | 避免 `latest` 某天升级破坏兼容 |

### v2 资源限制 + 日志轮转

```yaml
deploy:
  resources:
    limits:
      cpus: '2.0'   # 1.0 → 2.0
      memory: 1G    # 512M → 1G
logging:
  driver: json-file
  options:
    max-size: '10m'   # 单文件最大
    max-file: '3'     # 保留 3 个文件
```

### 常用命令

```bash
# 启动
docker-compose up -d

# 健康检查
curl http://localhost:8080/api/health    # 进程级健康 + 运行时指标
curl http://localhost:8080/api/ready     # 就绪检查

# 日志 / 重启 / 停止
docker-compose logs -f
docker-compose restart
docker-compose stop
docker-compose down

# 版本回滚
VERSION=v1.2.2 docker-compose up -d
```

**详细部署文档**: [DOCKER_DEPLOY.md](DOCKER_DEPLOY.md)

---

## 📊 支持的数据类型

| 数据类型 | 方法 | 说明 |
|---------|------|------|
| 五档行情 | `GetQuote` | 实时买卖五档、最新价、成交量 |
| 1/5/15/30/60分钟K线 | `GetKlineXXXAll` | 分钟级K线数据 |
| 日/周/月K线 | `GetKlineDayAll` 等 | 中长期K线数据 |
| 分时数据 | `GetMinute` | 当日每分钟价格 |
| 分时成交 | `GetTrade` | 逐笔成交记录 |
| 股票列表 | `GetCodeAll` | 全市场代码 |

---

## 📁 项目结构

```
tdx-api/
├── client.go              # TDX客户端核心
├── protocol/              # 通达信协议实现
├── web/                   # Web应用
│   ├── server.go          # 主服务器
│   ├── server_api_extended.go  # 扩展API
│   ├── tasks.go           # 任务管理
│   └── static/            # 前端文件
├── extend/                # 扩展功能
├── Dockerfile             # Docker镜像（国内源）
├── docker-compose.yml     # Docker编排
└── docs/                  # 文档
```

---

## � 相关资源

| 资源 | 链接 |
|-----|------|
| 原项目 | [injoyai/tdx](https://github.com/injoyai/tdx) |
| API文档 | [API_接口文档.md](API_接口文档.md) |
| Docker部署 | [DOCKER_DEPLOY.md](DOCKER_DEPLOY.md) |
| Python示例 | [API_使用示例.py](API_使用示例.py) |

### 通达信服务器

系统自动连接最快的服务器：

| IP | 地区 |
|----|------|
| 124.71.187.122 | 上海(华为) |
| 122.51.120.217 | 上海(腾讯) |
| 121.36.54.217 | 北京(华为) |
| 124.71.85.110 | 广州(华为) |

---

## ⚠️ 免责声明

1. 本项目仅供学习和研究使用
2. 数据来源于通达信公共服务器，可能存在延迟
3. 不构成任何投资建议，投资有风险

---

## 📄 许可证

MIT License - 详见 [LICENSE](LICENSE)

---

**如果这个项目对您有帮助，请点个 Star ⭐ 支持一下！**
