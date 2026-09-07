# 实时行情端点使用指南

> 适用版本：commit eccd882 之后（`feat(realtime): /api/realtime/quote NDJSON 流式推送 + 盘中量比`）
>
> 适用对象：需要把 TDX 五档行情 / 量比 / 累计成交量等"实时"数据喂给量化程序的开发者

## 1. 功能定位

`/api/realtime/quote` 是一个**高频轮询式的实时行情转发端点**，底层基于 TDX `client.GetQuote`（盘口五档）。

**特点**：
- NDJSON 流式输出（`application/x-ndjson; chunked`），HTTP/1.1 即可
- **Fan-out 架构**：1 个后台 polling goroutine + N 个 HTTP 客户端订阅者 → 池子只占 1 个 slot
- 1 秒 1 轮批量拉取（broker 内部 ticker 控制，**客户端不可调**）
- 含**盘中实时量比**（非全天 `volume_ratio`），基于本地内存 5 日窗口
- 不持久化任何数据（量比窗口仅内存）

**与 `/api/quote` 的区别**：

| 端点 | 模式 | 用途 |
|---|---|---|
| `GET /api/quote` | 单次拉取, 同步返回 JSON | 一次性查行情 |
| `GET /api/realtime/quote` | **持续推送** NDJSON 流 | 程序化订阅、量化监控 |

## 2. 端点列表

| 端点 | 方法 | 说明 |
|---|---|---|
| `/api/realtime/quote` | GET | NDJSON 流式行情（主端点） |
| `/api/realtime/health` | GET | broker 状态（poll 次数、最近错误、订阅者数等） |
| `/api/realtime/preheat` | POST | 主动预热量比窗口，body `{"codes":[...], "ratio_basis":5}` |
| `/api/realtime/codes` | GET | 列出已预热量比窗口的股票 |

## 3. 字段说明

NDJSON 每行一条 `QuoteTick`：

| 字段 | 类型 | 含义 | 示例 |
|---|---|---|---|
| `ts` | int | unix 秒（服务端写入时刻, Asia/Shanghai） | `1788505771` |
| `code` | string | 股票代码, **用户友好格式**（`.SH` / `.SZ` / `.BJ` 后缀） | `"600000.SH"` |
| `price` | float | 最新价（**元**, 不是厘） | `9.43` |
| `open` | float | 今日开盘（**元**） | `9.27` |
| `high` | float | 今日最高（**元**） | `9.45` |
| `low` | float | 今日最低（**元**） | `9.26` |
| `pre_close` | float | 昨日收盘（**元**） | `9.27` |
| `volume` | int | **累计成交量（手）**, 自开盘起累加 | `757659` |
| `amount` | float | 累计成交额（元） | `712270976` |
| `bid` | `[[价,量], ...]` | 5 档买盘, **降序** | `[[9.43, 1005], [9.42, 2773], ...]` |
| `ask` | `[[价,量], ...]` | 5 档卖盘, **升序** | `[[9.44, 7826], [9.45, 22843], ...]` |
| `volume_ratio` | float? | 盘中实时量比（窗口未就绪时 `null`） | `0.91` |
| `ratio_basis` | int | 实际使用的历史天数（`0` = 无窗口） | `5` |
| `error` | string? | 单只拉取失败时填, 不影响其他 code | `"upstream_unavailable"` / `"decode_failed"` |

## 4. 时段行为

接口**全天可调**，但只有盘中数据有意义：

| 时段 | 接口行为 | 推送意义 |
|---|---|---|
| 盘前 9:00-9:30 | 返回昨收/集合竞价报价 | ❌ 不算分时 |
| **盘中 9:30-11:30** | 实时报价 + 累计量递增 | ✅ 真正的分时 |
| 午休 11:30-13:00 | 报价冻结, 累计量不变 | ❌ 应跳过 |
| **盘中 13:00-15:00** | 实时报价 + 累计量继续累加 | ✅ |
| 盘后 15:00-次日 9:00 | 收盘价/昨收快照, 数据冻结 | ❌ |

**一天的有效分时点数**：240 分钟（早盘 120 + 午盘 120）= 240 根 / 每只股票。

## 5. 启动服务

### 容器化（推荐）

```bash
cd /home/orangepi/git_repos/tdx-api
docker run -d --name tdx-stock-web -p 8080:8080 \
  -e TZ=Asia/Shanghai \
  -v "$PWD/data:/app/data" \
  tdx-stock-web:latest

# 确认启动
docker logs -f tdx-stock-web   # 看到 "服务启动成功" 即可
```

### 源码

```bash
cd /home/orangepi/git_repos/tdx-api/web
go run .   # 注意: 必须 `go run .`, 不能 go run server.go
```

## 6. 快速验证

