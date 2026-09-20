# _UPDATE_FUNCTIONS.md — v2.0.0 新增能力盘点 与 Web API 未实现现状

> **读者**：tdx-api 下游项目的开发者
> **目的**：v2.0.0 完成上游同步后，Go 库层新增了大量数据能力，但目前 **全部只到库层、未暴露为 Web API**。本文盘点"有什么、入口在哪、有什么限制"，供下游结合自己的数据库设计讨论 Web API 应该如何规划。
> **约定**：本文档后半部分（§6）预留给下游开发者 append 自己的看法与补充。

---

## 0. 背景：v2.0.0 是什么

- v2.0.0 = **根模块全量对齐上游 [injoyai/tdx](https://github.com/injoyai/tdx) 2b3dcae（2026-09-05）+ 本地补丁重放 + web 层适配**，149 个文件、+16392/-1936 行。迁移细节见 `docs/migration-upstream-sync.md`。
- 对下游的**行为影响**：44 条既有 Web 路由语义全部保持不变（M4 阶段已做双容器逐字段对比），前端与下游无需改动。
- 对下游的**机会**：上游带来的新数据通道（扩展行情、板块包、财务快照等）现在都可以做 Web API 了——这是本文重点。

当前部署：`tdx-stock-web:v2.0.0` @ 8080，回滚手段 `VERSION=v1.4.0 docker compose up -d`。

---

## 1. 现状：Web API 已暴露的能力（44 条路由）

下游已可使用的、按数据类型分组：

| 类别 | 端点 |
|---|---|
| 实时行情 | `/api/quote`、`/api/batch-quote`、`/api/realtime/quote`(NDJSON 流式)、`/api/realtime/preheat`、`/api/realtime/codes`、`/api/realtime/health` |
| K 线 | `/api/kline`、`/api/kline-all`(ths/tdx)、`/api/kline-history`(ths/tdx/qfq)、`/api/kline-index-history`、`/api/index`、`/api/index/all` |
| 分时/逐笔 | `/api/minute`、`/api/trade`、`/api/minute-trade-all`、`/api/trade-history`、`/api/trade-history/full` |
| 股本/复权 | `/api/gbbq`、`/api/gbbq/refresh`(POST，按需拉取)、`/api/turnover` |
| 全市场 | `/api/market-snapshot`(慢，4-15min)、`/api/market-stats`、`/api/market-count` |
| 代码表 | `/api/codes`、`/api/stock-codes`、`/api/etf-codes`、`/api/etf`、`/api/search`、`/api/stock-info` |
| 日历/任务/运维 | `/api/workday`、`/api/workday/range`、`/api/tasks*`、`/api/health`、`/api/ready`、`/api/server-status` |
| 收益 | `/api/income` |

响应信封统一为 `{"code":0,"message":"success","data":...}`。完整说明见 `CLAUDE.md` 与 `docs/api-examples.md`。

**以下所有新能力都不在上述列表里。**

---

## 2. 新增能力盘点（库层已有、Web 未暴露）

### 2.1 港股 / 期货 / 外盘 — ExHq 扩展行情（全新协议通道）

独立于主行情的第二条 TCP 通道（端口 **7727**），覆盖 A 股之外的品种。

| 能力 | 入口 | 说明 |
|---|---|---|
| 市场列表 | `Client.ExMarkets()` | 枚举全部扩展市场（港股、期货、外盘等） |
| 品种列表 | `Client.ExInstruments(start, count)` / `ExCount()` | 全品种代码/名称 |
| 实时报价 | `Client.ExQuote(market, code)` | 含买卖盘 |
| 报价列表 | `Client.ExQuoteList(market, category, start, count)` | 批量 |
| **日 K** | `Client.ExBars(category, market, code, start, count)` | **港股日 K category=9、分钟=8；期货日 K=4**（与 A 股不同，已实测） |
| 分钟线 | `Client.ExMinute` / `ExHistMinute(market, code, date)` | 当日 / 历史分钟 |
| 逐笔 | `Client.ExTrade` / `ExHistTrade(market, code, date, start, count)` | 当日 / 历史逐笔 |
| 区间 K | `Client.ExBarsRange(market, code, date, date2)` | 按日期区间 |

**港股实测结论**（`exhq_live_test.go`）：market=31 香港交易所，代码 5 位数字（如 `00700` 腾讯），`ExBars(9, 31, "00700", 0, N)` 可取日 K。上游 `extend/pull/market/hk.go`、`hk_index.go` 已把港股/港股指数接入批量拉取引擎。

**连接方式**：`tdx.DialExHqDefault()`，可运行参考 `example/GetExHq/main.go`。

⚠️ **已知风险**：ExHq 服务器可用性一般。`hosts_exhq.go` 注释记载 2026-06 实测仅广州一台（116.205.143.214）完全可用，其余多超时；建议部署时启用 `SortExHosts` 按延迟重排，并把 ExHq 依赖的端点设计为可降级。

### 2.2 行业 / 概念板块 + 成分股（zhb.zip 一次拿全）

核心入口 `Client.GetZHBFiles()`（`client.go:573`）：下载通达信"盘后数据/板块"总包 **zhb.zip**（report file 协议 0x06B9），返回 `文件名 → 原始字节`。内含：

| 文件 | 内容 |
|---|---|
| `block_gn.dat` | **概念板块** |
| `block_hy.dat` | **行业板块**（部分服务器提供） |
| `block_zs.dat` | 指数板块 |
| `block_fg.dat` | 风格/地域板块 |
| `tdxzs.cfg` / `tdxzs3.cfg` | 板块名 ↔ 指数代码映射（tdxzs=880 系：行业T码/概念/风格/地域；tdxzs3=超集，另含 881 系申万行业 467 条） |
| `tdxdszs.cfg` | **港股**板块指数映射（HKxxxx） |
| `tdxbk.cfg` | 概念板块简称 ↔ 全称 |
| `tdxbjmore.cfg` | 北交所代码表（v2.0.0 起北交所代码走这里） |
| `incon.dat` / `hspy.dat` | 行业分类代码表 / 沪深拼音 |
| `tdxstat.cfg` / `tdxstat2.cfg` | 见 §2.7 / §2.8 |
| `xgsg.cfg` | 见 §2.9 |

板块解析与便利封装（均在 `client.go`）：

| 函数 | 返回 | 说明 |
|---|---|---|
| `GetBlockData(file)` | `[]*protocol.Block` | 板块 + 成分股列表 |
| **`GetBlockDataWithIndex(file)`** (`client.go:630`) | `[]*protocol.Block` | **推荐**：自动经 tdxzs.cfg + tdxbk.cfg 回填板块指数代码(id) |
| `GetTdxZs()` | `[]*protocol.TdxZs` | 板块名↔指数代码映射 |
| `GetTdxBk()` | `[]*protocol.TdxBk` | 概念板块简称↔全称 |

`protocol.Block` 结构（`protocol/model_block.go:149`）：

```go
type Block struct {
    Name  string
    Index string   // 板块指数代码(id), 如 880xxx
    Type  uint16
    Codes []string // 成分股，7 字符，首字符市场标志: 1=沪 0=深
}
```

可运行参考：`example/GetBlockData/main.go`、`example/GetBlockWithIndex/main.go`、`example/GetSpBlock/main.go`。

⚠️ zhb.zip 属**盘后数据**（通达信收盘后更新），不适合当日盘中做成分变动感知。

### 2.3 个股行业归属（正反两个方向）

- **板块 → 成分股**：§2.2 的 `Block.Codes`，一次下载全量板块的全量成分。
- **个股 → 板块**：`GetTdxStat2()` 的 `BlockIndex` 字段给出每只股票所属/领涨板块指数代码（见 §2.8）。
- **个股 → 行业归属**：`Client.GetTdxHy()`（`client.go:703`）解析 `tdxhy.cfg`，返回每只股票的**通达信行业码（T 前缀）+ 申万行业码（X 前缀）**：

```go
type TdxHy struct {
    Market uint8  // 0=深 1=沪
    Code   string
    TdxHy  string // 通达信新行业代码（T 前缀）
    SwHy   string // 申万行业代码（X 前缀）
}
```

可运行参考：`example/GetTdxHy/main.go`（内含 600519/000001/601398 样本输出）。需要申万口径分类时这个最直接。

### 2.4 财务快照

`Client.GetFinanceInfo(exchange, code)`（`client.go:495`）→ `protocol.FinanceInfo`，约 38 个字段（`protocol/model_finance.go:13`）：

> 流通股本 / 总股本 / 国家股 / 法人股 / B股 / H股 / 总资产 / 流动资产 / 固定资产 / 无形资产 / **股东户数** / 流动负债 / 长期负债 / 资本公积金 / 净资产 / 主营收入 / 主营利润 / 应收账款 / 营业利润 / 投资收益 / 经营现金流 / 总现金流 / 存货 / 利润总额 / 税后利润 / 净利润 / 未分配利润 / IPO 日期 / 更新日期 / 地域码 / 行业码 …

注意这是 TDX 口径的**最新快照**（无历史期报序列），与 tushare 等正式财报数据互补。可运行参考：`example/GetFinanceInfo/main.go`。

### 2.5 F10 公司资料

| 函数 | 说明 |
|---|---|
| `Client.GetCompanyCategory(exchange, code)` (`client.go:477`) | 列出该公司可读的资料文件（公司概况等分类目录） |
| `Client.GetCompanyContent(exchange, code, filename, start, length)` (`client.go:486`) | 按文件名分段读取文本内容 |

适合做"个股详情页"的公司资料展示。参考：`example/DumpReportFile/main.go`。

### 2.6 集合竞价逐笔

`Client.GetCallAuction(code)`（`client.go:429`）→ `protocol.CallAuctionResp`：

```go
type CallAuction struct {
    Time      time.Time
    Price     Price
    Match     int64 // 匹配量
    Unmatched int64 // 未匹配量(绝对值)
    Flag      int8  // 1=买盘排队, -1=卖盘排队
}
```

仅 09:15-09:25 竞价窗口内有意义，适合竞价异动监控。参考：`example/GetCallauction/main.go`。

### 2.7 盘后统计（字段已实测核验）

`Client.GetTdxStat()`（`client.go:646`）解析 `tdxstat.cfg` → `[]*protocol.TdxStat`，全市场一次拿全，35 字段。上游已用日线/东财数据交叉核验过精度的字段（`protocol/model_stat.go:25`）：

| 字段 | 精度结论 |
|---|---|
| `PETTM` 市盈率(TTM) / `PEStatic` 静态市盈率 | 近乎精确（CV≈0.012） |
| `DivYield` 股息率% | 通达信口径，F10 印证精确 |
| `TrendDays` 连涨连跌天数(正涨负跌) | 同源日线精确 |
| `ChangePct` 涨跌幅% | 精确 |
| `Chg5` / `Chg10` / `Chg20` / `Chg60` / `ChgYTD` | 5/10/60 日 MAE≈0；20 日 MAE0.23；YTD 基准上年末 |

未命名字段保留在 `Fields []string` 自取。

### 2.8 资金流向 + 板块归属

`Client.GetTdxStat2()`（`client.go:660`）解析 `tdxstat2.cfg` → `[]*protocol.TdxStat2`，21 字段。已核验：今日/昨日成交额(万元，精确)、`IPOPrice` 发行价(10/10 全中)、`High52W`/`Low52W` 52周高低、**`BlockIndex` 所属/领涨板块指数代码**（个股→板块反向查询的数据源）。注意：zhb.zip **不含融资融券**数据（上游已对东财两融全字段比对确认）。

§2.7/§2.8 两者都是**全市场一次拉全**（单文件解析，无逐股请求），配合 `/api/market-snapshot` 的并发模式很容易做成全市场扫描端点。参考：`example/DumpReportFile/main.go`。

### 2.9 新股申购

`Client.GetXgsg()`（`client.go:673`）解析 `xgsg.cfg` → `[]*protocol.TdxXgsg`：申购代码 / 申购日期 / 发行价 / 证券名称（18 个原始字段全保留在 `Fields`）。

### 2.10 新浪复权因子（第三方交叉验证源）

`extend/spider-sina.go`：

- `GetSinaFactor(code, type)` / `GetSinaFactorFull(code)` → `Factors`

与既有同花顺（THS）抓取、本地 gbbq 仿射计算构成**三个独立复权数据源**，可交叉验证。注意上游已改仿射复权模型（`Factor.QFQMul/QFQAdd/HFQAdd/HFQMul`，复权价 = `round_half_up(QFQMul*raw + QFQAdd)`，见 `protocol/model_gbbq.go:293`）。

### 2.11 通达信本地文件读写

`extend/local.go`：`ReadDay` / `ReadMinute1` / `ReadMinute5` 直接解析通达信客户端 `vipdoc` 目录的 `.day/.lc1/.lc5` 二进制文件；`WriteDay/WriteMinute1/WriteMinute5` 反向编码。适合：离线导入用户本机通达信数据、或把库内数据导出为通达信格式。

### 2.12 extend/pull 批量拉取引擎（新架构）

`extend/pull/`（~20 文件，带测试）：可插拔市场 Unit + per-code sqlite 存储 + 交易日历覆盖 + 增量合并。

- 内置市场适配器：`a_stock`(A股日/分钟)、`hk`(港股)、`hk_index`(港股指数)、`future`(期货)、`block`(板块)、`us`(美股)、`minute`、`fallback`
- 港股等非 A 股市场的日 K 全历史落库可以直接用它，不必自写批量循环

### 2.13 上游自带 HTTP server 模块

`extend/httpserver/`（含 `handler_exhq.go`）：上游对库能力的 HTTP 封装，含 ExHq handler。tdx-api 未采用它（我们有自己的 web 层与信封规范），但可作 API 形态参考。

---

## 3. 数据通道总览

| 通道 | 端口/来源 | 延迟特征 | 覆盖 |
|---|---|---|---|
| 主行情协议（既有） | TDX 7709 | 实时 | A股/指数 行情、K线、分笔、gbbq |
| **ExHq 扩展行情（新）** | TDX 7727 | 实时 | 港股、期货、外盘 |
| **zhb.zip 盘后包（新）** | report file 0x06B9 | 盘后 | 板块/成分/统计/财务归属/新股 |
| THS / 新浪 HTTP（既有+新增） | 外网 HTTP | 日频 | 前复权因子、交叉验证 |
| 通达信本地文件（新） | 本地 vipdoc | 离线 | day/minute K |

---

## 4. 已知限制与风险（下游设计时需要纳入）

1. **ExHq 服务器可用性**（§2.1）：单点风险，端点必须可降级/可缓存；建议首次接入先做独立探活验证。
2. **zhb.zip 为盘后数据**（§2.2）：板块成分与统计指标都是 T+0 盘后口径，不宜用于盘中成分变动判断；文件体积不小，适合服务端缓存后复用。
3. **财务快照只有最新值**（§2.4）：无历史期报序列，历史财务仍需下游用 tushare 等源补。
4. **TDX 口径与官方口径可能不一致**：如 `DivYield` 股息率是通达信口径（§2.7）；核验结论都写在协议模型注释里，下游用数前建议先读对应 `protocol/model_*.go` 的注释。
5. **既有行为约束不变**：gbbq 仍为按需拉取（`POST /api/gbbq/refresh`）；`/api/market-snapshot` 仍为长耗时同步端点。
6. **北交所口径变化**：`IsBJStock` 收窄为 92 开头（43/8 不再算 BJ），下游若有按前缀过滤北交所的逻辑需确认。

---

## 5. 候选 API 方向（初步，供下游讨论，非定案）

按预估价值排序：

| 优先级 | 候选端点 | 依赖能力 | 备注 |
|---|---|---|---|
| 高 | `GET /api/blocks`（行业+概念板块及成分股） | §2.2/§2.3 | zhb.zip 一次下载全量，性价比最高；需考虑服务端缓存策略 |
| 高 | `GET /api/finance-info`、`GET /api/tdx-stat` | §2.4/§2.7 | 字段已核验，适合做全市场估值/动量扫描 |
| 中 | `GET /api/hk/kline`（及港股 quote/minute） | §2.1 | 先解决 ExHq 探活与降级 |
| 中 | `GET /api/call-auction` | §2.6 | 仅竞价窗口有效，实时性要求高 |
| 低 | `GET /api/xgsg`（新股申购） | §2.9 | 低频，实现简单 |
| 低 | 复权因子交叉验证工具 | §2.10 | 内部质检用途 |

设计时建议一并考虑：盘后包类数据的**服务端缓存/定时刷新**（现有 TaskManager 可复用）、全市场批量端点的**并发与限流**（参考 market-snapshot 的 4 路并发模式）、以及响应信封继续沿用 `{code,message,data}`。

---

## 6. 下游开发者补充区

> （预留：请在此 append 你对数据需求、数据库表设计与 Web API 形态的看法。建议格式：需求描述 → 涉及 § 编号 → 期望的端点/字段/频率 → 对存储设计的想法。）

---

### 6.1 下游现状速览（stock_scrapper，2026-09-20）

- **角色分工**：tdx-api 只出 HTTP（不写库）；下游负责全部 ETL——拉取 → 落库 MySQL（`tdx_kline` 库）→ 算前复权/技术指标。
- **现有交互面**：`clients/tdx_api_client.py` 封装 9 个端点（`/api/ready`、`/api/stock-codes`、`/api/gbbq(+refresh)`、`/api/market-snapshot`、`/api/kline-history-tdx`、`/api/kline-all/tdx`、`/api/index/all`、`/api/kline-index-history`、`/api/workday`），requests session + 指数退避重试（429 例外）+ 并发 20，统一解 `{code,message,data}` 信封。
- **现有表**（9 张，均不动结构）：主链路 `d_rawdata_tdx`(厘) → `d_qfq_tdx`(元) → `d_indicator_tdx`；辅助 `d_equity_history`、`d_xrxd_history`；板块 `d_plate_base`(404 行) + `d_plate_members`(4.6 万行)；另有独立通道 hysz 两张表。
- **现状痛点**：板块数据目前**不走 tdx-api**，而是两条临时路线——① `mac_collect_sectors.py`（macOS 截屏 + OCR 通达信客户端，写 `d_plate_base`）；② `fetch_stocks.py`（直连 `page.tdx.com.cn:7615/TQLEX` 网页接口逐板块抓成分，写 `d_plate_members`，industry 命中率仅 39/127=30.7%）。都是协议外抓取，脆弱、覆盖不全，**这次整体退役，切到 §2.2 的 zhb.zip**。

### 6.2 数据需求与优先级（已决策）

| 优先级 | 需求 | 涉及 | 频率 | 说明 |
|---|---|---|---|---|
| P0 | 行业+概念板块全量 + 成分股 + 板块指数码映射 | §2.2/§2.3 | 每交易日盘后 1 次 | 替代上述两条临时路线，industry 覆盖 30.7% → 100% |
| P0 | 全市场盘后统计（PETTM/静态PE/股息率/连涨天数/5·10·20·60日涨幅/YTD） | §2.7 | 每交易日盘后 1 次 | 下游目前**完全没有估值维度**，选股刚需 |
| P0 | stat2（今日/昨日成交额、IPO价、52周高低、BlockIndex 领涨板块） | §2.8 | 每交易日盘后 1 次 | 与 stat 同源同频，合并落库 |
| P0 | 个股行业归属（通达信码 + 申万码） | §2.3 | 每交易日盘后 1 次 | 下游无行业分类；申万码直接满足既有分析需求 |
| 观望 | 财务快照（§2.4，38 字段仅最新值） | — | — | 无历史期报序列，历史财务我们走 tushare；等有最新值消费场景再接 |
| 观望 | ExHq 港股/期货（§2.1） | — | — | 上游自述服务器单点风险（仅广州一台完全可用），且下游暂无港期需求；未来接入前要求先有探活端点 + 可降级设计 |
| 暂缓 | 竞价（§2.6）/ 新股（§2.9）/ F10（§2.5）/ 新浪因子（§2.10） | — | — | 实时性要求高、低频、文本展示、质检用途，均非当前痛点 |

**关键认知**：`tdxstat.cfg` / `tdxstat2.cfg` / `tdxhy.cfg` / zhb.zip 都是"只有当前状态、没有历史"的文件。**历史只能由下游每日拉取自行积累**——所以接入时间越早，历史资产越早开始沉淀。这也是 P0 尽快实装的主要理由。

### 6.3 期望的 Web API 形态（对上游的具体建议）

四个端点，全部走既有信封 `{code,message,data}`、既有 TaskManager/缓存基建：

| 端点 | 数据源 | 建议行为 |
|---|---|---|
| `GET /api/blocks` | zhb.zip（§2.2） | 首次请求下载 + **服务端缓存复用**（文件不小，且属盘后数据，盘中重复下载无意义）；`POST /api/blocks/refresh` 强制重下（与 `/api/gbbq/refresh` 同模式对齐）。一次性返回全量板块数组 `{name, index, type, codes[]}`，下游不需要按板块分页 |
| `GET /api/tdx-stat` | tdxstat.cfg（§2.7） | 全市场单文件解析（无逐股请求），一次性返回数组；建议响应中**附带 `fields` 字段名名单**（含未核验的 `Fields []string`），方便下游对齐字段 |
| `GET /api/tdx-stat2` | tdxstat2.cfg（§2.8） | 同上 |
| `GET /api/tdx-hy` | tdxhy.cfg（§2.3） | 同上，`{market, code, tdx_hy, sw_hy}` 数组 |

通用意见：

1. **均为"文件当前快照"语义**，无历史可查，端点无需 `?date=` 参数；体积预估 stat 约 2-5MB（5300 股 × 35 字段），可一次性返回，若实测内存峰值高可参考 `/api/realtime/quote` 的 NDJSON 流式先例。
2. **列表字段请统一小写 `list`**。既有部分端点返回大写 `List`，下游消费侧被迫写 `data.get("List", data.get("list", []))` fallback 链（`run.py:508` 等 6 处），新端点别再继承这个坑。
3. 超时预期：这些都是本地文件解析，秒级即可，不需要 snapshot 级的长超时档位。
4. API 文档建议给 tdxstat 各字段标注**"已核验/未核验"**状态（§2.7 的核验结论很有价值，希望延续到 API 层文档）。

### 6.4 下游存储设计

沿用项目惯例：**谁写表谁放 DDL（模块内 `CREATE TABLE IF NOT EXISTS` 幂等建表）+ `(code,date)` 唯一键 + UPSERT**。三处改动，现有 9 张表结构零改动：

**① 新表 `d_stat_tdx`**（§2.7 + §2.8 合并落库，一行 = 一只股票一天的盘后全貌，避免查询 join）

```sql
CREATE TABLE d_stat_tdx (
  code        varchar(10) NOT NULL,
  date        date        NOT NULL,
  -- 来自 tdxstat.cfg (§2.7, 已核验字段)
  pettm       decimal(10,2),   -- 市盈率 TTM
  pe_static   decimal(10,2),   -- 静态市盈率
  div_yield   decimal(8,4),    -- 股息率%（通达信口径）
  trend_days  int,             -- 连涨连跌天数（正涨负跌）
  change_pct  decimal(8,4),    -- 当日涨跌幅%
  chg5        decimal(8,4),
  chg10       decimal(8,4),
  chg20       decimal(8,4),
  chg60       decimal(8,4),
  chg_ytd     decimal(8,4),    -- 基准上年末
  -- 来自 tdxstat2.cfg (§2.8)
  amount_today decimal(20,2),  -- 今日成交额（万元）
  amount_prev  decimal(20,2),  -- 昨日成交额（万元）
  ipo_price    decimal(10,3),  -- 发行价
  high_52w     decimal(12,3),
  low_52w      decimal(12,3),
  block_index  varchar(10),    -- 所属/领涨板块指数码（880xxx）
  PRIMARY KEY (code, date),
  KEY idx_date (date)
);
```

stat 与 stat2 两端点各自 UPSERT 各自字段组（同键不同列，互不覆盖），单端点失败不影响另一组。第一版只建已核验字段的列；上游若把 `fields` 名单随响应给出，未来发现某未命名字段有价值可低成本 `ALTER ADD`（代价：该字段从补列日起才有历史）。

**② 新表 `d_stock_industry`**（§2.3，SCD-lite：只在归属变化时插入新行，不按日重复堆 5300 行）

```sql
CREATE TABLE d_stock_industry (
  code     varchar(10) NOT NULL,
  eff_date date        NOT NULL,   -- 该归属自何日起生效
  tdx_hy   varchar(20) NOT NULL,   -- 通达信行业码（T 前缀）
  sw_hy    varchar(20) NOT NULL,   -- 申万行业码（X 前缀）
  PRIMARY KEY (code, eff_date)
);
```

每日拉全量与库内每股最新行 diff，变化才插（预计全年几百行）；查询某日快照 = 每股取 `eff_date <= d` 的最新行。

**③ `d_plate_base` / `d_plate_members`**：结构基本不动，**数据源整体切换到 `/api/blocks`**。
- `d_plate_base` 增加板块指数码（880xxx，来自 `GetBlockDataWithIndex` 的 `Block.Index`），用于既有 [03] 阶段的板块指数 K 线拉取（现走 `/api/kline-index-history` 的 396 个 board code 全部改由 zhb.zip 权威映射供给）。
- ⚠️ **字段损失声明**：`d_plate_members.gld`（关联度 5/4/3）来自 page.tdx.com.cn 网页接口，而 `protocol.Block` 只有 `Name/Index/Type/Codes`，**没有关联度**。切换后 gld 失去来源，将置 0（或废弃该列）。若 block_gn.dat 原始数据里其实有关联度信息，希望上游解析时保留——这是切换前唯一想向上游确认的点。

### 6.5 下游 ETL 集成

- `run_quick.py`（每日 16:30 cron）新增 **[09] stat+blocks 阶段**：`/api/blocks` → 重建 `d_plate_base/members` → `/api/tdx-stat` + `/api/tdx-stat2` → upsert `d_stat_tdx` → `/api/tdx-hy` → diff 插入 `d_stock_industry`。四个端点全市场各一次，预计秒级完成，对 cron 窗口无压力；zhb.zip 属盘后数据，16:30 拉取时序安全。
- 失败处理延续既有 checkpoint 惯例（`.etl_checkpoint.json` 失败名单次日重试），单端点失败不阻塞其他端点。
- `run.py --mode init`：新表无历史可回填（上游只有当前快照），init 仅建表 + 拉当日，从上线日起积累。

### 6.6 下游废弃清单（切换完成后退役）

- `hysz_fetch/mac_collect_sectors.py` — macOS 截屏 + OCR 采板块列表（与 znz_hack 替代 mac_collect_hysz.py 同一升级模式：协议通道替代 OCR）
- `hysz_fetch/fetch_stocks.py` — page.tdx.com.cn:7615 网页接口抓成分股
- `hysz_fetch/survey_industry_coverage.py` + `INDUSTRY_HIT_NAMES` 白名单 — industry 命中率摸底 hack，zhb.zip 全量覆盖后失去存在意义
- `page.tdx.com.cn` 外部依赖全部退出（消除协议外抓取的不稳定性）

### 6.7 其他意见与风险确认

1. **北交所口径变化（§4.6）**：`IsBJStock` 收窄为 92 开头，我们消费 `/api/stock-codes` 后按前缀过滤的逻辑会核查一遍（库内已有 920083 这类北交所股票）。
2. **TDX 口径标注**：`DivYield` 等通达信口径字段落库时会在表/文档注释里标注口径来源，与未来 tushare 官方口径字段区分。
3. 对 §5 候选优先级的反馈：同意 `/api/blocks`、`/api/tdx-stat` 排高优先；我们补充希望 `/api/tdx-stat2`、`/api/tdx-hy` 与它们**同一批实装**（同一文件族、同一解析基建，拆批反而增加联调次数）。


---

## 7. 上游回应（2026-09-20, v2.1.0 实装）

### 7.1 §6.4③ gld（关联度）确认结论：不存在

`block_gn.dat` 原始 757083 字节，`(大小-386) % 2813 = 0`，单记录 2813 字节 = 名称(9) + 成分数(2) + 类型(2) + 成分区(400×7)，成分区未用部分全为 0x00 填充（hexdump 验证）。**协议文件中没有关联度的容身之处**，网页接口的 gld 无法从 zhb.zip 获得。按贵方预案处理：置 0 或废弃该列。

### 7.2 行业板块的重要发现与解决：block_hy.dat 已死，服务端合成

实测 6 台不同地域服务器（上海华为/武汉电信/广州×2/北京×2）**全部不提供 `block_hy.dat`**，上游"部分服务器提供"的注释已过时。但行业板块已通过另一条路完整实现：

- `tdxzs.cfg` 中 **Ref 字段为 T 码** 的条目即行业板块（如 `煤炭|880301|ref=T0101`、`白酒|880381|ref=T030502`）
- `tdxhy.cfg` 个股 T 码为层级编码（如 600519=T030501），与板块 Ref **前缀匹配**即成员（三级行业自动归入一/二级）
- 抽检：银行(880471) 42 只含 000001 ✓；白酒(880381) 19 只含 600519 ✓；共 146 个行业板块，成分覆盖率 **100%**（对比贵方现状 30.7%）

对贵方的影响：`d_plate_members` 的 industry 覆盖从 39/127 → 全量；`gld` 之外无字段损失。

### 7.3 四端点已实装（v2.1.0）

§6.3 建议的四个端点 + refresh 已全部实装并部署，按贵方要求：小写 `list`、无 `?date=` 参数（快照语义）、`tdx-stat`/`tdx-stat2` 响应附 `field_names` 槽位说明、字段核验状态已写入 `API_接口文档.md`。补充说明：

- **缓存语义**：采纳"GET 自动过期刷新"方案——数据日期早于最近已收盘交易日（16:00 就绪线）时，GET 自动重下（mutex 防击穿）；`POST /api/blocks/refresh` 仍保留强制刷新。贵方 16:30 ETL **直接 GET 即可**，无需先 POST。
- **实测性能**：首刷约 2 秒（好于预估的 6-8s），缓存命中 58ms；响应实测体积 blocks 1.2MB / stat 3.5MB / stat2 2.6MB（低于预估，未启用 NDJSON 流式）。
- **行数差异**：`tdx-stat`/`tdx-stat2` 8055 行（含北交所及更多证券），`tdx-hy` 5663 行（仅沪深）。落库时按各自实际行数处理，勿假设一致。
- `?type=` 取值 `gn,hy,fg,zs,sp`：除贵方 P0 的行业+概念外，一并提供了风格地域(fg)/指数板块(zs)/专业板块(sp，中证500=500 只成分已验证)，未来扩展零改动。
- 成分码统一 6 位（`sp` 的 7 位市场前缀格式已在服务端归一）。

### 7.4 主机测速结论（顺带）

2026-09-20 全量 TCP 实测 41/41 台主行情服务器可用：**北京腾讯云 8-13ms 最优**（本机视角），武汉 24.7ms、上海 29-34ms、广州 41-50ms。已按此调整 hosts 表默认顺序，并在 web 启动时启用 SortHosts 动态测速。ExHq 16/16 台 TCP 全通（协议层可用性未验证，待未来接入时按贵方要求先做探活端点）。

---

## 8. 开发进展（2026-09-20 收工状态）

- **v2.1.0 已开发完成并部署生产 8080**（commit `25fb82f`，tag `v2.1.0`，容器 healthy；回滚 `VERSION=v2.0.0 docker compose up -d`）。
- §7 所述五个端点全部上线，生产环境双轮验证通过（本地 8081 + 生产 8080，11/11 端点回归）；数据抽检一致（000001 pettm=5.22 / block_index=880845，600519 hy=T030501，沪深300=300 只，中证500=500 只）。
- 实现落点：`web/server_zhb.go`（缓存与刷新编排）+ `web/server_api_extended.go`（5 个 handler）；字段核验状态已写入 `API_接口文档.md` "盘后数据接口"节，使用示例在 `docs/api-examples.md`。
- **待协商**：`gld`（关联度）字段缺失的处理方式，待与下游确定后补充到 §7.1（候选：d_plate_members.gld 置 0 / 废弃该列）。
- 小尾巴：`scripts/run_api_checks.py` 已加 7 个新端点用例，但宿主机缺 `requests`（无 pip），本次用标准库脚本验证；下次跑全量脚本前需 `apt install python3-requests` 或改造脚本为标准库实现。

---

## 9. 上游回应二：880/881 系专题（2026-09-20，全部实测验证）

先说结论：**880 系与 881 系是两套平行的行业分类体系，不是同一体系的两个层级**。你们存量"880=概念、881=行业"的理解基本正确；我方文档此前"881xxx=地域"的说法是**错的**（源自上游注释，已实测证伪并修正），地域实际在 880 系。逐条回答：

### 9.1 两套体系的关系（实测数据）

| | 880 系 | 881 系 |
|---|---|---|
| 配置来源 | `tdxzs.cfg`（604 条） | **仅存在于 `tdxzs3.cfg`**（该文件 1071 条 = tdxzs 全部 604 + 881 系 467） |
| 分类标准 | **通达信行业**（ref=T 码，如 煤炭=T0101、煤炭开采=T010101） | **申万行业**（ref=X 码，如 煤炭=X10、煤炭开采=X1001、动力煤=X100101） |
| 类型码 | type=2 行业 146 / type=3 地域 32 / type=4 概念 269 / type=5 风格 158 | type=12，共 467 个：一级 30 / 二级 128 / 三级 309 |
| 与 tdxhy.cfg 的对应 | TdxHy.TdxHy（T 码） | TdxHy.SwHy（X 码） |
| 板块成员 | /api/blocks type=hy 已合成（146 个，覆盖 100%） | 可用同法合成（见 9.5） |

- **880301 煤炭（通达信，T0101）与 881002 煤炭开采（申万二级，X1001）没有层级关系**，是两套体系里的平行条目，同名不同码属正常。层级关系只在各自体系内部存在（880301 煤炭 ⊃ 880302 煤炭开采；881001 煤炭 ⊃ 881002 煤炭开采 ⊃ 881003 动力煤/881004 炼焦煤）。
- "tdxzs.cfg vs tdxzs3.cfg 各对应哪套"：**tdxzs = 880 系全集；tdxzs3 = 880 系全集 + 881 系申万**。
- "881xxx=地域"过时且错误：地域是 880 系（黑龙江=880201 等 32 个，ref=block_fg 序号）；你们已拉的 881 系 = 申万行业。
- **主线建议：以 881 系（申万）为主线**。理由：业界标准、三级全覆盖（你们存量 128 个正好=申万二级）、与 `/api/tdx-hy` 的 `sw_hy` 直接对齐、成分可从 tdxhy.cfg 100% 合成。880 系行业并存即可（通达信自家口径，概念板块本来就在 880 段）。

### 9.2 881 系持续可得性

配置源稳定：tdxzs3.cfg 随 zhb.zip 每日下发（实测 2026-09-20 在包内）。行情源与 880 系同一条路（`GetIndexDay` 协议），**881 系全量 3818 根（2011-01-04 起）实测可拉**。结论：可长期拉取，无需迁移放弃；建议维持"881 为主线、880 并存"，无需二选一。

### 9.3 历史回填

| 体系 | 最早数据 | 实测根数（至 2026-09-18） |
|---|---|---|
| 880 系（煤炭/白酒/稀缺资源 抽 3 只） | **2005-06-07** | 各 5177 根 |
| 881 系（一级/二级/三级 抽 3 只） | **2011-01-04** | 各 3818 根 |

**`/api/kline-index-history` 无 count/limit 参数**：内部走 `GetIndexDayAll`（底层 800 条/批自动拼接拿全量）后按 `start_date`/`end_date` 过滤，**一次请求返回整个区间，无需分页**。全历史约 6000 根 × ~100B ≈ 600KB/只，回填时每板块一次请求即可。注意两点：16:00 前当日无数据；板块指数不复权（指数本身无复权概念，直接落库）。

### 9.4 新板块注入的权威清单源

**建议以 `/api/blocks` 为权威清单源**（每日盘后快照，直接来自 tdxzs*.cfg + block 文件）。zhb 清单变更频率低（板块新增/改名随通达信版本，月级尺度），**880xxx/881xxx 码是固定映射表不轮换**——变更形态是"新增板块"或"改名"，码本身稳定。建议 [03] 改造为：每日 ETL 拉 `/api/blocks` → 与库内清单 diff → 新板块自动进拉取清单补历史（消失的板块标记停更）。这与贵方 §6.4③ 的方向一致。

### 9.5 fg/zs/sp 的价值与建议

| 类型 | 实测内容 | 板块指数 K 线 | 建议 |
|---|---|---|---|
| `fg`（161） | 风格 158（880081 轮动趋势、880735 专精特新 等）+ 地域 32，**全是 880 系**（不是 881！） | 有（880735 实测最新 2026-09-18 可拉） | 可选：做风格/地域轮动观察有价值，优先级中低 |
| `zs`（117） | 指数板块（沪深300=300 只成分实测），成员上限 400 | 板块指数有 | **跳过**：与交易所官方指数成分重复，官方指数（sh000300）本身可拉 K |
| `sp`（35） | 中证500/1000/2000/A500 等大型指数成分，**无 400 上限**（这是它存在的唯一理由） | 板块本身无指数码，但对应官方指数（000852.SH 等）有 | **推荐接入**：规模指数成分是量化刚需，中证2000 级别成分别处不好拿 |

另附申万板块合成验证（881 系 × tdxhy.SwHy 前缀匹配）：467 板块、成分命中 5582/5663（98.4%，未命中为个别空 SwHy）、空板块 0；抽检 白酒(881135)19 只含 600519 ✓、银行(881385)42 只含 000001 ✓。**如果贵方确认需要，上游可在 `/api/blocks` 增加 `sw` 类型（与 hy 同结构，index=881xxx），当天可实装**——请回复确认 API 形态（新增 type=sw vs hy 带 std 字段二选一）。

### 9.6 文档勘误

上游注释与文档中"881xxx=地域"/"fg 板块指数为 881xxx"的说法与实测不符，已修正：`protocol/model_block.go`（tdxzs/Code 注释）、`example/GetBlockData/main.go`、本文件 §2.2、`docs/api-examples.md` blocks 表格。`API_接口文档.md` 中"`sh881xxx` 行业板块 K 线"的说法是对的（881 确为行业），未动。

---

## 10. 下游回应二（2026-09-20）

1. **确认 `type=sw`**：与既有 source 体系（gn/hy/fg/zs/sp）一致，下游 `/api/blocks` 调用直接加 `sw`，`d_block_tdx` 按 source 天然区分 880/881 两套。请按此实装。
2. 采纳 §9.1 主线建议：**881 申万为主线、880 并存**。下游 K 线侧 881 申万沿用既有 `category='industry'`，880 系行业将以新 `category='industry_tdx'` 落库区分。
3. 采纳 §9.4：下游 [03] 板块 K 线清单源改造为 `/api/blocks`（经下游 `d_block_tdx`）权威清单 + diff 注入 + 消失板块停更，替换现状的自举清单。
4. sp 采纳接入（成分入 `d_block_members`）；zs 跳过、fg 暂缓（与 §9.5 建议一致）。
5. 待 `sw` 实装后，下游回填计划：880 系 hy 145 只（2005 起）+ 881 申万新增约 339 只（2011 起），复用 `/api/kline-index-history` 无分页特性，每只一次请求。

---

## 11. sw 已实装（2026-09-20，回应 §10.1）

按贵方确认的 `type=sw` 形态实装完毕：

- **`GET /api/blocks?type=sw`**（或在缺省全量/任意组合中出现）：申万行业板块 467 个（一级 30 / 二级 128 / 三级 309），`index=881xxx`、`type=12`、`source=sw`、`codes` 统一 6 位，与既有结构完全一致。
- 合成方式：`tdxzs3.cfg`（881xxx 且 Ref=X 码）× `tdxhy.cfg` 个股 `SwHy` 层级前缀匹配。抽检：白酒(881135) 19 只含 600519 ✓、银行(881385) 42 只含 000001 ✓、煤炭 881001(32) ⊃ 煤炭开采 881002(25) ⊃ 动力煤 881003(15) 层级结构正确 ✓；空板块 0，成分总条目 15932。
- `POST /api/blocks/refresh` 的 `counts` 新增 `sw` 字段。全量 blocks 现为 1195 个（gn 269 / hy 146 / fg 161 / zs 117 / sp 35 / sw 467）。
- 贵方回填计划（§10.5）可执行：881 系指数 K 线 2011-01-04 起（3818 根），`/api/kline-index-history?code=sh881xxx` 一次拉全。

---

## 12. 下游反馈三：rawdata 新股坏点 + ETL 触发时间确认（2026-09-20）

（sw/sp 接入已完成并 E2E 验收：`d_block_tdx` 917 板块 / `d_block_members` 106,400 成分；881 申万 339 个新码全历史回填 127 万行，881003 动力煤精确 3818 根起 2011-01-04；二轮幂等 records=0。§10 全部事项闭环，仅剩以下两点：）

### 12.1 rawdata 新股平线坏点（数据质量，请上游排查 kline 链路）

交叉验证中顺带发现，与盘后端点无关，是 `/api/kline*` 日 K 链路的问题：

- **现象**：部分新股存在 OHLC 四值全等的坏点 bar。例：301717 在 2026-07-31（上市次日附近）`open=high=low=close=65.99`，而该股现价 350+ 元，65.99 明显非真实成交价。
- **覆盖面**：52 周高低交叉验证的"覆盖不足子集"（新股/rawdata 缺口）中 28% 的 low 超差均归因此类坏点（301717 / 301583 / 301707 / 301689 / 301655 等）。
- **影响**：任何历史极值类计算（52 周高低、区间振幅）被污染。
- 请上游排查 kline 对新股（尤其上市初期）的数据源。下游侧我们可以加"OHLC 全等且与前收比跳变离谱"的清洗规则兜底，但希望从源头修。

### 12.2 ETL 触发时间窗口确认（咨询，下游 cron 暂不动）

- **下游 crontab 现状**：交易日 16:15 跑 run_quick.py（历史固定，本次未改）。
- **上游已知信息**：上游侧 15:45 开始更新数据；§7.3 写的缓存语义是"数据日期早于最近已收盘交易日（**16:00 就绪线**）时 GET 自动重下"。
- 想确认三点：
  1. zhb.zip / tdxstat.cfg 等数据源 15:45 开始更新，一般多久完成？**16:15 拉取有没有可能碰到半更新状态**（部分文件新部分旧）？
  2. "16:00 就绪线"的确切语义：16:00 之后 GET 就能稳定拿到**当日完整**数据？
  3. 若上游认为 16:15 偏早，建议的 ETL 触发时间是几点（16:30 / 17:00）？
- 下游 cron 保持 16:15 不动，等上游结论后再定是否调整。

---

## 13. 上游回应三（2026-09-20）

### 13.1 新股平线坏点：定性为 TDX 占位 bar，非链路 bug，当前源数据已自愈

实测过程（2026-09-20）：

1. **当前 TDX 原始链已无坏点**：对 301717 / 301583 / 301707 三只全量拉 `GetKlineDayAll`（`/api/kline-all/tdx` 同源），**OHLC 全等 bar = 0 根**。301717 当前首根为 2026-08-11（真实上市日）O=450/H=554.73/L=420/C=503，数据正常。
2. **坏点价 = 发行价精确一致**：65.99 与 `/api/tdx-stat2` 的 `ipo_price` 完全吻合；其余 301583(22.6) / 301707(23.45) / 301689(16) / 301655(8.5) 同样全对上各自发行价。
3. **坏点日期早于上市日**：301717 的坏点在 2026-07-31，早于上市日 2026-08-11 整 11 天（即申购/中签公布期）。

**结论**：TDX 服务器在新股申购~上市日期间会写入一根 OHLC=发行价 的占位 bar，上市后服务器侧修正消失。tdx-api 全链路纯中转（rawdata 走 kline-history-tdx/kline-all-tdx 原始数据），无复权无加工，不存在链路 bug。贵方落库时点恰在占位期，坏点随 ETL 固化进了 rawdata。

**清洗规则建议**（确定性判据，覆盖全部 5 只）：

```
坏点 := (open == high == low == close) AND (close == ipo_price)
其中 ipo_price 来自 /api/tdx-stat2 的 ipo_price 字段
```

若想更保守可加 `AND date < 上市日`（上市日可用"首个非坏点 bar 日期"近似）。上游不改既有 kline 端点语义（v2.0.0 起 44 条路由行为承诺不变，且其他消费方可能依赖原始值）。

### 13.2 ETL 触发时间：三点回答

1. **不存在"部分文件新部分旧"**：四端点数据同源 `zhb.zip` 单文件，上游一次下载整体入缓存快照，四个端点永远读同一份快照，**内部一致性由构造保证**。唯一理论风险是服务器正重写 zhb.zip 的瞬间拉到中间态，这在客户端无法检测——兜底见第 3 点的 stat_date 校验。
2. **"16:00 就绪线"的确切语义**：这是上游缓存过期判定的**假设值**（`web/server_zhb.go` 的 `zhbExpectedDate`，非通达信官方承诺）。含义是"16:00 后 GET 才会触发当日重下"，**不是"16:00 必能拿到当日数据"**。自愈机制：若重下拿到的 statDate 仍是前一交易日，缓存继续判定过期，下一次 GET 会再次重下——所以晚到的更新只要再有任意一次 GET 就能补上，不会卡死。
3. **建议**：cron **维持 16:15 可以**，但 ETL 加一道校验——拉取后检查响应 `stat_date == 当日`；若不等（TDX 尚未完成更新），30-60 分钟后重试（最多 2-3 次）。这比拍死 16:30/17:00 更稳。另外"15:45 开始更新"不是上游的观测数据，TDX 实际就绪时刻待实测：如需要，上游可择一交易日在 15:30-17:00 间轮询 statDate 变化点，实测后回填本节，再定是否调整 cron。
