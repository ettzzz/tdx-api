# 📡 TDX股票数据API接口文档

## 🌐 基础信息

**Base URL**: `http://your-server:8080`  
**Content-Type**: `application/json; charset=utf-8`  
**编码**: UTF-8

---

## 📋 响应格式

所有接口统一返回格式：

```json
{
  "code": 0,           // 0=成功, -1=失败
  "message": "success", // 提示信息
  "data": {}           // 数据内容
}
```

---

## 📊 API接口列表

### 1. 获取五档行情

**接口**: `GET /api/quote`

**描述**: 获取股票实时五档买卖盘口数据

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| code | string | 是 | 股票代码（如：000001）支持多个，逗号分隔 |

**请求示例**:
```
GET /api/quote?code=000001
GET /api/quote?code=000001,600519
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "Exchange": 0,
      "Code": "000001",
      "Active1": 2843,
      "K": {
        "Last": 12250,    // 昨收价（厘）
        "Open": 12300,    // 开盘价（厘）
        "High": 12600,    // 最高价（厘）
        "Low": 12280,     // 最低价（厘）
        "Close": 12500    // 收盘价/最新价（厘）
      },
      "ServerTime": "1730617200",
      "TotalHand": 1235000,    // 总手数
      "Intuition": 100,        // 现量
      "Amount": 156000000,     // 成交额
      "InsideDish": 520000,    // 内盘
      "OuterDisc": 715000,     // 外盘
      "BuyLevel": [            // 买五档
        {
          "Buy": true,
          "Price": 12500,      // 买一价（厘）
          "Number": 35000      // 挂单量（股）
        },
        // ... 买二到买五
      ],
      "SellLevel": [           // 卖五档
        {
          "Buy": false,
          "Price": 12510,      // 卖一价（厘）
          "Number": 30000      // 挂单量（股）
        },
        // ... 卖二到卖五
      ],
      "Rate": 0.0,
      "Active2": 2843
    }
  ]
}
```

**数据说明**:
- 价格单位：厘（1元 = 1000厘）
- 成交量单位：手（1手 = 100股）
- 挂单量单位：股

---

### 2. 获取K线数据

**接口**: `GET /api/kline`

**描述**: 获取股票K线数据（OHLC + 成交量成交额）。日/周/月K线默认返回同花顺前复权数据；若第三方源不可用将直接返回错误提示，不再自动切换通达信源。需要原始数据或自行设置兜底时，可调用文末的 `/api/kline-all/tdx` 等接口。
**描述**: 获取股票K线数据（OHLC + 成交量成交额）。日/周/月K线优先返回同花顺前复权数据，若第三方源不可用则自动回退到通达信原始数据；分钟级及小时级为原始数据。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| code | string | 是 | 股票代码（如：000001） |
| type | string | 否 | K线类型，默认day |

**K线类型(type)**:
- `minute1` - 1分钟K线（最多24000条）
- `minute5` - 5分钟K线
- `minute15` - 15分钟K线
- `minute30` - 30分钟K线
- `hour` - 60分钟/小时K线
- `day` - 日K线（默认）
- `week` - 周K线
- `month` - 月K线

**请求示例**:
```
GET /api/kline?code=000001&type=day
GET /api/kline?code=600519&type=minute30
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "Count": 100,
    "List": [
      {
        "Last": 12250,      // 昨收价（厘）
        "Open": 12300,      // 开盘价（厘）
        "High": 12600,      // 最高价（厘）
        "Low": 12280,       // 最低价（厘）
        "Close": 12500,     // 收盘价（厘）
        "Volume": 1235000,  // 成交量（手）
        "Amount": 156000000,// 成交额（厘）
        "Time": "2024-11-03T00:00:00Z",
        "UpCount": 0,       // 上涨数（指数有效）
        "DownCount": 0      // 下跌数（指数有效）
      }
      // ... 更多K线数据
    ]
  }
}
```

**数据说明**:
- 数据按时间倒序排列（最新的在前）
- 价格单位：厘
- 成交量单位：手
- 成交额单位：厘

---

### 3. 获取分时数据

**接口**: `GET /api/minute`

**描述**: 获取股票分时走势数据。接口严格按照请求日期返回结果，不再自动回退其他交易日；若指定日期无数据，将返回空列表并保留原日期。
**描述**: 获取股票分时走势数据；若查询日期或当日无数据，会自动回退至最近一个有交易数据的工作日，并在响应体中附加实际数据日期。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| code | string | 是 | 股票代码（如：000001） |
| date | string | 否 | 日期（YYYYMMDD格式），默认当天 |

**请求示例**:
```
GET /api/minute?code=000001
GET /api/minute?code=000001&date=20241103
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "date": "20251110",   // 实际数据日期，与请求日期一致
    "date": "20251107",   // 实际数据日期，可能与请求日期不同
    "Count": 240,
    "List": [
      {
        "Time": "09:31",
        "Price": 12300,    // 价格（厘）
        "Number": 1500     // 成交量（手）
      },
      {
        "Time": "09:32",
        "Price": 12310,
        "Number": 1200
      }
      // ... 240个数据点（9:30-11:30, 13:00-15:00）
    ]
  }
}
```

**数据说明**:
- 交易时段：9:30-11:30（120分钟）, 13:00-15:00（120分钟）
- 共240个数据点
- 价格单位：厘
- 若 `List` 为空，表示该日期无分时数据，请由调用方自行选择备用日期或数据源

---

### 4. 获取分时成交

**接口**: `GET /api/trade`

**描述**: 获取股票逐笔成交明细

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| code | string | 是 | 股票代码（如：000001） |
| date | string | 否 | 日期（YYYYMMDD格式），默认当天 |

**请求示例**:
```
GET /api/trade?code=000001
GET /api/trade?code=000001&date=20241103
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "Count": 1800,
    "List": [
      {
        "Time": "2024-11-03T14:59:58Z",
        "Price": 12500,    // 成交价（厘）
        "Volume": 100,     // 成交量（手）
        "Status": 0,       // 0=买入, 1=卖出, 2=中性
        "Number": 5        // 成交单数
      },
      {
        "Time": "2024-11-03T14:59:55Z",
        "Price": 12490,
        "Volume": 50,
        "Status": 1,
        "Number": 3
      }
      // ... 更多成交记录
    ]
  }
}
```