```bash
# 健康检查
curl -sS http://localhost:8080/api/realtime/health | jq .

# 主动预热 (开盘前跑一次, 避免前几秒 volume_ratio=null)
curl -sS -X POST http://localhost:8080/api/realtime/preheat \
  -H "Content-Type: application/json" \
  -d '{"codes":["600000.SH","000001.SZ"],"ratio_basis":5}' | jq .

# 等 5-8 秒让窗口就绪
sleep 5

# 看 NDJSON 流, 5 秒后自动断开
timeout 5 curl -N "http://localhost:8080/api/realtime/quote?codes=600000.SH,000001.SZ" | head -5
```

预期输出：

```json
{"ts":1788505771,"code":"600000.SH","price":9.43,"open":9.27,"high":9.45,"low":9.26,"pre_close":9.27,"volume":757659,"amount":712270976,"bid":[[9.43,1005],[9.41,3639],[9.4,2938],[9.39,2579],[9.38,2767]],"ask":[[9.44,7826],[9.45,22843],[9.46,13958],[9.47,8988]],"volume_ratio":0.91,"ratio_basis":5}
{"ts":1788505771,"code":"000001.SZ","price":11.89,"open":11.86,"high":12,"low":11.85,"pre_close":11.88,"volume":814372,"amount":969948416,"bid":[[11.89,908],...],"ask":[...],"volume_ratio":0.77,"ratio_basis":5}
```

## 7. 量化程序接入 (Python)

### 7.1 一次性预热 (程序启动时)

```python
import requests

CODES = ["600000.SH", "000001.SZ", "300750.SZ", "600519.SH", "000858.SZ"]

requests.post(
    "http://localhost:8080/api/realtime/preheat",
    json={"codes": CODES, "ratio_basis": 5},
    timeout=30,
).raise_for_status()
```

### 7.2 订阅流式行情

```python
with requests.get(
    f"http://localhost:8080/api/realtime/quote?codes={','.join(CODES)}",
    stream=True,    # 必须 stream=True 才能拿到流式响应
    timeout=None,   # 长连接, 不设超时
) as resp:
    resp.raise_for_status()
    for line in resp.iter_lines():
        if not line:
            continue
        tick = json.loads(line)
        if "error" in tick:
            # 单只拉取失败, 不影响其他 code, log 即可
            print(f"[ERROR] {tick['code']}: {tick['error']}")
            continue
        # tick 含 ts/code/price/volume/amount/bid/ask/volume_ratio
```

### 7.3 按分钟聚合 → 落盘 (核心)

TDX 的 `volume` 字段是"自开盘累计成交手数", **直接当分时图右轴**, 不要做差分。

```python
import datetime as dt
from collections import defaultdict
import pandas as pd

current_minute = {}              # code -> 当前正在累积的分钟 (datetime)
minute_data = defaultdict(dict)  # code -> {minute_dt: {price, cum_volume, cum_amount}}

for line in resp.iter_lines():
    tick = json.loads(line)
    if "error" in tick: continue
    code = tick["code"]
    ts = dt.datetime.fromtimestamp(tick["ts"])
    minute = ts.replace(second=0, microsecond=0)

    # 跨分钟 -> 旧分钟冻结
    if code in current_minute and minute != current_minute[code]:
        # ... 写入 minute_data[code][current_minute[code]] 到文件
        pass

    current_minute[code] = minute
    minute_data[code][minute] = {
        "price":      tick["price"],
        "cum_volume": tick["volume"],   # 手
        "cum_amount": tick["amount"],   # 元
    }

# 收盘后: flatten 成 DataFrame
rows = []
for code, mdict in minute_data.items():
    for m, v in sorted(mdict.items()):
        rows.append({"code": code, "minute": m, **v})
df = pd.DataFrame(rows)
df.to_csv("tdx_minute_20260907.csv", index=False)
```

### 7.4 画图交叉验证

```python
import matplotlib.pyplot as plt

fig, axes = plt.subplots(len(CODES), 1, figsize=(10, 3*len(CODES)), sharex=True)
for ax, code in zip(axes, CODES):
    sub = df[df.code == code]
    # 左轴: 价格折线
    ax.plot(sub.minute, sub.price, label="价")
    ax.set_ylabel("价(元)")
    # 右轴: 累计成交量柱状
    ax2 = ax.twinx()
    ax2.bar(sub.minute, sub.cum_volume, width=0.8, alpha=0.3, color="orange", label="累计量(手)")
    ax2.set_ylabel("累计量(手)")
    ax.set_title(code)
plt.tight_layout()
plt.savefig("tdx_minute_verify_20260907.png", dpi=150)
```

**对比同花顺 / 通达信分时图**: 打开同日的同一只股票分时图, 目视对比价格曲线 + 累计量趋势, 应高度重合。

## 8. Go 客户端示例

