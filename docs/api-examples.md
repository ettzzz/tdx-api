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
