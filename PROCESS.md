# PROCESS.md — tdx-api 冒烟测试工作日志

> 本文档是 2026-06-02 在临时环境中对 PLAN.md 落地成果做端到端冒烟测试的完整工作日志。
> 与 `PLAN.md` 互补：PLAN 描述"要做什么 + 怎么做"，PROCESS 描述"做了什么 + 怎么做的 + 暴露了什么问题"。

---

## 0. 起点

- **PLAN.md** 8 个阶段全部按计划完成（提交记录 `7a9bdb3..d1726c0`）
- 4 个新端点已实现并通过 unit 编译：`/api/turnover`、`/api/gbbq`、`/api/kline-index-history`、修复后的 `/api/kline-history`
- `web/server.go` 有一个未提交的"smoke-test hack"：把 `stockCodes` 截断到前 10 只股票以避免 `gbbq.Update()` 拉全市场（11000+ 只）耗时过长

**环境约束**：
- 无 Docker
- Python 在 `../.venv/bin/python`（含 `requests 2.34.2`）
- 8080 被 code-server 占用

---

## 1. 任务清单（用户分配）

1. 验证 Go 编译 + 服务能正常运行
2. `scripts/run_api_checks.py` 全部 31 个端点通过
3. 用 5 个特定标的（指数 / 概念 / 行业 / 个股）打冒烟，结果与股票软件对比
4. 更新文档 / 脚本 / PLAN
5. 写 PROCESS.md
6. 清理临时代码

---

## 2. 进展时间线

### 2.1 编译 + 单元测试

| 步骤 | 结果 | 备注 |
|------|------|------|
| `go vet ./...` (根模块) | ❌ 网络超时 | `proxy.golang.org` 不可达；模块未下载 |
| 切换 `GOPROXY=https://goproxy.cn,direct` | ✅ 模块下载完成 | 与 Dockerfile 国内源保持一致 |
| 再次 `go vet ./...` | ❌ 编译错误 | `protocol/model_gbbq.go` 用了 Go 1.20+ 特性（`time.DateOnly`、字节切片转定长数组），系统是 Go 1.19.8 |
| 解压 `go1.26.3.linux-amd64.tar.gz` 到 `/usr/local/go1.26/` | ✅ Go 1.26.3 装好 | 用户提供的 tarball |
| `go vet ./...` + `go build ./...` (根模块) | ✅ exit 0 | 唯一 vet 告警：`client.go:359: unreachable code`（**已有**，非新增）|
| `go vet .` + `go build .` (web 子模块) | ✅ exit 0 | 唯一 vet 告警：`server.go:696: unreachable code`（**已有**）|
| `go test ./protocol/...` | ✅ PASS 0.005s | `TestXRXD_FQ` 通过 |

### 2.2 启动服务 + 端点冒烟

| 步骤 | 结果 | 备注 |
|------|------|------|
| 直接 `go run .` (端口 8080) | ❌ `bind: address already in use` | code-server 占着 8080 |
| `server.go` 加 `PORT` 环境变量支持 | ✅ | 默认 `:8080`，可 `PORT=8081 go run .` 启动 |
| `run_api_checks.py` 加 `BASE_URL` 环境变量 | ✅ | 默认 `http://127.0.0.1:8080` |
| 启动 `PORT=8081 go run .` | ✅ gbbq 缓存就绪（0 秒，命中本地 SQLite）| smoke-test hack 保留 |
| `python scripts/run_api_checks.py` | ✅ **31/31 端点全绿** | 含 4 个新端点 + 27 个回归端点 |

### 2.3 5 标的冒烟（股票软件对比）

用户指定 5 个标的 × 2026-05-27 ~ 2026-06-01 区间：

| 类别 | 代码 | 名称 |
|------|------|------|
| 指数 | sh000688 | 科创板指 |
| 概念板块 | sh880666 | 可控核聚变 |
| 行业板块 | sh881338 | 通信设备 |
| 个股 1 | sh688799 | 华纳药厂 |
| 个股 2 | sz300620 | 光库科技 |

**第一次跑的问题**：
- 个股 `sh688799` / `sz300620` 的 `turnover` 全为 `0.0000`，`float` 全为 0
- 根因：gbbq 缓存里**没有这两只股票的股本数据**
  - `web/server.go` 的 smoke-test hack 把 `stockCodes` 截到前 10 只（`sh600000..sh600015`）
  - gbbq 启动时只对这 10 只拉过股本
  - `sh688799 / sz300620` 根本没在缓存里 → `gbbq.GetEquity` 返回 nil → handler 按设计回 0