```go
resp, err := http.Get("http://localhost:8080/api/realtime/quote?codes=600000.SH")
if err != nil { log.Fatal(err) }
defer resp.Body.Close()

scanner := bufio.NewScanner(resp.Body)
for scanner.Scan() {
    var tick struct {
        Ts          int64    `json:"ts"`
        Code        string   `json:"code"`
        Price       float64  `json:"price"`
        Volume      int64    `json:"volume"`
        VolumeRatio *float64 `json:"volume_ratio"`
        RatioBasis  int      `json:"ratio_basis"`
        Error       string   `json:"error"`
    }
    if err := json.Unmarshal(scanner.Bytes(), &tick); err != nil {
        continue
    }
    // ... 你的策略
}
```

## 9. 关键踩坑点

1. **时区**: 容器必须 `TZ=Asia/Shanghai`。否则 `currentTradingMinute()` 永远返回 -1, 量比计算失败
2. **集合竞价 9:15-9:25**: TDX 是否返回集合竞价数据**没测试过**。开盘后看 9:30 那一根的 `cum_volume` 是否从 0 开始; 如果起点非零, 需做 baseline 调整 (减 9:25 那段的预扣量)
3. **午休 11:30-13:00**: tick 数据不变, **不要连成斜线**, 画图时跳过这段时间
4. **TDX 偶发 `decode_failed`**: 单只解析失败, 其他正常, **try/except 单只**, 别因为 1 只挂了断整条流
5. **时间戳用响应 `tick.ts`**: 不要用本地 `datetime.now()`, 否则容器时区变了会乱
6. **跨日重置**: 今日 15:00 数据冻结, 明早 9:30 重新归零。多日采集要按交易日分文件
7. **池子 4 slot**: broker 占 1, 剩 3 服务其他接口。**不会**和 `/api/market-snapshot` 等抢; 但跑 4 个 snapshot 后再起 NDJSON 仍能正常推送 (broker 拿到 1 个 slot)
8. **预热窗口**: 冷启动首次订阅某只股票时, broker 异步拉 5 天历史 minute K (~1-2 秒), 这期间该只 `volume_ratio` 是 `null`。开盘前手动调 `/api/realtime/preheat` 可提前就绪

## 10. 故障排查

| 现象 | 排查 |
|---|---|
| `volume_ratio: null` 持续 > 10s | `curl /api/realtime/codes` 看窗口; 容器时区是否对; `/api/realtime/preheat` 是否返回 `window_count > 0` |
| `{"error":"upstream_unavailable"}` 持续 | TDX 连接问题, 看 `last_poll_err` 字段; `docker logs tdx-stock-web \| grep "panic"` |
| `{"error":"decode_failed"}` 偶发 | TDX 协议层偶发 bug, 同 batch 其他 code 正常推送, **忽略即可** |
| 完全没 NDJSON 输出 (连 header 都没) | `?codes=` 拼错了; 先试 `?codes=600000.SH` 单只; 看 `/api/realtime/health` 确认 broker 在跑 |
| 1 秒只输出 1-2 行 | 正常 — broker 是 fan-out, 所有订阅者共享同一份数据; 同一 code 无论多少客户端订阅都只 poll 一次 |
| 多个客户端想看不同间隔怎么办 | 当前所有客户端**共享 broker 的 1 秒间隔**; 要改需扩 broker (多 ticker) 或客户端侧过滤 |

## 11. 已知限制 / 后续 sprint 候选

- [ ] **WebSocket 端点 `/api/realtime/ws`**: 当前 NDJSON 是单向流; 加 WS 支持动态订阅/退订
- [ ] **跨日量比窗口滚动**: 当前跨自然日不自动重建, 需重启或手动 reheat
- [ ] **量比持久化**: 当前 5 日窗口仅内存, 服务重启丢失; 每日收盘归档到 SQLite 可避免重复拉历史
- [ ] **可配置轮询间隔**: 当前 broker 固定 1 秒; 想降速到 5 秒省流量要改 broker 内部 ticker
- [ ] **多 broker 分片**: >100 只订阅时按股票代码 hash 分到多个 polling goroutine
- [ ] **集合竞价数据**: 9:15-9:25 段数据未验证; 需实际开盘日测试

## 12. 实现细节 (给维护者看)

- **Broker**: `web/server_realtime.go` 的 `Broker` struct, fan-out 模式, 1 个后台 polling goroutine
- **量比窗口**: `VolumeWindow` 类型, `Days [][]int64` 存 5 天每分钟成交量 (手); `Ratio()` 函数计算
- **代码格式归一化**: `normalizeCode()` 接受 `"600000.SH"` / `"SH600000"` / `"600000"`, 内部统一存 6 位数字; 出口处 `withExchangeSuffix()` 还原 `.SH/.SZ`
- **重要 fix**: `client.GetQuote` 内部会**修改入参 slice** (`client.go:302-312`), pollOnce 必须 `copy` 一份再传, 否则下次循环 keys 全乱
- **零协议层改动**: `client.go` / `pool.go` / `protocol/` / `extend/` 一行未改
- **池子配置**: `ManageConfig.Number` 保持默认 4, broker 仅占 1 slot
