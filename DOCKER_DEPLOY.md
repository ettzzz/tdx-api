# 🐳 Docker部署指南

> **2026-06-02 v2 更新说明**（对应 PLAN_v2 §2 落地）
>
> 相比 2024-11 旧版文档，本次更新反映了以下生产化变更：
> - **版本固定**：构建阶段 `golang:1.26-alpine`、运行阶段 `alpine:3.20`（替代 `latest`，避免某天升级破坏兼容）
> - **构建缓存优化**：`Dockerfile` 拆两层（go mod 单独一层，源码改动不破坏 go mod 缓存）
> - **权限收敛**：`COPY --chown=appuser:appuser` 替代后置 `chown -R /app`（仅 `/app/data` 仍需 chown）
> - **镜像可独立 tag**：`image: tdx-stock-web:${VERSION:-latest}` 配合 `VERSION=v1.2.3 docker-compose up -d` 支持版本回滚
> - **端口可配置**：`ports: "${HOST_PORT:-8080}:8080"` + 容器内 `PORT=8080`（与 `web/server.go` 的 `PORT` env var 配套）
> - **资源限制**：`cpus: 2.0` + `memory: 1G`（旧版是 1.0 / 512M）
> - **日志轮转**：`json-file` driver + `max-size: 10m` × `max-file: 3`
> - **健康检查端点升级**：`/api/health` 增强版（含 gbbq_cache_size / goroutines / memory_mb 等运行时指标），新增 `/api/ready` 就绪检查
> - **gbbq 按需拉取（PLAN_v2 §3）**：`/api/turnover` / `/api/gbbq` 等依赖 gbbq 缓存的端点在容器启动时**不会**自动拉数据，需要手动 `POST /api/gbbq/refresh` 触发（详见下文"gbbq 按需拉取"章节）
>
> 文档保留旧版基础内容（容器管理、故障排查、常用命令等），这些仍适用。

## 📋 概述

使用Docker部署TDX股票数据查询系统，无需配置Go环境，一键启动！

---

## 🎯 优势

✅ **无需Go环境** - Docker容器内置所有依赖  
✅ **一键部署** - 简单的命令即可启动  
✅ **环境隔离** - 不影响主机系统  
✅ **跨平台** - Windows/Linux/Mac统一方案  
✅ **易于管理** - 启动/停止/重启非常方便  

---

## 📦 前置要求

### 安装Docker

#### Windows系统

**方法一：Docker Desktop（推荐）**

1. 下载Docker Desktop
   - 官网：https://www.docker.com/products/docker-desktop/
   - 选择Windows版本下载

2. 运行安装程序
   - 双击安装包
   - 按向导完成安装
   - 重启电脑

3. 启动Docker Desktop
   - 双击桌面图标
   - 等待Docker启动完成（状态显示为绿色）

4. 验证安装
   ```powershell
   docker --version
   docker-compose --version
   ```

**方法二：手动安装Docker Engine**

适用于Windows Server或不使用Docker Desktop的场景。

#### Linux系统

```bash
# Ubuntu/Debian
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# CentOS/RHEL
sudo yum install -y docker
sudo systemctl start docker
sudo systemctl enable docker

# 添加当前用户到docker组
sudo usermod -aG docker $USER

# 验证安装
docker --version
docker-compose --version
```

#### Mac系统

1. 下载Docker Desktop for Mac
2. 安装.dmg文件
3. 启动Docker
4. 验证安装

---

## 🚀 快速开始

### 方式一：使用docker-compose（推荐）

#### 1. 进入项目目录
```powershell
cd C:\Users\Administrator\Downloads\tdx-master
```

#### 2. 构建并启动
```powershell
docker-compose up -d
```

这个命令会：
- 📦 自动构建Docker镜像
- 🚀 启动容器
- 🔌 映射端口到本机8080

#### 3. 查看日志
```powershell
# 查看实时日志
docker-compose logs -f

# 看到以下信息表示启动成功：
# 成功连接到通达信服务器
# 服务启动成功，访问 http://localhost:8080
```

#### 4. 访问应用
打开浏览器访问：http://localhost:8080

#### 5. 停止服务
```powershell
docker-compose down
```

---

### 方式二：使用docker命令

#### 1. 构建镜像
```powershell
docker build -t tdx-stock-web:latest .
```

#### 2. 运行容器
```powershell
docker run -d \
  --name tdx-stock-web \
  -p 8080:8080 \
  --restart unless-stopped \
  tdx-stock-web:latest
```

#### 3. 查看日志
```powershell
docker logs -f tdx-stock-web
```

#### 4. 停止容器
```powershell
docker stop tdx-stock-web
docker rm tdx-stock-web
```

---

## 📝 常用命令

### 容器管理