**数据说明**:
- Status: 0=主动买入(红色), 1=主动卖出(绿色), 2=中性
- 当日最多返回1800条
- 历史日期最多返回2000条
- 价格单位：厘
- 成交量单位：手

---

### 5. 搜索股票代码

**接口**: `GET /api/search`

**描述**: 根据关键词搜索股票代码和名称

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| keyword | string | 是 | 搜索关键词（代码或名称） |

**请求示例**:
```
GET /api/search?keyword=平安
GET /api/search?keyword=000001
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": [
    {
      "code": "000001",
      "name": "平安银行"
    },
    {
      "code": "601318",
      "name": "中国平安"
    }
    // ... 最多50条结果
  ]
}
```

**数据说明**:
- 支持代码和名称模糊搜索
- 最多返回50条结果
- 仅返回A股（过滤指数等）

---

### 6. 获取股票综合信息

**接口**: `GET /api/stock-info`

**描述**: 一次性获取股票的多种数据（五档行情+日K线+分时）

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| code | string | 是 | 股票代码（如：000001） |

**请求示例**:
```
GET /api/stock-info?code=000001
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "quote": {
      // 五档行情数据（同/api/quote）
    },
    "kline_day": {
      // 最近30天日K线（同/api/kline?type=day）
    },
    "minute": {
      // 今日分时数据（同/api/minute）
    }
  }
}
```

**数据说明**:
- 整合了五档行情、最近30条日K线、最新分时数据
- 分时数据自带 `date`、`Count`、`List` 字段；若 `List` 为空表示该日期无分时数据
- 分时数据自带 `date`、`Count`、`List` 字段，便于识别回退日期
- 适合快速获取股票概览，减少API调用次数

---

## 🔧 扩展接口（高级功能）

### 7. 获取股票列表

**接口**: `GET /api/codes`

**描述**: 获取指定交易所的所有股票代码列表

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| exchange | string | 否 | 交易所代码，默认all |

**交易所代码**:
- `sh` - 上海证券交易所
- `sz` - 深圳证券交易所
- `bj` - 北京证券交易所
- `all` - 全部（默认）

**请求示例**:
```
GET /api/codes
GET /api/codes?exchange=sh
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 5234,
    "exchanges": {
      "sh": 2156,
      "sz": 2845,
      "bj": 233
    },
    "codes": [
      {
        "code": "000001",
        "name": "平安银行",
        "exchange": "sz"
      }
      // ... 更多股票
    ]
  }
}
```

---

### 8. 批量获取行情

**接口**: `POST /api/batch-quote`

**描述**: 批量获取多只股票的实时行情

**请求参数** (JSON Body):
```json
{
  "codes": ["000001", "600519", "601318"]
}
```

**请求示例**:
```bash
curl -X POST http://localhost:8080/api/batch-quote \
  -H "Content-Type: application/json" \
  -d '{"codes":["000001","600519","601318"]}'
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": [
    // 数组，每个元素同/api/quote的单个股票数据
  ]
}
```

---

### 9. 获取历史K线（同花顺前复权）

**接口**: `GET /api/kline-history`

**描述**: 获取指定股票在指定时间范围内的 K 线数据，日 / 周 / 月 K 线通过同花顺 (`d.10jqka.com.cn`) 取得**前复权**数据，价格曲线连续无缝，便于长期回测与可视化。`start_date` / `end_date` 缺省时不限制。**仅服务个股**，指数 / 板块请使用 `/api/kline-index-history`。

> **端点对比**：本端点走同花顺前复权，`Amount` 字段（成交额）恒为 0（同花顺不返回）。**如需不复权与真实 `Amount` 请使用** `/api/kline-history-tdx`。两者共用同一份请求参数与响应结构。`/api/kline-history-ths` 是本端点的显式命名别名。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| code | string | 是 | 个股代码（如 000001、600519） |
| type | string | 是 | K线类型，取值见下表 |
| start_date | string | 否 | 开始日期（`YYYYMMDD` 或 `YYYY-MM-DD`），缺省时不限制起点 |
| end_date | string | 否 | 结束日期（`YYYYMMDD` 或 `YYYY-MM-DD`），缺省时不限制终点 |

**K线类型(type)**:
- `minute1` / `minute5` / `minute15` / `minute30` / `hour` - 分钟级 K 线（通达信原始）
- `day` / `week` / `month` - 日 / 周 / 月 K 线（**同花顺前复权**）

**请求示例**:
```
GET /api/kline-history?code=000001&type=day&start_date=20241001&end_date=20241101
GET /api/kline-history?code=600519&type=minute30&start_date=20241101&end_date=20241108
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "Count": 23,
    "List": [
      {
        "Time": "2024-01-02T00:00:00Z",
        "Open": 8500,
        "High": 8700,
        "Low": 8450,
        "Close": 8600,
        "Volume": 1235000,
        "Amount": 0,
        "Last": 8400
      }
    ]
  }
}
```

**数据说明**:
- 价格单位：厘（1 元 = 1000 厘）
- 成交量单位：手（1 手 = 100 股）
- `Amount` 字段：恒为 0（同花顺前复权数据源不返回成交额）
- `start_date` / `end_date` 同时省略时返回最近 800 条；指定任一端时按区间裁剪
- 日 / 周 / 月 K 线为前复权曲线，除权除息日无跳空，长期连续显示

---

### 9b. 获取历史K线（TDX 原始，不复权）

**接口**: `GET /api/kline-history-tdx`

**描述**: 获取指定股票在指定时间范围内的 K 线数据，数据源为**通达信协议原始 K 线**（不复权）。`start_date` / `end_date` 缺省时不限制。**仅服务个股**，指数 / 板块请使用 `/api/kline-index-history`。除权除息当日 K 线会**跳空**，但 `Amount`（成交额）有真实值，适合成交额 / 换手率 / 复权因子重算等场景。

**与 `/api/kline-history` 的差异**:

