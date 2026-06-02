import json
import os
import sys
from typing import Dict, List, Optional

import requests

BASE_URL = os.environ.get("BASE_URL", "http://127.0.0.1:8080")

# Endpoints: 3-tuple = (name, method, path)
#            4-tuple = (name, method, path, extra)
#            5-tuple = (name, method, path, extra, slow)
# `slow=True` 表示该用例耗时较长(>30s), 默认不跑; 加 --slow 参数启用
ENDPOINTS = [
    ("quote", "GET", "/api/quote?code=000001"),
    ("kline_day", "GET", "/api/kline?code=000001&type=day"),
    ("minute", "GET", "/api/minute?code=000001&date=20241108"),
    ("trade", "GET", "/api/trade?code=000001&date=20241108"),
    ("search", "GET", "/api/search?keyword=000001"),
    ("stock_info", "GET", "/api/stock-info?code=000001"),
    ("codes_sz", "GET", "/api/codes?exchange=sz"),
    ("batch_quote", "POST", "/api/batch-quote", {"json": {"codes": ["000001", "600519", "601318"]}}),
    ("kline_history", "GET", "/api/kline-history?code=000001&type=day&start_date=20241011&end_date=20241108"),
    ("index_day", "GET", "/api/index?code=sh000001&type=day"),
    ("index_all_day", "GET", "/api/index/all?code=sh000001&type=day"),
    ("market_stats", "GET", "/api/market-stats"),
    ("market_count", "GET", "/api/market-count"),
    ("stock_codes", "GET", "/api/stock-codes"),
    ("etf_codes", "GET", "/api/etf-codes"),
    ("server_status", "GET", "/api/server-status"),
    # §2.3.3: 增强版 /api/health (返回 status/time/uptime_seconds/gbbq_cache_size/goroutines/memory_mb)
    # 已切到标准信封 {code,message,data}; 老格式 `{"status":..,"time":..}` 不再保留
    ("health_enhanced", "GET", "/api/health"),
    # §2.3.4: 新增 /api/ready (返回 ready/uptime_seconds), 与 health 区别语义
    ("ready", "GET", "/api/ready"),
    ("etf_list", "GET", "/api/etf?exchange=sh&limit=10"),
    ("trade_history", "GET", "/api/trade-history?code=000001&date=20241108&start=0&count=200"),
    ("trade_history_full", "GET", "/api/trade-history/full?code=000001&start_date=2024-10-01&end_date=2024-10-08&limit=500"),
    ("minute_trade_all", "GET", "/api/minute-trade-all?code=000001&date=20241108"),
    ("kline_all_tdx", "GET", "/api/kline-all/tdx?code=000001&type=day&limit=1000"),
    ("kline_all_ths", "GET", "/api/kline-all/ths?code=000001&type=day&limit=1000"),
    ("workday", "GET", "/api/workday?date=2024-11-08&count=3"),
    ("workday_range", "GET", "/api/workday/range?start=2024-11-01&end=2024-11-08"),
    ("income", "GET", "/api/income?code=000001&start_date=2024-11-01&days=5,10,20"),
    ("tasks_list", "GET", "/api/tasks"),
    ("turnover", "GET", "/api/turnover?code=000001&start_date=20240101&end_date=20240131"),
    ("gbbq", "GET", "/api/gbbq?code=000001&start_date=20240101&end_date=20240131"),
    # gbbq_refresh 单只股票拉取,几秒内可完成,服务端会同步阻塞到拉完为止
    ("gbbq_refresh", "POST", "/api/gbbq/refresh", {"json": {"codes": ["sh000001"]}, "timeout": 60}),
    ("kline_index_history", "GET", "/api/kline-index-history?code=sh000001&start_date=20240101&end_date=20240131"),
    ("kline_history_dated", "GET", "/api/kline-history?code=000001&type=day&start_date=20240101&end_date=20240131"),
    ("kline_history_tdx_dated", "GET", "/api/kline-history-tdx?code=000001&type=day&start_date=20240101&end_date=20240131"),
    ("kline_history_ths_dated", "GET", "/api/kline-history-ths?code=000001&type=day&start_date=20240101&end_date=20240131"),
    # §4: 全市场当日 K 断面, 5300+ 只股票单线程串行, 4-15 分钟
    # 默认不跑(慢测试), 需用 --slow 启用
    ("market_snapshot", "GET", "/api/market-snapshot", {"timeout": 900}, True),
]


def request_endpoint(
    name: str,
    method: str,
    path: str,
    extra: Optional[Dict] = None,
    timeout: Optional[int] = 25,
) -> Dict:
    url = BASE_URL + path
    kwargs = {"timeout": timeout}
    if extra:
        kwargs.update(extra)
    response = requests.request(method, url, **kwargs)
    response.raise_for_status()
    if name == "health":
        # health接口返回非标准格式
        return response.json()
    data = response.json()
    if not isinstance(data, dict) or data.get("code") != 0:
        raise RuntimeError(f"unexpected response body: {data}")
    return data


def extract_metric(payload: Dict) -> Optional[str]:
    if not isinstance(payload, dict):
        return None
    data = payload.get("data") if "data" in payload else payload
    if isinstance(data, dict):
        if "count" in data:
            return f"count={data['count']}"
        if "Count" in data:
            return f"Count={data['Count']}"
        if "total" in data:
            return f"total={data['total']}"
        if "list" in data and isinstance(data["list"], list):
            return f"items={len(data['list'])}"
    elif isinstance(data, list):
        return f"items={len(data)}"
    return None


def main() -> int:
    run_slow = "--slow" in sys.argv

    successes: List[str] = []
    failures: List[str] = []
    skipped: List[str] = []

    for item in ENDPOINTS:
        # 3-tuple: (name, method, path)
        # 4-tuple: (name, method, path, extra)
        # 5-tuple: (name, method, path, extra, slow)
        slow = False
        if len(item) == 3:
            name, method, path = item
            extra = None
        elif len(item) == 4:
            name, method, path, extra = item
        else:
            name, method, path, extra, slow = item

        if slow and not run_slow:
            skipped.append(f"[SKIP] {name} (slow test, pass --slow to run)")
            continue

        try:
            payload = request_endpoint(name, method, path, extra)
            metric = extract_metric(payload)
            successes.append(f"[OK] {name}: {metric}" if metric else f"[OK] {name}")
        except Exception as exc:  # noqa: BLE001 - simple script
            failures.append(f"[FAIL] {name}: {exc}")

    print("\n=== API endpoint test summary ===")
    for line in successes:
        print(line)
    for line in failures:
        print(line)
    for line in skipped:
        print(line)

    if failures:
        print(f"\nPassed: {len(successes)} | Failed: {len(failures)} | Skipped: {len(skipped)}")
        return 1

    print(f"\nAll {len(successes)} endpoints passed (skipped: {len(skipped)})")
    return 0


if __name__ == "__main__":
    sys.exit(main())