```powershell
# 查看运行中的容器
docker ps

# 查看所有容器（包括停止的）
docker ps -a

# 启动容器
docker-compose start

# 停止容器
docker-compose stop

# 重启容器
docker-compose restart

# 删除容器和网络
docker-compose down

# 删除容器、网络和镜像
docker-compose down --rmi all
```

### 日志查看

```powershell
# 查看最近100行日志
docker-compose logs --tail=100

# 实时查看日志
docker-compose logs -f

# 查看特定时间的日志
docker-compose logs --since="2024-11-03T14:00:00"

# 只查看错误日志
docker-compose logs | findstr "error"
```

### 进入容器

```powershell
# 进入容器shell
docker exec -it tdx-stock-web sh

# 执行命令
docker exec tdx-stock-web ls -la

# 查看容器内环境变量
docker exec tdx-stock-web env
```

### 镜像管理

```powershell
# 查看镜像列表
docker images

# 删除镜像
docker rmi tdx-stock-web:latest

# 清理未使用的镜像
docker image prune

# 查看镜像详细信息
docker inspect tdx-stock-web:latest
```

---

## ⚙️ 配置说明

### docker-compose.yml 配置项

```yaml
services:
  stock-web:
    build:
      context: .              # 构建上下文
      dockerfile: Dockerfile  # Dockerfile路径
    
    container_name: tdx-stock-web  # 容器名称
    
    ports:
      - "8080:8080"          # 端口映射 主机:容器
    
    restart: unless-stopped   # 重启策略
    
    environment:
      - TZ=Asia/Shanghai     # 时区设置
    
    networks:
      - stock-network        # 网络配置
```

### 修改端口

如果8080端口被占用，修改`docker-compose.yml`：

```yaml
ports:
  - "9090:8080"  # 将主机端口改为9090
```

然后访问：http://localhost:9090

### 环境变量

可以在`docker-compose.yml`中添加环境变量：

```yaml
environment:
  - TZ=Asia/Shanghai
  - DEBUG=true
  - LOG_LEVEL=info
```

---

## 🔍 故障排查

### 问题1：Docker命令不可用

**症状**：
```
docker : 无法将"docker"项识别为 cmdlet、函数、脚本文件或可运行程序的名称
```

**解决方案**：
1. 确认Docker Desktop已安装并启动
2. 查看系统托盘是否有Docker图标
3. 重启Docker Desktop
4. 重启PowerShell终端

### 问题2：构建失败 - 网络问题

**症状**：
```
ERROR: failed to solve: golang:1.21-alpine: error getting credentials
```

**解决方案**：
```powershell
# 配置Docker镜像加速（国内）
# 在Docker Desktop设置中添加：
{
  "registry-mirrors": [
    "https://docker.mirrors.ustc.edu.cn",
    "https://registry.docker-cn.com"
  ]
}
```

### 问题3：端口被占用

**症状**：
```
Error starting userland proxy: listen tcp4 0.0.0.0:8080: bind: Only one usage...
```

**解决方案**：
```powershell
# 方法1：停止占用端口的程序
netstat -ano | findstr :8080
taskkill /PID <进程ID> /F

# 方法2：修改docker-compose.yml中的端口映射
ports:
  - "9090:8080"
```

### 问题4：容器启动后立即退出

**症状**：
```
docker ps -a  # 显示Exited状态
```

**解决方案**：
```powershell
# 查看容器日志
docker logs tdx-stock-web

# 查看详细错误信息
docker-compose logs
```

### 问题5：无法访问网页

**症状**：浏览器无法打开 http://localhost:8080

**排查步骤**：
```powershell
# 1. 确认容器正在运行
docker ps

# 2. 检查端口映射
docker port tdx-stock-web

# 3. 查看容器日志
docker logs tdx-stock-web

# 4. 测试容器内部服务
docker exec tdx-stock-web wget -O- http://localhost:8080

# 5. 检查防火墙设置
# Windows防火墙 → 允许应用通过防火墙 → Docker
```

### 问题6：构建速度慢

**解决方案**：

1. **使用镜像加速**（已在Dockerfile中配置）
   ```dockerfile
   ENV GOPROXY=https://goproxy.cn,direct
   ```

2. **使用构建缓存**
   ```powershell
   # Docker会自动缓存构建层
   # 第二次构建会快很多
   ```

3. **多阶段构建优化**（已实现）
   ```dockerfile
   # 第一阶段：构建（包含完整Go环境）
   # 第二阶段：运行（只包含二进制文件）
   # 最终镜像大小：约20MB
   ```

---

## 📊 监控和维护

### 查看容器状态

```powershell
# 查看容器资源使用
docker stats tdx-stock-web

# 查看容器详细信息
docker inspect tdx-stock-web

# 查看容器进程
docker top tdx-stock-web
```

### 健康检查

容器配置了自动健康检查，**v2 升级要点**：