| 维度 | `/api/kline-history` | `/api/kline-history-tdx` |
|------|---------------------|--------------------------|
| 日 / 周 / 月 数据源 | 同花顺 HTTP（前复权） | 通达信协议（不复权） |
| 分钟级数据源 | 通达信协议（原始） | 通达信协议（原始） |
| `Amount`（成交额） | 恒为 0（同花顺不返回） | 真实值 |
| 网络依赖 | 通达信 + 同花顺 | 通达信服务器 |
| 启动 / 首次拉取 | 较慢（HTTP 调用） | 快 |
| 价格曲线 | 连续无缝 | 除权日跳空 |
| 适用场景 | 回测、长期图表 | 实时分析、成交额、换手率、因子重算 |

**请求参数**、**响应结构**、**数据说明** 与 `/api/kline-history` 一致（`Amount` 字段在日 / 周 / 月 K 线上有真实值）。

**请求示例**:
```
GET /api/kline-history-tdx?code=000001&type=day&start_date=20241001&end_date=20241101
```

---

### 9c. 全市场当日 K 线断面（v2-§4）

**接口**: `GET /api/market-snapshot`

**描述**: 一次性拉取**全市场 5300+ 只 A 股**"当天"日 K 线断面（OHLCV + 昨收 + 涨跌幅），用于量化系统每天 16:00 后**一次性入库**到自有 MySQL。**单线程串行**，预计耗时 **4-15 分钟**（受 TDX 限流影响），HTTP 客户端需设大超时（`curl -m 900`）。tdx-api 仅做数据中转，**不复权、不计算衍生字段、不入库**。

**使用场景**：
- 量化选股 / 回测：每天 16:00 调用一次，把全市场 5300+ 只的当日 OHLCV 一次性拉走
- 跨日数据回填：某日漏跑，可重跑整个 `market-snapshot`（用幂等的方式覆盖 MySQL）

**请求参数**: 无

**调用时序**：
- 推荐：**每个交易日 16:00 之后**（避开 15:00 收盘数据回填期）
- 非交易日调用：TDX 返回上一个交易日数据（**这是 TDX 协议行为，不是 bug**）