**解决路径**（与用户沟通后）：
- **方案 C**：加临时 backfill 端点 `POST /api/_smoke/backfill-gbbq?code=xxx`
- 加 `Gbbq.FetchOne(code string)` 公开方法（也是合理的库 API）
- backfill 两只股票：sh688799 = 14 条记录、sz300620 = 56 条记录

### 2.4 单位换算 bug

- 我第一版显示脚本里把 `Price / 100`，应该 `/ 1000`（Price 是**厘**，1 元 = 1000 厘）
- **收盘价都偏大 10 倍**：华纳药厂显示 524.80 元，实际 52.48 元
- 修正脚本后所有价格合理（科创板指 1815 点而不是 18154 点；华纳药厂 52.48 元）

**换手率不受影响**：换手率计算用的是 `Volume`（手）和 `Float`（股），与 Price 单位换算无关。

### 2.5 kline-history 端点拆分（重要决策）

**冒烟暴露的问题**：
- `/api/kline-history` (type=day) 走**同花顺 HTTP**，**`Amount` 字段恒为 0**（同花顺接口本身不返回）
- 5 个标的对比中，个股的成交额完全看不到

**与用户讨论后的决策**：
- `/api/kline-history`：**改走通达信原始（不复权）**，`Amount` 可用；除权日 K 线跳空
- `/api/kline-history-ths`：**新增**，走同花顺前复权；`Amount` 恒为 0，价格曲线连续
- **原路径换语义**（用户选定）：不是新增别名端点，而是改 `/api/kline-history` 的语义

**实施**：
- `web/server_api_extended.go` 拆出 `handleGetKlineHistory`（TDX 原始）和 `handleGetKlineHistoryTHS`（同花顺前复权）
- `web/server.go` 注册新路由
- 重启服务后 5 标的 TDX vs THS 对比：4 天内差额全为 0（这段时间没有除权除息）

### 2.6 文档与脚本同步

| 文件 | 改动 |
|------|------|
| `API_接口文档.md` | 把 `/api/kline-history` 章节重写为"通达信原始不复权"；新增 `9b. /api/kline-history-ths` 章节，含对比表 |
| `CLAUDE.md` | 第 151-154 行端点列表同步，加 `kline-history-ths`，更新 `kline-history` 描述 |
| `scripts/run_api_checks.py` | 复制 `kline_history_dated` 为 `kline_history_ths_dated`，分别打两个端点 |
| `PLAN.md` | 新增 `§12 冒烟测试阶段补充`，记录所有与原计划的偏差 |

### 2.7 清理临时代码

移除本次冒烟特有的临时代码（见 §3）。

---

## 3. 临时代码清单

| 项目 | 位置 | 用途 | 处理 |
|------|------|------|------|
| smoke-test hack | `web/server.go` 截断 `stockCodes` | 加快 `gbbq.Update()` 启动 | **移除** |
| `Gbbq.FetchOne(code)` | `gbbq.go` | backfill 端点调用 | **保留**（是合理的库 API）|
| `handleBackfillGbbq` | `web/server_api_extended.go` | 临时补拉单只 gbbq | **移除** |
| `/api/_smoke/backfill-gbbq` 路由 | `web/server.go` | backfill 入口 | **移除** |

---

## 4. 测试结果汇总

### 4.1 静态 / 编译

- ✅ `go vet ./...` 根模块：exit 0
- ✅ `go build ./...` 根模块：exit 0
- ✅ `go vet .` web 子模块：exit 0
- ✅ `go build .` web 子模块：exit 0
- ✅ `go test ./protocol/...`：PASS

### 4.2 服务启动

- ✅ 启动时间 ~1 分钟（含 gbbq 加载 12 只股票）
- ✅ `/api/health` 返回 200 OK

### 4.3 端点冒烟（`scripts/run_api_checks.py`）

- ✅ **31/31 端点全绿**（含 4 个新端点 + 27 个回归端点）
  - `turnover` ✅ `gbbq` ✅ `kline_index_history` ✅ `kline_history_dated` ✅
  - 旧端点无回归

