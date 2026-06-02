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

- **K-line types** accepted in `type=` query param: `minute1`, `minute5`, `minute15`, `minute30`, `hour`, `day`, `week`, `month`. Day/week/month return 前复权 (THS source) by default via `getQfqKlineDay` in `web/server.go`; minutes are unadjusted. Failure to reach `d.10jqka.com.cn` silently falls back to raw TDX data.

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
  - 所有上述端点共享 `parseKlineDateRange` / `inDateRange` 辅助函数(`web/server_api_extended.go`)

- **Tasks** (`web/tasks.go`): in-memory only, not persisted. Each task has a `context.CancelFunc`; cancellation sets status to `cancelled` and ends the task. Test scripts cancel tasks on exit.

- **Chinese comments everywhere** — the project is bilingual by intent. Keep new comments in 简体中文 to match.

- **No formal linter configured.** No `.golangci.yml`, no `gofumpt`/`gofmt` hook in CI. `go vet ./...` is the minimum sanity check.

## Docker

Multi-stage build with 国内镜像 sources (Alpine + Go proxy both pointed at Aliyun). Builds inside `web/` (`go build -o ../stock-web .`) then copies the binary + `web/static/` to a non-root `appuser` Alpine image. Healthcheck hits `/api/health` via `wget --spider`. `TZ=Asia/Shanghai` is set in both image and compose file — keep this; the protocol date math assumes Shanghai.

## Common pitfalls

- **Don't `go run server.go`** — silently drops `server_api_extended.go` and `tasks.go`. Always `go run .` in `web/`.
- **`tdx.DefaultCodes` is package-level.** Multiple test runs in the same process share state.
- **`/api/kline` day/week/month** depends on outbound to `d.10jqka.com.cn`. Behind a firewall, they degrade silently to TDX raw data — log a warning if you need to detect this.
- **TDX servers change IPs.** `hosts.go` was last updated 2024-11-30; servers go offline without notice. `FastHosts` is the resilience layer, not a permanent server list.
- **Single client per connection.** Don't spawn many goroutines hammering one `*Client`; use the `Pool` (via `Manage`) instead. Concurrent calls on one client serialize behind a 2s gate.
- **`gbbq` 改为按需拉取(PLAN_v2 §3)** — `NewGbbq` 不再启动 cron 也不再调用 `Update()`,空缓存冷启动是秒级。需要数据时调 `POST /api/gbbq/refresh`(单只秒级,全量约 9-15 分钟)。`/api/turnover` 在 gbbq 缓存空时返回 turnover=0,**不会**自动补拉;查询路径与更新路径已解耦。