**响应结构**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "date": "2026-06-02",
    "count": 5300,
    "list": [
      {
        "code":        "sh600000",
        "open":        12340,
        "high":        12500,
        "low":         12280,
        "close":       12450,
        "last_close":  12400,
        "volume":      1234567,
        "change_pct":  0.40
      }
    ]
  }
}
```

**字段来源**（对照 `protocol/model_kline.go:41 Kline`）:

| 字段 | 数据源 | 单位 | 备注 |
|------|--------|------|------|
| `code` | 入参 | - | 带前缀小写,如 `sh600000` |
| `open`/`high`/`low`/`close` | `Kline.Open`/`High`/`Low`/`Close` | 厘 | tdx-api 不换算,如需元为单位则 ÷1000 |
| `last_close` | `Kline.Last` | 厘 | 昨收 |
| `volume` | `Kline.Volume` | 手 | 协议原生,如需股为单位则 ×100 |
| `change_pct` | `Kline.RiseRate()` | % | 协议层原生,已是百分比 |
| `date` | `time.Now().Format("2006-01-02")` | - | handler 调用当天的本地日期 |
| `count` | `len(list)` | - | 成功返回的股票数(失败的 stock 不在 list 中) |

**响应字段**（最小化, 8 个 per item）：
- 不返回 `name`、`date`(K.Time)、`change`(可算术衍生)、`amount`(用户不需要)

**请求示例**:
```
GET /api/market-snapshot
```

**性能与稳定性**：
- 耗时: 5300+ × ~50ms = 4-15 分钟（受 TDX 限流影响）
- 响应体积: 5300+ × ~200B ≈ 1MB
- 客户端超时: `curl -m 900`（15 分钟）
- 失败模式: **宽松**——单只股票拉取失败 `logs.Warnf` 后 continue,该只股票不在响应 list 中;其他股票正常返回
- 重连: handler 不做断点续传,失败请重跑(每日一次,重跑成本可接受)
- 单连接断开: TDX 单连接断开后,后续所有请求失败 → 重跑整个 endpoint

**边界与坑**:

| 情况 | 行为 | 备注 |
|------|------|------|
| 停牌股票 | K 线存在,字段可能为 0 | 量化系统入库时自己处理 |
| 非交易日调用 | TDX 返回上一个交易日数据 | 用户每天 16:00 工作日调,不会踩到 |
| 新上市股票 | TDX 返回最近一个交易日数据 | 正常 |
| 退市股票 | 不会出现在 `GetStocks()` 里 | codes 缓存会过滤 |
| 单只拉取失败 | `logs.Warnf` 记录,该只不入响应 | 宽松模式 |
| 指数 / 板块 | **不在响应中** | `GetStocks()` 通过 `protocol.IsStock()` 过滤不含指数;指数断面是 PLAN_v2 §4.6 标记的扩展,未在本节实施 |

**冒烟测试**:
```bash
# 默认不跑(slow=True)
python3 scripts/run_api_checks.py
# 跑慢测试
python3 scripts/run_api_checks.py --slow
```

---

### 9.1 获取指数 / 板块历史 K 线

**接口**: `GET /api/kline-index-history`

**描述**: 获取指数 / 板块的日 K 线（仅日 K，不复权）。支持日期范围过滤。`code` 必须显式带交易所前缀，涵盖综合指数（`sh000xxx` / `sz399xxx`）、概念板块（`sh880xxx`）、行业板块（`sh881xxx`）。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| code | string | 是 | 指数 / 板块代码（必须显式前缀，如 `sh000001`、`sz399006`、`sh880666`） |
| start_date | string | 否 | 开始日期（`YYYYMMDD` 或 `YYYY-MM-DD`），缺省时不限制起点 |
| end_date | string | 否 | 结束日期（`YYYYMMDD` 或 `YYYY-MM-DD`），缺省时不限制终点 |

**请求示例**:
```
GET /api/kline-index-history?code=sh000001
GET /api/kline-index-history?code=sh000001&start_date=20240101&end_date=20240131
GET /api/kline-index-history?code=sz399006
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "count": 23,
    "list": [
      {
        "Time": "2024-01-02T00:00:00Z",
        "Open": 297200,
        "High": 298800,
        "Low": 296800,
        "Close": 298500,
        "Volume": 123500000,
        "Amount": 156000000000000,
        "Last": 297200,
        "UpCount": 1800,
        "DownCount": 1500
      }
    ]
  }
}
```

**数据说明**:
- 仅返回日 K 线；指数 K 线额外保留 `UpCount` / `DownCount` 字段（涨跌家数）
- 价格单位：厘；成交量单位：手；成交额单位：厘
- 与 `/api/kline-history` 响应字段保持一致，便于同一套代码处理

---

### 9.2 获取个股换手率序列

**接口**: `GET /api/turnover`

**描述**: 按日计算个股换手率（成交股数 / 流通股本 × 100%）。数据源为通达信日 K 线（不复权） + gbbq 内存缓存中的流通股本。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| code | string | 是 | 个股代码（如 `000001`、`600519`） |
| start_date | string | 否 | 开始日期（`YYYYMMDD` 或 `YYYY-MM-DD`），缺省时返回全部历史 |
| end_date | string | 否 | 结束日期（`YYYYMMDD` 或 `YYYY-MM-DD`），缺省时取到最近一日 |

**请求示例**:
```
GET /api/turnover?code=000001
GET /api/turnover?code=000001&start_date=20240101&end_date=20240131
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "code": "sz000001",
    "count": 23,
    "list": [
      {
        "date": "2024-01-02",
        "turnover": 0.85,
        "float": 19405571850
      }
    ]
  }
}
```

**数据说明**:
- `turnover` 单位为**百分比**（1.23 表示 1.23%）
- `float` 单位为**股**（流通股本）
- 计算公式：`turnover = (kline.Volume * 100 / float) * 100`（已封装为 `protocol.Equity.Turnover`）
- TDX 成交量单位为**手**（1 手 = 100 股），内部已转换
- 当 gbbq 缓存尚未同步该股股本数据时，对应日期 `turnover` 为 0、`float` 为 0

---

### 9.3 获取个股股本变迁 / 除权除息

**接口**: `GET /api/gbbq`

**描述**: 返回指定日期范围内个股的股本变化与除权除息事件。数据源为 TDX 协议层推送的 gbbq 记录，由 `gbbq` 管理器在内存中缓存。`date` 字段为**生效日**（TDX 推送时间为 15:00 表示当日已生效，对外以 +1 天为生效日）。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| code | string | 是 | 个股代码（如 `000001`、`600519`） |
| start_date | string | 否 | 开始日期（`YYYYMMDD` 或 `YYYY-MM-DD`），缺省时返回全部历史 |
| end_date | string | 否 | 结束日期（`YYYYMMDD` 或 `YYYY-MM-DD`），缺省时取到最近一日 |

**请求示例**:
```
GET /api/gbbq?code=000001
GET /api/gbbq?code=000001&start_date=20240101&end_date=20240131
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "code": "sz000001",
    "equity": [
      {"date": "2024-06-19", "category": 5, "float": 19405753400, "total": 19405753400}
    ],
    "xrxd": [
      {"date": "2024-06-19", "fenhong": 1.6, "peigujia": 0, "songzhuangu": 0, "peigu": 0}
    ]
  }
}
```

**数据说明**:
- `equity`（股本变化）字段：
  - `date` - 生效日（`YYYY-MM-DD`）
  - `category` - 变化类型：2=送配股上市, 3=非流通股上市, 5=股本变化, 7=股份回购, 8=增发新股上市, 9=转配股上市, 10=可转债上市
  - `float` / `total` - 流通 / 总股本，单位为**股**
- `xrxd`（除权除息）字段（每 10 股对应数值）：
  - `date` - 生效日
  - `fenhong` - 分红（元 / 10 股）
  - `peigujia` - 配股价
  - `songzhuangu` - 送转股
  - `peigu` - 配股
- 当 gbbq 缓存尚未同步时，`equity` / `xrxd` 均为空数组

---

### 9.4 主动刷新 gbbq 缓存

**接口**: `POST /api/gbbq/refresh`

**描述**: 主动触发 gbbq（股本变迁 / 除权除息）数据拉取。空缓存冷启动时服务不会自动拉取，需要调用方显式调此端点。**同步阻塞**，全量 11000+ 只预计 9-15 分钟（取决于 TDX 限流），HTTP 客户端需设置大超时（`curl -m 900`）。

**请求参数（请求体 JSON，可为空）**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| codes | array | 否 | 要刷新的股票代码列表（支持 `sh600000` / `600000` 两种写法，大小写不敏感）。**缺省 / 空数组 / null = 全量**（从本地 codes 缓存读） |

**请求示例**:
```
# 全量刷新
curl -X POST http://127.0.0.1:8080/api/gbbq/refresh \
  -H "Content-Type: application/json" \
  -d '{}'

# 刷单只
curl -X POST http://127.0.0.1:8080/api/gbbq/refresh \
  -H "Content-Type: application/json" \
  -d '{"codes":["sh600000","sz000001"]}'
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "success_count": 5300,
    "failed_count": 0,
    "failed": {},
    "duration_ms": 482301
  }
}
```

**数据说明**:
- `success_count` - 成功刷新的股票数
- `failed_count` - 失败的股票数
- `failed` - `code -> error` 映射，**只包含失败的股票**（成功的 list 不返回，调用方按入参自己 diff）
- `duration_ms` - 整个 refresh 耗时（毫秒）
- **宽松失败模式**：单只股票拉取失败不影响其他股票，返回值会按 `success` / `failed` 分别统计
- 启动时 `gbbq` 缓存为空时，`/api/turnover` 返回的换手率均为 0；调本端点拉完数据后 `/api/turnover` 立即有结果

**典型使用流程**:
```bash
# 1. 启动服务（gbbq 缓存空, 但服务已就绪）
./stock-web &

# 2. 先拉关注的若干只
curl -X POST http://127.0.0.1:8080/api/gbbq/refresh \
  -H "Content-Type: application/json" \
  -d '{"codes":["sh000688","sh688799","sz300620"]}'

# 3. 现在 /api/turnover /api/gbbq 接口能查到这 N 只的数据

# 4. 隔天（或每周）做一次全量
curl -X POST http://127.0.0.1:8080/api/gbbq/refresh \
  -H "Content-Type: application/json" \
  -d '{}'
