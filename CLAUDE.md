# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

A Go library + HTTP/Web frontend for retrieving Chinese A-share stock data via the 通达信 (TDX) binary protocol, with a SQLite-backed `Codes` cache, a connection pool, and an `extend` package for batch K-line pulling and 同花顺 (THS) 前复权 (forward-adjusted) data. Forks [`injoyai/tdx`](https://github.com/injoyai/tdx).

The repository contains **two Go modules**:

| Path | Module | Purpose |
|------|--------|---------|
| `/` | `github.com/injoyai/tdx` | Core library: protocol, client, pool, codes, workday, manage |
| `/web` | `web` (local replace `=> ../`) | HTTP API + static frontend |

Always run `go` commands from the module that owns the code under edit. Edits inside `web/` need `cd web` first.

## Commands

### Build & run

```bash
# Library (root)
go build ./...

# HTTP server (must use "." — see caveat below)
cd web && go run .

# Docker
docker-compose up -d
docker-compose logs -f stock-web
docker-compose down
```

**Caveat:** The README explicitly forbids `go run server.go` from `web/`. The web package is `package main` spread across `server.go`, `server_api_extended.go`, `tasks.go`, and `data/` — running a single file skips the other handlers.

### Tests

```bash
# Protocol round-trip / parser tests live in /protocol
cd /Users/eee/Desktop/git_repos/tdx-api
go test ./protocol/...

# Extend (spider-ths has a live HTTP test — requires network to d.10jqka.com.cn)
go test ./extend/...
```

Test layout:
- `protocol/model_*_test.go` - 协议层单元测试(quote, kline, gbbq, history trade 等)
- `extend/spider-ths_test.go` - 同花顺前复权 HTTP 拉取,需要外网到 `d.10jqka.com.cn`

The library has no top-level `_test.go` files. The protocol package is the only place with unit tests.

### Live API smoke test

`scripts/run_api_checks.py` hits ~30 endpoints against a locally running server. Requires `requests` and the server on `127.0.0.1:8080`:

```bash
pip install requests
go run .  # in web/
python3 scripts/run_api_checks.py
```

## Architecture

### Layering

```
web/                      ← HTTP handlers (net/http, no framework)
  ├─ server.go            core endpoints (quote, kline, minute, trade, search, stock-info, tasks)
  ├─ server_api_extended.go  extended endpoints (~20 more, incl. kline-all/tdx, kline-all/ths)
  └─ tasks.go             in-memory TaskManager with context-cancel

extend/                   ← higher-level utilities built on tdx
  ├─ pull-kline.go        PullKline: cron-scheduled full-history K-line pull to SQLite
  ├─ pull-trade.go        PullTrade: same for tick data
  ├─ codes-server.go      standalone HTTP server for codes.db (/stocks, /etfs)
  ├─ codes-bj.go          BJ exchange code fetcher
  ├─ income.go            N-day return calc over K-lines
  ├─ spider-ths.go        THS HTTP scraper for 前复权/后复权 day K-line
  └─ ths-factor.go        forward/back adjustment factor computation

(/) protocol/             ← binary wire format (no I/O)
  ├─ frame.go             Frame encode/decode, ReadFrom rsc → Response
  ├─ const.go             message type codes (0x053E=quote, 0x052D=kline, ...)
  ├─ model_quote.go       5-level quote (Decode)
  ├─ model_kline.go       all K-line periods
  ├─ model_minute.go      intraday minute bars
  ├─ model_trade.go       intraday ticks
  ├─ model_history_*.go   date-scoped minute/trade pulls
  └─ types_price.go       Price is int64 in 厘 (1/100 yuan); Float64() helper

(/) client.go             ← Client (high-level), DialDefault, heartbeat + connect
(/) dial.go               ← 4 dial strategies: TCP / Host round-robin / Random / Range
(/) pool.go               ← bounded channel pool; Do() / Go() helpers
(/) codes.go              ← Codes backed by xorm (SQLite or MySQL) — code cache
(/) workday.go            ← Workday backed by xorm — trading-day calendar
(/) manage.go             ← Manage: pool + Codes + Workday + cron
(/) hosts.go              ← TDX server IPs (SH/BJ/GZ/WH) + FastHosts TCP ping
(/) client_bj_code.go     BJ-specific code list
```

### TDX protocol flow

1. `Client.Write(connect.Frame())` — handshake (model_connect.go).
2. Server pushes 30s heartbeat (`MHeart`) via `GoTimerWriter`.
3. Each request = `Frame{ MsgID, Control:0x01, Type:<msgCode>, Data }`. Single client, single TCP connection; concurrent calls are serialized through `Wait` (a 2s gate in `client.go:79`).
4. `client.Event.OnReadFrom = protocol.ReadFrom` splits the TCP stream into frames by `Prefix 0x0C`; `OnDealMessage` dispatches by `Type`.
5. Response `data` field is zlib-compressed for some message types (handled in `frame.go Decode`).

### Connection pooling

`Pool` (pool.go) is a fixed-size channel of `*Client`. `Manage` (manage.go) wraps it and adds `Codes`, `Workday`, and a `robfig/cron` scheduler. Use `Manage.Pool.Do(fn)` for one-off and `Pool.Go(fn)` for fire-and-forget.

`FastHosts` (hosts.go) TCP-pings all server IPs in parallel and sorts by latency — used to pick the best server on startup.

### `Codes` / `Workday` databases

- `DefaultDatabaseDir = "./data/database"` (created on first run).
- `tdx.DefaultCodes` is a package-level singleton initialized in `web/server.go:init()`.
- Use `NewCodesSqlite`/`NewCodesMysql` (in codes.go) to choose backend; `Workday` is the same pattern in workday.go.

### Frontend

- `web/static/index.html` + `app.js` + `style.css`. Vanilla JS, ECharts loaded from CDN.
- The `app.js` `switchTab` and ECharts resize call (see `docs/update-2025-11-10.md`) requires the `requestAnimationFrame + resize()` pattern — if charts render only on the left half, check this first.
- Server serves `/static/*` and a single `/` that returns `index.html`.

## Conventions

- **Response envelope** (all JSON handlers):
  ```json
  { "code": 0, "message": "success", "data": <any> }
  ```
  `code != 0` is an error. Use `successResponse(w, data)` and `errorResponse(w, msg)`.

- **Prices are in 厘** (× 0.01 yuan). Use `protocol.Price` and its `.Float64()` / `.String()` helpers — don't divide by hand.

- **Date strings**: `YYYYMMDD` for TDX history endpoints (e.g., `20241108`), `YYYY-MM-DD` for HTTP query params.

- **K-line types** accepted in `type=` query param: `minute1`, `minute5`, `minute15`, `minute30`, `hour`, `day`, `week`, `month`. Day/week/month return 前复权 (THS source) by default via `getQfqKlineDay` in `web/server.go`; minutes are unadjusted. Failure to reach `d.10jqka.com.cn` silently falls back to raw TDX data. `/api/kline-history-qfq` 支持 `day`/`week`/`month`,使用本地 gbbq 事件计算前复权,不依赖外部 HTTP;需 gbbq 缓存已填充。

- **Kline full-history endpoints** `/api/kline-all/tdx` and `/api/kline-all/ths` return `{ count, list, meta: { source, type, batch_limit, notes } }`. TDX底层单次返回最多 800 条 — the server concatenates batches. Set a generous client timeout (1–5s for long-history symbols).

- **Trade pull endpoints** also batch: `/api/trade-history/full?start_date=…&end_date=…` walks trading days and caps at `limit`.

- **Workday** queries auto-trigger `manager.Workday.Update()` when the table is empty — handled in `/api/workday/range`.

- **New date-range endpoints** (2025-11+, 计划文档 PLAN.md §5):
  - `GET /api/turnover?code=&start_date=&end_date=` - 个股换手率序列,内部走 `Client.GetKlineDayAll` + `gbbq.GetEquity`
  - `GET /api/gbbq?code=&start_date=&end_date=` - 个股股本变迁/除权除息,数据源为 gbbq 内存缓存
  - `POST /api/gbbq/refresh` - 主动刷新 gbbq 缓存,body `{"codes":[..]}`(可省),缺省 = 全量;**同步阻塞**,全量 9-15 分钟
  - `GET /api/kline-index-history?code=&start_date=&end_date=` - 指数/板块日 K,`code` 必须显式带交易所前缀
  - `GET /api/kline-history?code=&type=&start_date=&end_date=` - **修复了日期范围过滤,日/周/月走同花顺前复权(原始语义)**,**仅服务个股**;`Amount` 恒为 0,价格曲线连续无缝
  - `GET /api/kline-history-tdx?code=&type=&start_date=&end_date=` - 走通达信原始(不复权)数据;`Amount` 字段有真实值,除权日跳空
  - `GET /api/kline-history-ths?code=&type=&start_date=&end_date=` - `/api/kline-history` 的显式命名别名,语义一致(同花顺前复权)
  - `GET /api/kline-history-qfq?code=&type=&start_date=&end_date=` - 仿射前复权数据,使用 TDX 原始 K 线 + gbbq 事件本地计算,与 THS QFQ 结果一致但不依赖外部 HTTP;gbbq 缓存必须已填充(否则返回错误,需先 `POST /api/gbbq/refresh`);`Amount` 有真实调整值(优于 THS 的 Amount=0)
  - `GET /api/market-snapshot` - 全市场 5300+ 只股票当日 OHLCV 断面(`client.GetDaySnapshot`,**同步阻塞** 4-15 分钟,客户端要 `curl -m 900`);`tdx-api` 纯中转,不复权/不计算/不入库
  - `GET /api/health` - 进程级健康检查(PLAN_v2 §2.3.3 增强版):返回 `status` / `time`(unix 秒) / `uptime_seconds` / `gbbq_cache_size` / `goroutines` / `memory_mb`;已切到标准信封,给 docker healthcheck / k8s liveness 用
  - `GET /api/ready` - 就绪检查(PLAN_v2 §2.3.4 新增):返回 `{ready:true, uptime_seconds}`;gbbq 缓存是否为空不再阻塞 ready,给 k8s readiness probe / 反向代理 upstream 用
  - 所有上述端点共享 `parseKlineDateRange` / `inDateRange` 辅助函数(`web/server_api_extended.go`)
  - 实时行情 NDJSON (commit eccd882 之后, 见 `docs/realtime-usage.md` 完整使用指南):
    - `GET /api/realtime/quote?codes=600000.SH,000001.SZ` - **NDJSON 流式推送**, 1 秒 1 轮批量 `client.GetQuote`; fan-out 架构只占 1 个 Pool slot; 含价/量/5档/盘中量比
    - `GET /api/realtime/health` - broker 状态 (poll 次数/最近错误/订阅者数)
    - `POST /api/realtime/preheat` - 主动预热量比窗口, body `{"codes":[...], "ratio_basis":5}`
    - `GET /api/realtime/codes` - 已预热量比窗口列表
    - 实现细节在 `web/server_realtime.go`,零协议/连接层改动;代码格式归一化接受 `600000.SH` / `SH600000` / `600000` 三种入参

- **Tasks** (`web/tasks.go`): in-memory only, not persisted. Each task has a `context.CancelFunc`; cancellation sets status to `cancelled` and ends the task. Test scripts cancel tasks on exit.

- **Chinese comments everywhere** — the project is bilingual by intent. Keep new comments in 简体中文 to match.

- **No formal linter configured.** No `.golangci.yml`, no `gofumpt`/`gofmt` hook in CI. `go vet ./...` is the minimum sanity check.

## Docker

Multi-stage build with 国内镜像 sources (Alpine + Go proxy both pointed at Aliyun). Builds inside `web/` (`go build -o ../stock-web .`) then copies the binary + `web/static/` to a non-root `appuser` Alpine image. Healthcheck hits `/api/health` via `wget --spider`. `TZ=Asia/Shanghai` is set in both image and compose file — keep this; the protocol date math assumes Shanghai.

**PLAN_v2 §2 落地后的生产化要点**：

- 版本固定：`golang:1.26-alpine`（构建）/ `alpine:3.20`（运行）。`latest` 不可控，某天升级可能破坏兼容；1.22 缺 `time.DateOnly` 等新特性。
- 构建缓存优化：`Dockerfile` 拆成两层——第一层只 COPY `go.mod`/`go.sum` + `go mod download`；第二层 COPY 源码 + 编译。源码改动不破坏 go mod 下载层。
- 权限收敛：用 `COPY --chown=appuser:appuser ...` 替代后置 `chown -R /app`（仅 `/app/data` 仍需 chown，因为是 RUN mkdir 创建的）。
- 镜像可独立 tag：`image: tdx-stock-web:${VERSION:-latest}`，配合 `VERSION=v1.2.3 docker-compose build` 支持版本回滚。
- 端口可配置：`ports: "${HOST_PORT:-8080}:8080"` + `environment: PORT=8080`（与 `web/server.go` 的 `PORT` env var 配套）。
- 资源限制：`deploy.resources.limits: { cpus: '2.0', memory: 1G }`——防止 OOM 拖垮宿主机。
- 日志轮转：`logging: { driver: json-file, options: { max-size: '10m', max-file: '3' } }`——防止容器日志把磁盘吃满。
- 健康检查：`HEALTHCHECK` 保留 `start-period=600s` 兜底（gbbq 按需拉取后实际冷启动秒级，但 codes/workday 仍要从 TDX 拉一次）。

**健康/就绪端点**：
- `GET /api/health`（已增强）— 进程级健康 + 运行时指标：`status` / `time`（unix 秒，不再写死）/ `uptime_seconds` / `gbbq_cache_size` / `goroutines` / `memory_mb`（MB）。已切到标准信封 `{code,message,data}`，老格式 `{"status":..,"time":..}` 不再保留。
- `GET /api/ready`（新增）— 就绪检查，返回 `{ready:true, uptime_seconds}`。语义：服务可接收 HTTP 请求即可。gbbq 缓存是否为空不再阻塞 ready（缓存空时 `/api/turnover` 等端点仍 200，由调用方按需 `POST /api/gbbq/refresh`）。

## Common pitfalls

- **Don't `go run server.go`** — silently drops `server_api_extended.go` and `tasks.go`. Always `go run .` in `web/`.
- **`tdx.DefaultCodes` is package-level.** Multiple test runs in the same process share state.
- **`/api/kline` day/week/month** depends on outbound to `d.10jqka.com.cn`. Behind a firewall, they degrade silently to TDX raw data — log a warning if you need to detect this.
- **TDX servers change IPs.** `hosts.go` was last updated 2024-11-30; servers go offline without notice. `FastHosts` is the resilience layer, not a permanent server list.
- **Single client per connection.** Don't spawn many goroutines hammering one `*Client`; use the `Pool` (via `Manage`) instead. Concurrent calls on one client serialize behind a 2s gate.
- **`gbbq` 改为按需拉取(PLAN_v2 §3)** — `NewGbbq` 不再启动 cron 也不再调用 `Update()`,空缓存冷启动是秒级。需要数据时调 `POST /api/gbbq/refresh`(单只秒级,全量约 9-15 分钟)。`/api/turnover` 在 gbbq 缓存空时返回 turnover=0,**不会**自动补拉;查询路径与更新路径已解耦。
- **`/api/market-snapshot` 是耗时长端点(PLAN_v2 §4)** — 单线程串行拉全市场 5300+ 只,**同步阻塞 4-15 分钟**。HTTP 客户端必须设大超时(`curl -m 900`)。响应体积 ≈ 1MB(5300+ × 200B)。失败模式宽松(单只失败不阻断)。建议调用时序:每个交易日 16:00 之后(避开 15:00 收盘数据回填期)。`run_api_checks.py` 把这个端点标为 `slow=True`,默认不跑,需加 `--slow` 启用。