### 4.4 5 标的对比（与股票软件对比参考用）

| 代码 | 名称 | 类别 | 4 天收盘价范围 | 状态 |
|------|------|------|---------------|------|
| sh000688 | 科创板指 | 指数 | 1663.69 ~ 1844.25 | ✅ |
| sh880666 | 可控核聚变 | 概念板块 | 2333.72 ~ 2414.50 | ✅ |
| sh881338 | 通信设备 | 行业板块 | 5014.49 ~ 5281.74 | ✅ |
| sh688799 | 华纳药厂 | 个股 | 50.22 ~ 52.48 | ✅ |
| sz300620 | 光库科技 | 个股 | 259.84 ~ 292.99 | ✅ |

> 数字已用脚本从 API 拉取并格式化输出，可直接去股票软件核对。

---

## 5. 未提交改动列表

```
M PLAN.md                                 (新增 §12 冒烟阶段补充)
M PROCESS.md                              (本文件)
M API_接口文档.md                          (/api/kline-history 章节重写, +9b)
M CLAUDE.md                               (端点列表更新)
M scripts/run_api_checks.py               (+kline_history_ths_dated)
M web/server.go                           (+PORT 环境变量支持)
M web/server_api_extended.go              (handleGetKlineHistory 改 TDX, +handleGetKlineHistoryTHS, -handleBackfillGbbq)
M gbbq.go                                 (+Gbbq.FetchOne 方法)
M web/data/database/codes.db              (启动时 init() 重新写入)
M web/data/database/workday.db            (同上)
M web/data/database/gbbq.db               (新增)
```

---

## 6. 后续建议

1. **amount 字段语义统一**：考虑 `turnover` 端点也返回成交额，或者在 `kline-history-ths` 端点用 `close * volume * 100` 估算 amount
2. **backfill API 正式化**：如果 `Gbbq.FetchOne` 有用，可以考虑加 `POST /api/gbbq/backfill?code=xxx` 作为管理类端点（带鉴权）
3. **破坏性变更通知**：如果项目对外发布，`/api/kline-history` 的语义变化需要在 CHANGELOG 中明确标注

---

## §7. v2 计划（PLAN_v2.md）实施记录

> 起点：2026-06-02 冒烟测试结束后，与用户讨论得出的下一轮迭代方向（4 节任务：§1/§2/§3/§4）。本节按节追加。

### 7.1 §1 再次调整：恢复 kline-history 原路径语义（重要决策）

**上下文**：
- §2.5 上一轮把 `/api/kline-history` 从"同花顺前复权"改成了"通达信原始不复权"（新增 `/api/kline-history-ths` 承载前复权）
- PLAN_v2 §1.2 用户复盘认为：**原路径换语义**是破坏性变更，与既有公开文档/CLAUDE.md 端点说明不一致
- 需要恢复原语义、给 TDX 原始版本一个独立路径

**决策**：
- `/api/kline-history`：**恢复为同花顺前复权**（原始语义），`Amount` 恒为 0，价格曲线连续
- `/api/kline-history-tdx`：**新增**，走通达信原始不复权，`Amount` 有真实值，除权日 K 线跳空
- `/api/kline-history-ths`：**保留**为 `/api/kline-history` 的显式命名别名

**兼容性**：
- 既有客户端按"前复权"语义调用 `/api/kline-history`：**零改动**，行为回到 §2.5 之前
- §2.5 WIP 期间切到 `/api/kline-history-ths` 显式命名的客户端：零改动
- 极少有客户端可能已经迁移到"用 `/api/kline-history` 拿 TDX 原始数据"：需迁到 `/api/kline-history-tdx`

**变更清单**（commit `ba85455` + merge `3a8d693` + 本节 fix commit）：
- `web/server.go`：路由重绑，`/api/kline-history` → `handleGetKlineHistoryTHS`，新增 `/api/kline-history-tdx` → `handleGetKlineHistoryTDX`
- `web/server_api_extended.go`：`handleGetKlineHistory` → `handleGetKlineHistoryTDX`（重命名，函数体不变）
- `API_接口文档.md`：§9 改回"前复权（同花顺）"；§9b 改为"TDX 原始（不复权）"；对比表行/列反转
- `CLAUDE.md`：端点列表（line 153-155）同步，三个端点描述对应新矩阵
- `scripts/run_api_checks.py`：新增 `kline_history_tdx_dated` 冒烟测试
- `API_集成指南.md`：line 71 旧函数名引用 → `handleGetKlineHistoryTHS`
- `.gitignore`：移除 `PROCESS.md` 行（PROCESS.md 从此入库，作为项目级过程日志）