```

---

### 10. 获取指数数据

**接口**: `GET /api/index`

**描述**: 获取指数K线数据（如上证指数、深证成指）

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|-----|------|------|------|
| code | string | 是 | 指数代码（如：sh000001） |
| type | string | 否 | K线类型，默认day |

**常用指数代码**:
- `sh000001` - 上证指数
- `sz399001` - 深证成指
- `sz399006` - 创业板指
- `sh000300` - 沪深300

**请求示例**:
```
GET /api/index?code=sh000001&type=day
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "Count": 100,
    "List": [
      {
        "Last":      3080,
        "Open":      3100,
        "High":      3120,
        "Low":       3070,
        "Close":     3110,
        "Volume":    1234567,
        "Amount":    0,
        "Time":      "2024-11-08T00:00:00Z",
        "UpCount":   1234,
        "DownCount": 567
      }
    ]
  }
}
```

**数据说明**:
- 与 `/api/kline` 个股响应结构相同（KlineResp），但 `UpCount` / `DownCount` 字段在指数上有意义
- 价格/成交量单位同 §2
```

---

### 11. 获取服务状态

**接口**: `GET /api/server-status`

**描述**: 返回API服务运行状态。

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "status": "running",
    "connected": true,
    "version": "1.0.0",
    "uptime": "unknown"
  }
}
```

---

### 11a. 健康检查（增强版）

**接口**: `GET /api/health`

**描述**: 进程级健康检查，给 docker healthcheck / k8s liveness probe 用。**PLAN_v2 §2.3.3** 增强版：除 `status` 外还返回进程级运行时指标。**已切到标准响应信封**（`{code, message, data}`），老格式 `{"status":"healthy","time":...}` 不再保留。

**请求参数**: 无

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "status":          "healthy",
    "time":            1730617200,
    "uptime_seconds":  123,
    "gbbq_cache_size": 0,
    "goroutines":      12,
    "memory_mb":       8
  }
}
```

**响应字段**:

| 字段 | 类型 | 说明 |
|------|------|------|
| `status` | string | 固定 `"healthy"`，只要进程能响应就 200 |
| `time` | int64 | 当前 unix 时间戳（秒），**不再是写死的 1730617200** |
| `uptime_seconds` | int64 | 进程启动至今的秒数，与 `/api/ready` 同一基准 |
| `gbbq_cache_size` | int | gbbq 内存缓存中的股票数；`0` 表示尚未拉过（正常冷启动状态） |
| `goroutines` | int | 当前 goroutine 数，用来辅助观察是否有泄漏 |
| `memory_mb` | int | 当前堆分配内存（`runtime.MemStats.Alloc`），粗略指标 |

**请求示例**:
```
GET /api/health
```

**典型用途**:
- docker `HEALTHCHECK` 探活（容器内 `wget --spider http://localhost:8080/api/health`）
- k8s `livenessProbe`——进程崩溃 / OOM 时返回非 200
- 监控 / 告警系统定时拉取，采集 `gbbq_cache_size` / `goroutines` / `memory_mb` 做趋势图

---

### 11b. 就绪检查

**接口**: `GET /api/ready`

**描述**: 就绪检查，**PLAN_v2 §2.3.4** 新增。语义为 "服务可接收 HTTP 请求"——给 k8s `readinessProbe` / 反向代理 upstream 健康检查用。

**与 `/api/health` 的差异**:

| 维度 | `/api/health` | `/api/ready` |
|------|---------------|--------------|
| 语义 | 进程级存活 | 服务可接收请求 |
| 失败含义 | 进程崩溃 / 死锁 | 启动未完成 / 过载拒绝流量 |
| 典型用途 | docker healthcheck / k8s liveness | k8s readiness / nginx upstream |
| 响应字段 | status + 6 个运行时指标 | ready + uptime_seconds |

**§3 之后语义变化**：gbbq 缓存按需拉取（`POST /api/gbbq/refresh`），启动时不再阻塞；**gbbq 缓存是否为空不再阻塞 ready**。缓存空时 `/api/turnover` 等端点仍正常返回（turnover=0），由调用方按需主动触发 refresh。

**请求参数**: 无

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "ready":          true,
    "uptime_seconds": 12
  }
}
```

**请求示例**:
```
GET /api/ready
```

---

### 12. 创建批量K线入库任务

**接口**: `POST /api/tasks/pull-kline`

**描述**: 启动后台任务，批量拉取指定股票、指定周期的K线数据并存入本地数据库（默认目录：`data/database/kline`）。任务在后台异步执行，可通过任务管理接口查询状态。

**请求参数**（JSON Body）:
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| codes | array | 否 | 股票代码数组，默认遍历全部A股 |
| tables | array | 否 | K线类型列表，取值见下表，默认 `["day"]` |
| dir | string | 否 | 数据库存储目录，默认 `data/database/kline` |
| limit | int | 否 | 并发协程数量，默认1 |
| start_date | string | 否 | 起始日期阈值（`YYYY-MM-DD` 或 `YYYYMMDD`），早于此日期的数据不会重新拉取 |

**K线类型列表**:
`minute`, `5minute`, `15minute`, `30minute`, `hour`, `day`, `week`, `month`, `quarter`, `year`

**请求示例**:
```bash
curl -X POST http://localhost:8080/api/tasks/pull-kline \
  -H "Content-Type: application/json" \
  -d '{
    "codes": ["000001","600519"],
    "tables": ["day","week","month"],
    "limit": 4,
    "start_date": "2020-01-01"
  }'
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "task_id": "9b0d1b1b-7c3d-4ce6-9a0e-bd9f5e0dcf3b"
  }
}
```

---

### 13. 创建分时成交入库任务

**接口**: `POST /api/tasks/pull-trade`

**描述**: 拉取指定股票从 `start_year` 到 `end_year` 的历史分时成交数据，并自动导出CSV（默认目录：`data/database/trade`）。

**请求参数**（JSON Body）:
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| code | string | 是 | 股票代码（如：000001） |
| dir | string | 否 | 输出目录，默认 `data/database/trade` |
| start_year | int | 否 | 起始年份，默认2000 |
| end_year | int | 否 | 结束年份，默认当年 |

**请求示例**:
```bash
curl -X POST http://localhost:8080/api/tasks/pull-trade \
  -H "Content-Type: application/json" \
  -d '{
    "code": "000001",
    "start_year": 2015,
    "end_year": 2023
  }'
