# API 使用示例

## 仿射前复权 K 线 (QFQ)

使用 TDX 原始 K 线 + gbbq 事件本地计算前复权，与同花顺 QFQ 结果一致。

**端点**: `GET /api/kline-history-qfq`

**参数**:
- `code`: 股票代码（如 `sh600000`）
- `type`: K 线类型（`day`/`week`/`month`）
- `start_date`: 开始日期（可选，`YYYY-MM-DD`）
- `end_date`: 结束日期（可选，`YYYY-MM-DD`）

**前置条件**: gbbq 缓存必须已填充。若为空，调用 `POST /api/gbbq/refresh`。

**示例**:
```bash
# 先刷新 gbbq 缓存（首次调用）
curl -X POST "http://localhost:8080/api/gbbq/refresh"

# 获取仿射前复权日 K
curl "http://localhost:8080/api/kline-history-qfq?code=sh600000&type=day"

# 获取指定日期范围
curl "http://localhost:8080/api/kline-history-qfq?code=sz002222&type=day&start_date=2025-01-01&end_date=2025-12-31"
```

**响应格式**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "count": 100,
    "list": [
      {
        "time": "2025-05-20T15:00:00+08:00",
        "open": 7500,
        "high": 7600,
        "low": 7400,
        "close": 7550,
        "volume": 1000000,
        "amount": 7550000000
      }
    ]
  }
}
```

**与 THS QFQ 的区别**:
- THS: 依赖外部 HTTP 到 `d.10jqka.com.cn`，`Amount` 恒为 0
- QFQ: 本地计算，`Amount` 有真实调整值，不依赖外部服务

---

## 盘后数据：板块/统计/行业归属 (v2.1.0)

数据源为通达信 `zhb.zip` 盘后包（每交易日一份，收盘后更新）。四个 GET 端点共享同一份服务端缓存：
首次调用或缓存数据日期早于「最近已收盘交易日」（16:00 就绪线）时，**同步触发刷新，实测约 2-8 秒**，
客户端超时建议 ≥30s；缓存命中时毫秒级返回。`POST /api/blocks/refresh` 强制刷新。
单个数据源失败不阻断整体，失败原因在响应 `errors` 字段透出。

### GET /api/blocks — 行业/概念/风格地域/指数/专业板块全量及成分股

**参数**: `type`（可选，逗号分隔 `gn,hy,fg,zs,sp,sw`，缺省全量）

| type | 含义 | 数量(2026-09-20) | index 字段 |
|---|---|---|---|
| `gn` | 概念板块（block_gn.dat） | 269 | 880xxx |
| `hy` | 通达信行业板块（服务端合成，见下注） | 146 | 880xxx |
| `fg` | 风格地域板块（block_fg.dat） | 161 | 880xxx |
| `zs` | 指数板块（block_zs.dat，沪深300 等） | 117 | 空（文件本身无指数码） |
| `sp` | 专业板块（spblock.dat，中证500/1000/2000 等） | 35 | 空 |
| `sw` | **申万行业板块**（881 系，tdxzs3.cfg × tdxhy.SwHy 合成，一级30/二级128/三级309） | 467 | 881xxx |

> **hy 注**：`block_hy.dat` 当前各地域服务器均不再提供（2026-09-20 实测 6 台全部无数据）。
> 行业板块由服务端合成：`tdxzs.cfg` 中 Ref 为 T 码的条目（板块名 + 880xxx 指数码）×
> `tdxhy.cfg` 个股 T 码（层级编码）前缀匹配，成分覆盖率 100%。个股→行业归属原始数据见 `/api/tdx-hy`。

```bash
curl "http://localhost:8080/api/blocks"            # 全部 5 类
curl "http://localhost:8080/api/blocks?type=gn,hy" # 只要概念+行业
```

**响应格式**（`codes` 统一为 6 位代码）:
```json
{
  "code": 0, "message": "success",
  "data": {
    "count": 728,
    "stat_date": "20260918",
    "fetched_at": "2026-09-20 12:02:27",
    "errors": {},
    "list": [
      {"name": "白酒", "index": "880381", "ref": "T0305", "type": 2, "type_name": "行业板块",
       "source": "hy", "code_count": 19, "codes": ["600519", "000568", "..."]}
    ]
  }
}
```

> **ref 字段**（2026-09-20 新增）：板块的行业分类码，即合成本板块时 `tdxzs.cfg`/`tdxzs3.cfg`
> 条目的 Ref——`hy` 为通达信 T 码、`sw` 为申万 X 码，与 `/api/tdx-hy` 返回的 `tdx_hy`/`sw_hy`
> 是同一编码体系（个股码以板块 ref 为前缀，含相等）。`gn`/`fg`/`zs`/`sp` 来自板块文件直读，
> 无分类码，恒为空串。下游可用它把个股行业归属 join 到板块名（精确相等，或取最长前缀作为
> 归属到上级板块的口径）。

### GET /api/tdx-stat — 全市场盘后统计（tdxstat.cfg）

无逐股请求（单文件全市场一次解析）。响应约 3.5MB（8055 行，含沪深京）。

**字段核验状态**（已核验 = 上游对日线/东财交叉验证，见 `protocol/model_stat.go` 注释）：
`pettm`/`pe_static` 近乎精确；`div_yield` 精确（**通达信口径**）；`trend_days`/`change_pct` 精确；
`chg5`/`chg10`/`chg60` MAE≈0；`chg20` MAE0.23；`chg_ytd` 基准上年末 MAE0.73。
未命名槽位保留在每行 `fields` 数组（0 基 35 槽），槽位说明见响应顶层 `field_names`。

```bash
curl "http://localhost:8080/api/tdx-stat"
```

```json
{
  "code": 0, "message": "success",
  "data": {
    "date": "20260918", "count": 8055, "field_names": ["未知字段[0]", "market(市场 0=深 1=沪 2=京)", "..."],
    "errors": {},
    "list": [
      {"market": 0, "code": "000001", "date": "20260918",
       "pettm": 5.22, "pe_static": 5.3257, "div_yield": 5.21, "trend_days": 1,
       "change_pct": 0.54, "chg5": -0.34, "chg10": 1.2, "chg20": -2.1, "chg60": 8.9, "chg_ytd": 5.88,
       "fields": ["...", "..."]}
    ]
  }
}
```

### GET /api/tdx-stat2 — 全市场资金流向 + 板块归属（tdxstat2.cfg）

**字段核验状态**：`amount_today`/`amount_prev`（万元，精确）、`ipo_price`（10/10 全中）、
`high_52w`/`low_52w`、`block_index`（个股所属/领涨板块指数代码，个股→板块反向查询）。
注意：zhb.zip **不含融资融券**数据。响应约 2.6MB（8055 行）。

```bash
curl "http://localhost:8080/api/tdx-stat2"
```

```json
{
  "code": 0, "message": "success",
  "data": {
    "date": "20260918", "count": 8055, "field_names": ["market(市场)", "..."],
    "errors": {},
    "list": [
      {"market": 0, "code": "000001", "date": "20260918",
       "amount_today": 99991.81, "amount_prev": 88000.0, "ipo_price": 1.0,
       "high_52w": 12.08, "low_52w": 8.5, "block_index": "880845",
       "fields": ["..."]}
    ]
  }
}
```

### GET /api/tdx-hy — 全市场个股行业归属（tdxhy.cfg）

通达信行业码（`T` 前缀）+ 申万行业码（`X` 前缀）。覆盖沪深 5663 只（不含北交所）。

**参数**: `code`（可选，单股过滤，`600519.SH`/`sh600519`/`600519` 均可）

```bash
curl "http://localhost:8080/api/tdx-hy?code=600519"
```

```json
{
  "code": 0, "message": "success",
  "data": {
    "count": 1, "stat_date": "20260918", "errors": {},
    "list": [{"market": 1, "code": "600519", "tdx_hy": "T030501", "sw_hy": "X210205"}]
  }
}
```

### POST /api/blocks/refresh — 强制刷新盘后缓存

同步阻塞约 2-8 秒，刷新 blocks/stat/stat2/hy 共享的同一份缓存。模式对齐 `POST /api/gbbq/refresh`。

```bash
curl -X POST "http://localhost:8080/api/blocks/refresh"
```

```json
{
  "code": 0, "message": "success",
  "data": {
    "duration_ms": 1778, "stat_date": "20260918", "fetched_at": "2026-09-20 12:02:27", "errors": {},
    "counts": {"blocks_gn": 269, "blocks_hy": 146, "blocks_fg": 161, "blocks_zs": 117,
               "sp": 35, "sw": 467, "stat": 8055, "stat2": 8055, "hy": 5663}
  }
}
```
