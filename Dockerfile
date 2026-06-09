# 多阶段构建 - 第一阶段：构建
# 使用官方镜像（如果国内拉取慢，可以配置docker daemon的registry-mirrors）
# 固定 Go 版本 (1.22 缺 time.DateOnly 等新特性; latest 不可控)
FROM golang:1.26-alpine AS builder

# 替换Alpine镜像源为阿里云
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories

# 设置工作目录
WORKDIR /app

# 设置Go代理（使用国内镜像加速）
ENV GO111MODULE=on \
    GOPROXY=https://goproxy.cn,https://mirrors.aliyun.com/goproxy/,direct \
    GOTOOLCHAIN=auto \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=arm64

# ---- 第 1 层: 仅 Go 模块清单 (提升 build cache 命中率) ----
# 这一层只依赖 go.mod / go.sum, 源码改动不会让 go mod 下载重跑
COPY go.mod go.sum ./
RUN go mod download

# ---- 第 2 层: 源码 + 编译 ----
COPY . .

# 在子 shell 中编译，避免模块路径混淆问题
RUN go mod tidy && (cd web && go build -ldflags="-s -w" -o ../stock-web .)

# 多阶段构建 - 第二阶段：运行
# 固定 Alpine 小版本 (latest 可能某天 glibc 升级破坏兼容)
FROM alpine:3.20

# 替换Alpine镜像源为阿里云，安装必要的运行时依赖
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk --no-cache add ca-certificates tzdata wget

# 设置时区为上海
ENV TZ=Asia/Shanghai

# 创建非root用户
RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

# 设置工作目录
WORKDIR /app

# ===================================================================
# 【语法修正】
# 从构建阶段复制编译好的二进制文件 (--chown 避免后续 chown -R 整个 /app)
COPY --from=builder --chown=appuser:appuser /app/stock-web .
# ===================================================================

# ===================================================================
# 【语法修正】
# 复制静态文件 (--chown 同样避免 root 操作)
COPY --from=builder --chown=appuser:appuser /app/web/static ./static
# ===================================================================

# 预创建数据目录 (codes.db / workday.db / gbbq.db 持久化),
# 由 appuser 持有, 避免后续 chown -R
RUN mkdir -p /app/data/database && chown -R appuser:appuser /app/data

# 切换到非root用户
USER appuser

# 暴露端口
EXPOSE 8080

# 健康检查
# start-period=600s 保留: 即使 gbbq 按需拉取, codes/workday 启动时仍需从 TDX 拉,
# 600s 缓冲期可以保证健康检查不误报 unhealthy (冷启动几秒完成但留余量更稳)
HEALTHCHECK --interval=30s --timeout=3s --start-period=600s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/api/health || exit 1

# 启动应用
CMD ["./stock-web"]