```

**响应示例**同上，返回 `task_id`。

---

### 14. 查询与控制任务

| 接口 | 方法 | 描述 |
|------|------|------|
| `/api/tasks` | GET | 列出所有已创建任务及状态 |
| `/api/tasks/{task_id}` | GET | 查询指定任务详情 |
| `/api/tasks/{task_id}/cancel` | POST | 取消正在执行的任务 |

**任务状态枚举**:
- `running`：执行中
- `success`：已完成
- `failed`：执行失败，`error` 字段包含原因
- `cancelled`：已取消

**响应示例** (`GET /api/tasks/{task_id}`):
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "id": "9b0d1b1b-7c3d-4ce6-9a0e-bd9f5e0dcf3b",
    "type": "pull_kline",
    "status": "running",
    "started_at": "2025-11-10T13:05:26.123456+08:00"
  }
}
```

---

### 15. 获取ETF列表

**接口**: `GET /api/etf`

**描述**: 返回当前可用的 ETF 基金列表，可按交易所过滤并限制返回数量。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| exchange | string | 否 | 交易所，`sh` / `sz` / `all`（默认） |
| limit | int | 否 | 返回条数限制 |

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 2,
    "list": [
      {
        "code": "510300",
        "name": "沪深300ETF",
        "exchange": "sh",
        "last_price": 4.123
      },
      {
        "code": "159915",
        "name": "创业板ETF",
        "exchange": "sz",
        "last_price": 1.876
      }
    ]
  }
}
```

---

### 16. 获取历史分时成交（分页）

**接口**: `GET /api/trade-history`

**描述**: 分页获取历史交易日的分时成交明细，单次最多返回 2000 条。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| code | string | 是 | 股票代码 |
| date | string | 是 | 交易日期（YYYYMMDD） |
| start | int | 否 | 起始游标，默认0 |
| count | int | 否 | 返回条数，默认2000，最大2000 |

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "Count": 2000,
    "List": [
      {
        "Price": 12345,
        "Time": "2024-11-08T14:58:00+08:00",
        "Status": 0,
        "Volume": 50
      }
    ]
  }
}
```

---

### 17. 获取全天分时成交

**接口**: `GET /api/minute-trade-all`

**描述**: 一次性获取某交易日的全部分时成交明细；未指定日期时返回当日实时成交。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| code | string | 是 | 股票代码 |
| date | string | 否 | 交易日期（YYYYMMDD），默认当天 |

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "Count": 3150,
    "List": [
      {
        "Price": 12500,
        "Time": "2024-11-08T09:30:01+08:00",
        "Volume": 10,
        "Status": 0
      }
    ]
  }
}
```

---

### 18. 查询交易日信息

**接口**: `GET /api/workday`

**描述**: 查询指定日期是否为交易日，并返回前后若干个最近的交易日。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| date | string | 否 | 查询日期（YYYYMMDD 或 YYYY-MM-DD），默认当天 |
| count | int | 否 | 返回的前后交易日数量，范围 1-30，默认1 |

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "date": {
      "iso": "2024-11-08",
      "numeric": "20241108"
    },
    "is_workday": true,
    "next": [
      {
        "iso": "2024-11-11",
        "numeric": "20241111"
      }
    ],
    "previous": [
      {
        "iso": "2024-11-07",
        "numeric": "20241107"
      }
    ]
  }
}
```

---

### 19. 获取市场证券数量

**接口**: `GET /api/market-count`

**描述**: 获取上交所、深交所、北交所当前可用证券数量统计。

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "total": 7654,
    "exchanges": [
      { "exchange": "sh", "count": 2163 },
      { "exchange": "sz", "count": 5337 },
      { "exchange": "bj", "count": 154 }
    ]
  }
}
```

---

### 20. 获取股票代码列表

**接口**: `GET /api/stock-codes`

**描述**: 返回全市场股票代码列表，可控制是否携带交易所前缀。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| limit | int | 否 | 返回条数限制 |
| prefix | bool | 否 | 是否包含交易所前缀（默认 true，即 `sh600000`） |

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "count": 5600,
    "list": [
      "sh600000",
      "sz000001"
      // ...
    ]
  }
}
```

---

### 21. 获取ETF代码列表

**接口**: `GET /api/etf-codes`