```yaml
healthcheck:
  test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/api/health"]
  interval: 30s       # 每30秒检查一次
  timeout: 3s         # 超时时间3秒
  retries: 3          # 失败3次后标记为unhealthy
  start_period: 600s  # 启动后 600 秒才开始检查（gbbq 已按需，但 codes/workday 仍要从 TDX 拉一次）
```

**v2 健康检查端点**：

| 端点 | 用途 | 适用场景 |
|------|------|----------|
| `GET /api/health` | 进程级健康 + 运行时指标 | docker `HEALTHCHECK` / k8s liveness probe / 监控系统 |
| `GET /api/ready` | 服务可接收 HTTP 请求 | k8s readiness probe / 反向代理 upstream |

**`/api/health` 响应（v2 增强版，标准信封）**：
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "status":          "healthy",
    "time":            1780409238,
    "uptime_seconds":  54,
    "gbbq_cache_size": 5525,
    "goroutines":      25,
    "memory_mb":       58
  }
}
```

**`/api/ready` 响应**：
```json
{
  "code": 0,
  "message": "success",
  "data": {"ready": true, "uptime_seconds": 54}
}
```

**重要语义变化**（v2）：
- gbbq 缓存是否为空**不再**阻塞 `/api/ready`——缓存空时 `/api/turnover` 等端点仍 200，由调用方按需 `POST /api/gbbq/refresh` 触发
- 监控 `gbbq_cache_size` 字段判断数据是否到位：`0` 表示尚未拉过

查看健康状态：
```powershell
docker ps  # 查看HEALTH列
```

手动测试：
```bash
# 容器内
docker exec tdx-stock-web wget -q -O - http://localhost:8080/api/health
docker exec tdx-stock-web wget -q -O - http://localhost:8080/api/ready
# 宿主机
curl http://localhost:8080/api/health
```

### 备份和恢复

```powershell
# 导出容器为镜像
docker commit tdx-stock-web tdx-stock-web-backup:v1.0

# 保存镜像到文件
docker save -o tdx-stock-web-backup.tar tdx-stock-web:latest

# 从文件加载镜像
docker load -i tdx-stock-web-backup.tar
```

---

## 🔄 更新和升级

### 🆕 gbbq 按需拉取（v2 关键变更）

> **这是 v2 最重要的行为变化，请仔细阅读。**

v2 之前，gbbq（股本变迁 / 除权除息）数据在容器启动时**自动**从 TDX 拉取全市场 11000+ 只股票（耗时 9-15 分钟）。
v2 之后，gbbq 启动时**不再自动拉取**——服务可用时间从 ~10 分钟降到 < 5 秒，但 `gbbq_cache_size: 0`。

**哪些端点受 gbbq 缓存影响？**
- `GET /api/turnover`（个股换手率序列）—— 缓存空时返回 `turnover=0`
- `GET /api/gbbq`（股本变迁 / 除权除息）—— 缓存空时返回 `equity: []` / `xrxd: []`
- `POST /api/gbbq/refresh`（**新增端点**）—— 主动触发拉取

**拉取数据（首次部署后必跑）**：

```bash
# 全量刷新 (11000+ 只, 约 9-15 分钟, 同步阻塞, 客户端要 -m 900)
curl -X POST http://localhost:8080/api/gbbq/refresh \
  -H "Content-Type: application/json" \
  -d '{}' \
  -m 900

# 单只刷新 (几秒)
curl -X POST http://localhost:8080/api/gbbq/refresh \
  -H "Content-Type: application/json" \
  -d '{"codes":["sh600000","sz000001"]}'

# 响应示例 (全量)
# {
#   "code": 0, "message": "success",
#   "data": {
#     "success_count": 5525,
#     "failed_count": 0,
#     "failed": {},
#     "duration_ms": 542000
#   }
# }
```

**生产建议：用 crontab 定期刷新**

```bash
# crontab -e
# 每个工作日 17:00 跑一次全量 (避开 15:00 收盘数据回填期, 16:00 后拉"当天"数据)
0 17 * * 1-5 curl -s -X POST http://localhost:8080/api/gbbq/refresh -H "Content-Type: application/json" -d '{}' -m 900 > /dev/null 2>&1
```

**容器内查看当前缓存大小**：

```bash
docker exec tdx-stock-web wget -q -O - http://localhost:8080/api/health | grep gbbq_cache_size
# 输出: "gbbq_cache_size": 5525,
```

**常见疑问**：
- Q: 不跑 refresh 会怎样？
- A: 服务**完全可用**，`/api/turnover` 返回 0（不是 bug），其他端点（行情/K线等）不受影响
- Q: 每天什么时机跑最好？
- A: 每个交易日 16:00 之后（避开 15:00 收盘数据回填期）
- Q: 失败的单只股票会怎样？
- A: 宽松模式——单只失败 `logs.Warnf` 后 continue，不阻断整批。响应里 `failed` map 记录失败 code → error

---

### 更新应用

```powershell
# 1. 停止并删除旧容器
docker-compose down

