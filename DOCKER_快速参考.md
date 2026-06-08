# 🐳 Docker 快速参考卡（v2 / 2026-06）

> 一页纸速查。详细文档见 `DOCKER_DEPLOY.md` / `README.md`。

---

## 🚀 一键启动

```bash
# 标准启动
docker-compose up -d

# 等待 30-60 秒冷启动（codes/workday 拉数据）
# 浏览器访问 http://localhost:8080
```

启动脚本：
- Windows: 双击 `docker-start.bat`
- Linux/Mac: `chmod +x docker-start.sh && ./docker-start.sh`

---

## 🆕 gbbq 按需拉取（v2 关键变更）

**v2 之后，gbbq 启动时不再自动拉取**（避免 9-15 分钟启动阻塞）。需要时手动触发：

```bash
# 全量刷新（11000+ 只，约 9-15 分钟）
curl -X POST http://localhost:8080/api/gbbq/refresh \
  -H "Content-Type: application/json" -d '{}' -m 900

# 单只刷新（几秒）
curl -X POST http://localhost:8080/api/gbbq/refresh \
  -H "Content-Type: application/json" -d '{"codes":["sh600000"]}'

# 查当前缓存大小
curl http://localhost:8080/api/health | grep gbbq_cache_size
```

**生产定时（crontab -e）**：每个工作日 17:00 跑全量（避开 15:00 收盘回填期）：

```cron
0 17 * * 1-5 curl -s -X POST http://localhost:8080/api/gbbq/refresh -H "Content-Type: application/json" -d '{}' -m 900 > /dev/null 2>&1
```

---

## 🔄 版本管理（v2 新增）

```bash
# 部署特定版本
VERSION=v1.2.3 docker-compose build
VERSION=v1.2.3 docker-compose up -d

# 回滚
VERSION=v1.2.2 docker-compose up -d

# 端口自定义
HOST_PORT=9090 docker-compose up -d
```

---

## ❤️ 健康检查（v2 增强）

| 端点 | 用途 |
|------|------|
| `GET /api/health` | 进程级健康 + 运行时指标（gbbq_cache_size / goroutines / memory_mb） |
| `GET /api/ready` | 就绪检查（语义：服务可接收 HTTP 请求） |

```bash
curl http://localhost:8080/api/health
curl http://localhost:8080/api/ready
```

gbbq 缓存是否为空**不再阻塞** `/api/ready`。通过 `/api/health` 的 `gbbq_cache_size` 字段监控数据是否到位。

---

## 🛠 容器管理

```bash
docker-compose up -d          # 启动
docker-compose stop          # 停止
docker-compose restart       # 重启
docker-compose ps            # 状态
docker-compose logs -f       # 实时日志
docker-compose logs --tail=100   # 最近 100 行
docker-compose down          # 完全清理
```

## 🖥 监控 + 诊断

```bash
docker stats tdx-stock-web           # 实时资源
docker top tdx-stock-web             # 容器进程
docker inspect tdx-stock-web         # 容器详情（含资源限制 / 日志策略）
docker exec -it tdx-stock-web sh     # 进入容器
docker system prune                  # 清理未用资源
```

## 🔧 故障排查

```bash
# 端口被占用
# Windows:    netstat -ano | findstr :8080
# Linux/Mac:  netstat -tulpn | grep :8080
lsof -i :8080

# 健康检查失败
docker logs tdx-stock-web
docker inspect tdx-stock-web | grep -A 5 "Health"

# 重新构建
docker-compose up -d --build
```

## 🆘 完全重置（清空数据卷！慎用）

```bash
docker-compose down --rmi all --volumes
docker-compose up -d --build
```

## 💾 镜像备份

```bash
# 导出
docker save -o stock-web.tar tdx-stock-web:latest

# 导入
docker load -i stock-web.tar
```

---

## 📊 v2 资源限制 + 日志策略（已内置）

| 项 | 旧版 | v2 |
|----|------|-----|
| 构建镜像 | `golang:1.22-alpine` | **`golang:1.26-alpine`** |
| 运行镜像 | `alpine:latest` | **`alpine:3.20`** |
| CPU 限制 | 1.0 | **2.0** |
| 内存限制 | 512M | **1G** |
| 日志轮转 | 无 | **json-file 10m × 3** |
| 权限 | `chown -R` 后置 | **`COPY --chown=appuser`** |

---

## ✅ 成功标志

```
Creating network "tdx-api_stock-network" with driver "bridge"
Creating tdx-stock-web ... done
```

`docker ps` 显示 `Up` 且 `STATUS` 为 `healthy`。

---

**详细文档**：`DOCKER_DEPLOY.md` / `README.md` / `CLAUDE.md`