**描述**: 返回所有 ETF 基金代码，参数与 `/api/stock-codes` 相同。

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "count": 200,
    "list": [
      "sh510050",
      "sz159915"
    ]
  }
}
```

---

### 22. 获取股票全部历史K线

**接口**: `GET /api/kline-all`（别名 `/api/kline-all/tdx`）

**描述**: 返回指定股票在某个周期的全部历史 K 线数据，**数据源为通达信原始（不复权）**。
如需前复权数据请使用 `/api/kline-all/ths` 或 `/api/kline-history`（推荐）。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| code | string | 是 | 股票代码（如：000001） |
| type | string | 否 | K 线类型，默认 day，可选 minute1/5/15/30/hour/day/week/month/quarter/year |
| limit | int | 否 | 返回条数限制（从最近开始截取） |

**请求示例**:
```
GET /api/kline-all/tdx?code=000001&type=day&limit=200
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "count": 200,
    "list": [
      {
        "Last":      12250,
        "Open":      12300,
        "High":      12600,
        "Low":       12280,
        "Close":     12500,
        "Volume":    1235000,
        "Amount":    156000000,
        "Time":      "2024-11-08T00:00:00Z",
        "UpCount":   0,
        "DownCount": 0
      }
    ],
    "meta": {
      "source":      "tdx",
      "type":        "day",
      "batch_limit": 800,
      "notes":       [
        "通达信单次底层请求最多返回 800 条数据，服务端已顺序拼接全量结果",
        "对于上市时间较长的标的，请预估调用耗时（通常 1-5 秒），客户端可增加超时时间"
      ]
    }
  }
}
```

**注意**: 全量数据较大，建议配合 `limit` 控制响应大小。

---

### 23. 获取指数全部历史K线

**接口**: `GET /api/index/all`

**描述**: 返回指数在各周期的全部历史 K 线数据。**数据源为通达信原始（不复权）**——指数通常不需要复权。

**请求参数**与 `/api/kline-all` 相同（见 §22）。

**请求示例**:
```
GET /api/index/all?code=sh000001&type=day
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "count": 200,
    "list": [
      {
        "Last":      3080,
        "Open":      3100,
        "High":      3120,
        "Low":       3070,
        "Close":     3110,
        "Volume":    1234567,
        "Amount":    0,
        "Time":      "2024-11-08T00:00:00Z",
        "UpCount":   1234,
        "DownCount": 567
      }
    ]
  }
}
```

**数据说明**:
- 指数 K 线自带 `UpCount` / `DownCount` 字段（涨跌家数），个股 K 线这两字段恒为 0
- 数据源为通达信；同花顺不提供指数全量接口

---

### 24. 获取上市以来分时成交

**接口**: `GET /api/trade-history/full`

**描述**: 返回指定股票上市以来的全部历史分时成交明细，可选截断截止日期与限制数量。
**分日期批次拉取**，单日 TDX 最多返回 2000 条，全量范围可能较大，建议配合 `limit` 控制。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| code | string | 是 | 股票代码 |
| start_date | string | 否 | 起始日期（YYYYMMDD 或 YYYY-MM-DD），缺省时不限制起点 |
| end_date | string | 否 | 结束日期（YYYYMMDD 或 YYYY-MM-DD），默认昨日（不含今天） |
| before | string | 否 | 截止日期（同 `end_date`，二选一；优先级高于 `end_date`） |
| include_today | bool | 否 | 是否包含当天数据（默认 `false`；TDX 当日数据持续回填，建议等收盘后） |
| limit | int | 否 | 返回条数限制（从最近开始截取） |

**请求示例**:
```
GET /api/trade-history/full?code=000001&start_date=20241101&end_date=20241108&limit=5000
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "code":          "000001",
    "start_date":    "2024-11-01",
    "end_date":      "2024-11-08",
    "limit":         5000,
    "count":         1234,
    "truncated":     false,
    "covered_dates": ["20241101", "20241104", "20241105", "20241106", "20241107", "20241108"],
    "list": [
      {
        "time":   "2024-11-08T14:59:58Z",
        "price":  12.50,
        "volume": 100,
        "status": 0,
        "number": 5
      }
    ]
  }
}
```

**响应字段**:

| 字段 | 类型 | 说明 |
|------|------|------|
| `code` | string | 入参股票代码 |
| `start_date` / `end_date` | string | 实际处理的时间范围（YYYY-MM-DD） |
| `limit` | int | 入参限制 |
| `count` | int | 实际返回条数 |
| `truncated` | bool | 是否被 `limit` 截断（true 时 `covered_dates` 不完整） |
| `covered_dates` | []string | 实际拉到的交易日列表（YYYYMMDD） |
| `list` | array | 分时成交明细数组 |
| `list[].time` | string | 成交时间（RFC3339，UTC） |
| `list[].price` | float64 | 成交价（元；厘 ÷ 1000） |
| `list[].volume` | int | 成交量（手） |
| `list[].status` | int | 0=主动买入(红色), 1=主动卖出(绿色), 2=中性 |
| `list[].number` | int | 成交单数 |

---

### 25. 获取交易日范围

**接口**: `GET /api/workday/range`

**描述**: 返回指定起止日期之间的所有交易日（含起止两天本身，若是交易日）。
底层走 `manager.Workday.Range`，**未命中本地缓存时自动从 TDX 拉取**（首次启动或新日期段）。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| start | string | 是 | 起始日期（YYYYMMDD 或 YYYY-MM-DD） |
| end | string | 是 | 结束日期（YYYYMMDD 或 YYYY-MM-DD），需 ≥ start |

**请求示例**:
```
GET /api/workday/range?start=2024-11-01&end=2024-11-08
```

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": [
    {"iso": "2024-11-01", "numeric": "20241101"},
    {"iso": "2024-11-04", "numeric": "20241104"},
    {"iso": "2024-11-05", "numeric": "20241105"},
    {"iso": "2024-11-06", "numeric": "20241106"},
    {"iso": "2024-11-07", "numeric": "20241107"},
    {"iso": "2024-11-08", "numeric": "20241108"}
  ]
}
```

**响应字段**:
- 每个元素含 `iso`（YYYY-MM-DD 格式）和 `numeric`（YYYYMMDD 格式）两个日期表达
- 返回已排序（升序）

---

### 26. 计算收益区间指标

**接口**: `GET /api/income`

**描述**: 以某日收盘价格为基准，计算若干交易日后的收益情况。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| code | string | 是 | 股票代码 |
| start_date | string | 是 | 基准日期（YYYYMMDD 或 YYYY-MM-DD） |
| days | string | 否 | 多个天数偏移（逗号分隔），默认 5,10,20,60,120 |

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "count": 3,
    "list": [
      {
        "offset": 5,
        "time": "2024-11-15T15:00:00+08:00",
        "rise": 350.0,
        "rise_rate": 0.0285,
        "source": { "close": 12250.0, "open": 12300.0, "...": 0 },
        "current": { "close": 12580.0, "open": 12600.0, "...": 0 }
      }
    ]
  }
}
```

---

## 💡 使用示例

### Python示例

```python
import requests

BASE_URL = "http://your-server:8080"

# 1. 获取五档行情
def get_quote(code):
    url = f"{BASE_URL}/api/quote?code={code}"
    response = requests.get(url)
    data = response.json()
    if data['code'] == 0:
        return data['data']
    return None

# 2. 获取日K线
def get_kline(code, type='day'):
    url = f"{BASE_URL}/api/kline?code={code}&type={type}"
    response = requests.get(url)
    data = response.json()
    if data['code'] == 0:
        return data['data']['List']
    return None

# 3. 搜索股票
def search_stock(keyword):
    url = f"{BASE_URL}/api/search?keyword={keyword}"
    response = requests.get(url)
    data = response.json()
    if data['code'] == 0:
        return data['data']
    return None

# 使用示例
if __name__ == "__main__":
    # 搜索股票
    stocks = search_stock("平安")
    print(f"搜索结果: {stocks}")
    
    # 获取行情
    quote = get_quote("000001")
    print(f"最新价: {quote[0]['K']['Close'] / 1000}元")
    
    # 获取K线
    klines = get_kline("000001", "day")
    print(f"获取到{len(klines)}条K线数据")
```

### JavaScript示例

```javascript
const BASE_URL = 'http://your-server:8080';