# 2. 拉取最新代码
git pull  # 如果使用Git

# 3. 重新构建并启动
docker-compose up -d --build

# 4. 查看日志确认启动成功
docker-compose logs -f
```

### 版本管理

**v2 升级要点**：通过 `VERSION` 环境变量独立 tag 镜像，支持版本回滚。

```powershell
# 标准 v2 流程: 用 VERSION tag 镜像 + docker-compose
VERSION=v1.2.3 docker-compose build
VERSION=v1.2.3 docker-compose up -d

# 镜像会同时带 latest 和具体版本两个 tag
#   tdx-stock-web:latest
#   tdx-stock-web:v1.2.3

# 回滚到上一个版本
VERSION=v1.2.2 docker-compose up -d

# 列出所有版本
docker images tdx-stock-web
```

**手动 docker build**（旧版命令，保留供参考）：

```powershell
# 构建带版本标签的镜像
docker build -t tdx-stock-web:v1.0.0 .

# 使用特定版本
docker run -d \
  --name tdx-stock-web \
  -p 8080:8080 \
  tdx-stock-web:v1.0.0
```

---

## 🌐 生产环境部署

### 使用环境变量文件

创建 `.env` 文件：
```bash
# .env
TZ=Asia/Shanghai
PORT=8080
LOG_LEVEL=info
```

修改 `docker-compose.yml`：
```yaml
services:
  stock-web:
    env_file:
      - .env
    ports:
      - "${PORT}:8080"
```

### 数据持久化（如需要）

```yaml
services:
  stock-web:
    volumes:
      - ./data:/app/data      # 数据目录
      - ./logs:/app/logs      # 日志目录
```

### 反向代理（Nginx）

```yaml
services:
  nginx:
    image: nginx:alpine
    ports:
      - "80:80"
    volumes:
      - ./nginx.conf:/etc/nginx/nginx.conf
    depends_on:
      - stock-web
    networks:
      - stock-network
```

---

## 📈 性能优化

### 1. 镜像优化

✅ 已实现多阶段构建  
✅ 使用Alpine Linux（体积小）  
✅ 编译时使用 `-ldflags="-s -w"` 减小二进制文件  

最终镜像大小：**约20MB**

### 2. 资源限制

**v2 升级要点**：v2 默认 2.0 CPU / 1G 内存（旧版是 1.0 / 512M）。gbbq 全量刷新时内存峰值约 200-400MB，1G 足够。

```yaml
services:
  stock-web:
    # v2 资源限制
    deploy:
      resources:
        limits:
          cpus: '2.0'   # v2: 1.0 → 2.0
          memory: 1G    # v2: 512M → 1G
    # v2 日志轮转 (防止容器日志把磁盘吃满)
    logging:
      driver: json-file
      options:
        max-size: '10m'   # 单文件最大 10MB
        max-file: '3'     # 保留 3 个文件 = 30MB 总上限
```

### 3. 容器优化

```yaml
services:
  stock-web:
    security_opt:
      - no-new-privileges:true
    read_only: true
    tmpfs:
      - /tmp
```

---

## ✅ 完整部署检查清单

部署前检查：
- [ ] Docker已安装并启动
- [ ] 8080端口未被占用
- [ ] 网络连接正常
- [ ] 有足够的磁盘空间（至少500MB）

部署步骤：
- [ ] 进入项目目录
- [ ] 运行 `docker-compose up -d`
- [ ] 查看日志确认启动
- [ ] 浏览器访问测试

验证成功：
- [ ] 容器状态为 `Up`
- [ ] 健康检查显示 `healthy`
- [ ] 可以访问 http://localhost:8080
- [ ] 能够搜索和查看股票数据

---

## 🎉 快速命令参考

```powershell
# 一键启动
docker-compose up -d

# 查看状态
docker-compose ps

# 查看日志
docker-compose logs -f

# 重启服务
docker-compose restart

# 停止服务
docker-compose stop

# 完全清理
docker-compose down

# 重新构建
docker-compose up -d --build
```

---

## 📞 获取帮助

### 常用诊断命令

```powershell
# Docker版本信息
docker version
docker-compose version

# Docker系统信息
docker info

# 查看Docker磁盘使用
docker system df

# 清理系统
docker system prune -a
```

### 下一步

Docker部署成功后，您可以：

1. ✅ 访问 http://localhost:8080 使用应用
2. ✅ 查看 `web/DEMO.md` 了解功能
3. ✅ 查看 `web/USAGE.md` 学习使用技巧
4. ✅ 根据需要修改配置

---

**祝您部署顺利！** 🐳🚀

有任何问题，请查看故障排查章节或反馈给我。