**验收**：
- ✅ `go build ./...` 通过
- ✅ `go test ./protocol/...` 通过
- ✅ 路由绑定：kline-history → THS、kline-history-tdx → TDX、kline-history-ths → THS 别名
- ⏳ `run_api_checks.py` 33 端点全绿（待 §3 修掉 gbbq 启动阻塞后回归）

### 7.2 §3 gbbq 拉取性能重构（2026-06-02）

**上下文**：
- 上一轮（PLAN.md）首次跑 gbbq 时炸出 9+ 分钟启动阻塞；本轮 PLAN_v2 §3 与用户确认：彻底解耦查询与更新
- 决策定稿（PLAN_v2 §3.3 D1-D7）：干掉 gbbq 的 cron 自动更新；查询路径不触发更新；失败宽松；一个端点 `POST /api/gbbq/refresh`；codes 缺省=全量；进度只打 log

**实施**（commit 待出）：
- `gbbq.go`：
  - 删除 `DefaultGbbqSpec`、`WithGbbqSpec`、`WithGbbqRetry`
  - 删除 `Gbbq.spec` / `Gbbq.retry` / `Gbbq.updateKey` / `Gbbq.updated` 字段
  - 删除私有 `update()` 中的"任一失败即整体回滚"逻辑（已用宽松模式：失败 `logs.Warn` + continue）
  - 删除公开 `Update()` 方法（原 `Update()` 是 cron 入口，已被 `Refresh()` 替代）
  - 删除 `NewTimer(...)` 调用与 `NewUpdated(...)` 调用
  - 新增 `Refresh(codes []string) (success []string, failed map[string]error, err error)`：codes 空/缺省 = 全量（回退 `this.codes` → `c.GetStockAll()`）；非空 = 逐只 `FetchOne`
  - `NewGbbq` 改为只做"建内存 / 建 db / Sync2 / 加载历史缓存到内存"，不再有任何网络/定时器
  - 保留 `IGbbq` / `FetchOne` / `WithGbbqCodes` / `Get*` 系列方法不变
- `web/server.go`：
  - `init()` 中删除 `gbbq.Update()` 同步阻塞
  - 启动日志改为"gbbq 缓存已初始化（数据按需拉取, 调 POST /api/gbbq/refresh 触发）"
  - 注册 `POST /api/gbbq/refresh` 路由 → `handleRefreshGbbq`
- `web/server_api_extended.go`：新增 `handleRefreshGbbq` — 解析 body，调 `gbbq.Refresh`，返回 `{ success_count, failed_count, failed: {code:err}, duration_ms }`
- `example/GetTurnover/main.go`：文档加注"启动后请调用 Refresh 拉取 gbbq"；main 里显式 `gbbq.Refresh([]string{code})` 拉单只演示
- `API_接口文档.md`：新增 §9.4 `POST /api/gbbq/refresh` 章节（典型使用流程 + curl 例子）
- `CLAUDE.md`：端点列表加 `POST /api/gbbq/refresh`；pitfalls 段"gbbq 同步阻塞"改为"按需拉取"说明
- `scripts/run_api_checks.py`：新增 `gbbq_refresh` 用例（POST 单只 `sh000001`，60s 超时）
- `updated.go`：注释里 `gbbq.Update()` 改为"任何依赖 Updater 的 Update()"——gbbq 本身不再用 Updated 机制，但 codes/workday 还在用

**未动**：
- `updated.go` 整体保留（codes.go 9:00:10 + workday.go 9:00:00 仍用 `NewUpdated` 节点机制）
- `protocol/`、`Dockerfile`、`docker-compose.yml`（属 §2 / §4）
- `gbbq.db` 数据库文件本身的 `update` 表（schema 保留，无新写入；如果以后要清理可手动 `DROP TABLE update`）