// 1. 获取五档行情
async function getQuote(code) {
    const response = await fetch(`${BASE_URL}/api/quote?code=${code}`);
    const data = await response.json();
    if (data.code === 0) {
        return data.data;
    }
    return null;
}

// 2. 获取K线
async function getKline(code, type = 'day') {
    const response = await fetch(`${BASE_URL}/api/kline?code=${code}&type=${type}`);
    const data = await response.json();
    if (data.code === 0) {
        return data.data.List;
    }
    return null;
}

// 3. 批量获取行情
async function batchGetQuote(codes) {
    const response = await fetch(`${BASE_URL}/api/batch-quote`, {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({ codes })
    });
    const data = await response.json();
    return data.data;
}

// 使用示例
(async () => {
    // 获取行情
    const quote = await getQuote('000001');
    console.log('最新价:', quote[0].K.Close / 1000);
    
    // 获取K线
    const klines = await getKline('000001', 'day');
    console.log('K线数据量:', klines.length);
    
    // 批量获取
    const quotes = await batchGetQuote(['000001', '600519', '601318']);
    console.log('批量行情:', quotes.length);
})();
```

### cURL示例

```bash
# 1. 获取五档行情
curl "http://localhost:8080/api/quote?code=000001"

# 2. 获取日K线
curl "http://localhost:8080/api/kline?code=000001&type=day"

# 3. 获取分时数据
curl "http://localhost:8080/api/minute?code=000001"

# 4. 搜索股票
curl "http://localhost:8080/api/search?keyword=平安"

# 5. 批量获取行情
curl -X POST http://localhost:8080/api/batch-quote \
  -H "Content-Type: application/json" \
  -d '{"codes":["000001","600519"]}'
```

---

## 📚 全量历史K线接口

为了区分不同数据源，并方便调用方自行决定兜底策略，历史K线提供以下两个独立接口，返回格式完全一致：

### 1. 通达信原始历史K线

**接口**: `GET /api/kline-all/tdx`

**说明**: 返回通达信原始（不复权）K线，内部按800条一批拼接完成。支持所有 `type` 取值（分钟、小时、日、周、月、季、年）。

**请求参数**:
| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| code | string | 是 | 股票代码（6位数字） |
| type | string | 否 | 默认 `day`，取值同 `/api/kline` |
| limit | int | 否 | 结果截断条数（从末尾取最近N条），默认返回全量 |

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "count": 4100,
    "list": [
      {
        "Time": "1991-04-03T00:00:00Z",
        "Open": 1260,
        "High": 1320,
        "Low": 1240,
        "Close": 1280,
        "Volume": 3500,
        "Amount": 4280000,
        "Last": 0
      }
      // ... 时间正序排列的全部K线
    ],
    "meta": {
      "source": "tdx",
      "type": "day",
      "batch_limit": 800,
      "notes": [
        "通达信单次底层请求最多返回 800 条数据，服务端已顺序拼接全量结果",
        "对于上市时间较长的标的，请预估调用耗时（通常 1-5 秒），客户端需自行设置超时与兜底策略",
        "若实测请求在超时阈值内成功返回数据，即视为成功调用，无需按预设超时上限计入统计"
      ]
    }
  }
}
```

### 2. 同花顺前复权历史K线

**接口**: `GET /api/kline-all/ths`

**说明**: 返回同花顺前复权日K线，并提供基于日K转换的周、月K线。仅支持 `type=day/week/month`。

**请求参数**: 同上，`type` 限于 `day`、`week`、`month`。

**响应示例**:
```json
{
  "code": 0,
  "message": "success",
  "data": {
    "count": 4100,
    "list": [
      {
        "Time": "1991-04-03T00:00:00Z",
        "Open": 1260,
        "High": 1320,
        "Low": 1240,
        "Close": 1280,
        "Volume": 3500,
        "Amount": 4280000,
        "Last": 0
      }
      // ... 全量前复权数据
    ],
    "meta": {
      "source": "ths",
      "type": "day",
      "batch_limit": 4100,
      "notes": [
        "同花顺接口一次性返回前复权数据，响应时长依赖网络与标的数据量（通常 2-8 秒）",
        "建议调用方在 Python 等客户端中设置 ≥10 秒超时时间，并按需准备自定义兜底逻辑",
        "若实测请求在超时阈值内成功返回数据，即视为成功调用，无需按预设超时上限计入统计"
      ]
    }
  }
}
```

> ⚠️ **提示**：上述接口不会对接第三方兜底逻辑；若返回空或失败，请由调用方自行决定重试或切换数据源。

---

## 🔒 错误码说明

| code | message | 说明 |
|------|---------|------|
| 0 | success | 请求成功 |
| -1 | 股票代码不能为空 | 缺少必填参数code |
| -1 | 获取行情失败: xxx | 数据获取失败，xxx为具体错误 |
| -1 | 获取K线失败: xxx | K线数据获取失败 |
| -1 | 未找到相关股票 | 搜索无结果 |
| -1 | 搜索关键词不能为空 | 缺少keyword参数 |

---

## 📊 数据单位换算

### 价格单位
- **返回值**：厘（1元 = 1000厘）
- **换算公式**：元 = 厘 / 1000
- **示例**：12500厘 = 12.50元

### 成交量单位
- **返回值**：手（1手 = 100股）
- **换算公式**：股 = 手 × 100
- **示例**：1235手 = 123500股

### 成交额单位
- **返回值**：厘
- **换算公式**：元 = 厘 / 1000
- **示例**：156000000厘 = 156000元 = 15.6万元

---

## 🚀 性能建议

1. **批量请求**：使用批量接口代替多次单个请求
2. **缓存**：对不常变化的数据（如股票列表）做本地缓存
3. **限流**：避免频繁请求，建议间隔>=3秒
4. **压缩**：使用gzip压缩减少传输量

---

## 📝 更新日志

### v1.0.0 (2024-11-03)
- ✅ 实现基础6个API接口
- ✅ 统一响应格式
- ✅ 完整文档和示例

### v1.1.0 (计划中)
- 🔄 批量查询接口
- 🔄 历史K线范围查询
- 🔄 指数数据接口
- 🔄 WebSocket实时推送

---

## 📞 技术支持

- 文档地址：本文件
- API测试：使用Postman或cURL
- 问题反馈：GitHub Issues

---

**Happy Coding!** 🎉