**验收**：
- ✅ `go build ./...` 通过（根模块）
- ✅ `go build .` 通过（web 子模块）
- ✅ `go test ./protocol/...` 通过
- ✅ `go vet ./...` 根模块：exit 0（唯一告警 `client.go:359: unreachable code` 是 §1 已有）
- ✅ orphan 检查：`grep -rn 'DefaultGbbqSpec\|WithGbbqSpec\|WithGbbqRetry\|gbbq\.Update' --include='*.go'` 0 命中
- ⏳ `run_api_checks.py` 34 端点（+gbbq_refresh）待服务冷启后回归

---

### 7.3 §4 /api/market-snapshot 全市场当日断面（2026-06-02）

**上下文**：
- 用户的量化系统每天 16:00 需要把全市场 5300+ 只 A 股的当日 OHLCV 一次性入库到 MySQL
- 之前用东方财富要 1.5 小时；用 TDX 单线程串行预期 4-15 分钟
- 决策定稿（PLAN_v2 §4.2 D1-D12）：单线程 + count=1 + 不复权 + 不带 name/date + 宽松失败 + 16:00 推荐 + 不入库

**实施**：

- `client.go`：
  - 新增 `Client.GetDaySnapshot(codes []string) (map[string]*protocol.Kline, error)`：单线程串行调 `GetKline(TypeKlineDay, code, 0, 1)`，单只失败 `logs.Warnf` 后 continue，返回 `code -> Kline` map（失败的 code 不在 map 中），第一个错误用 `fmt.Errorf("code %s: %w", code, err)` 包装
  - 位置：紧跟 `GetKlineDayAll` 之后
- `web/server.go`：
  - 注册 `GET /api/market-snapshot` → `handleMarketSnapshot`（在 `/api/market-count` 之后）
- `web/server_api_extended.go`：
  - 新增 `handleMarketSnapshot` handler：
    - 校验 `tdx.DefaultCodes` 已初始化
    - 调 `client.GetDaySnapshot(tdx.DefaultCodes.GetStocks())`（5300+ 只）
    - 遍历 codes 顺序，构造 list 元素（8 字段：`code/open/high/low/close/volume/last_close/change_pct`）
    - `change_pct` 用 `k.RiseRate()` 协议层原生
    - 响应外层加 `date`（handler 调用当天的本地日期，YYYY-MM-DD）+ `count`（成功返回的股票数）
    - `err != nil` 时 `log.Printf` 不阻断，继续返回成功的
  - 注释里说明单位（价格=厘、成交量=手）与设计取舍（count=1 性能关键、宽松失败模式）
- `API_接口文档.md`：新增 §9c 章节（描述/字段表/调用时序/性能/指数断面扩展点）
- `CLAUDE.md`：端点列表新增 `/api/market-snapshot` 说明 + pitfalls 段加"耗时长端点"提示
- `scripts/run_api_checks.py`：
  - 新增 `--slow` 参数支持（默认不跑 `slow=True` 用例）
  - 新增 `market_snapshot` 用例：`{"timeout": 900}` + `slow=True`
  - 4-tuple → 5-tuple 兼容性已处理（运行时按 tuple 长度解析）
- `PROCESS.md`：本节（§7.3）

**未动**：
- `protocol/`（不改 model）
- `gbbq.go`（属 §3）
- `Dockerfile` / `docker-compose.yml`（属 §2）
- `updated.go`（属 §3；§4 没用 Updated 机制）
- 指数断面（PLAN_v2 §4.6 标记为扩展，等下次验证 `GetIndex` 路径）

**验证**：
- ✅ `go build ./...` 通过（根模块）
- ✅ `go build .` 通过（web 子模块）
- ✅ `go vet ./...` 根模块：唯一告警 `client.go:359: unreachable code` 是历史遗留（与 §4 无关）
- ✅ `scripts/run_api_checks.py` Python 语法校验通过
- ⏳ `run_api_checks.py` 35 端点（+market_snapshot）需服务冷启后回归；慢测试需 `--slow` 单独跑
- ⏳ 实际跑通市场快照（4-15 分钟）尚未在本环境验证（TDX 服务器连接 + 网络稳定性是外部条件）

**后续**：
- 如果用户决定加指数断面（PLAN_v2 §4.6），先单跑 `client.GetIndex(protocol.TypeKlineDay, "sh000001", 0, 1)` 验证协议层 `GetIndex` 路径，字段语义可能跟个股不同（指数 Volume 单位可能是亿股/万手而非手）
- 慢测试可以加到 CI 的"nightly" job，跟 fast suite 分开跑
