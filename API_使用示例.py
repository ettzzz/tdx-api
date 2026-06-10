#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
TDX 通达信股票数据 API 使用示例 (完整版)
===========================================

覆盖全部 40 个 HTTP 端点，按功能分为 10 组。
适用于 tdx-api v2 (2025-11+)，对应 README 中的 36 个端点 + tasks 相关 4 个。

环境要求: Python 3.8+, `pip install requests`

使用方式:
    python API_使用示例.py              # 运行所有示例
    python API_使用示例.py --quick      # 只跑核心 10 个端点
    python API_使用示例.py --slow       # 包含耗时端点 (market-snapshot 等)
"""

import requests
import json
import sys
import os
from datetime import datetime


# ─────────────────────────── 配置 ───────────────────────────
BASE_URL = os.environ.get("TDX_API_URL", "http://localhost:8080")
TIMEOUT_DEFAULT = 10          # 秒，普通请求
TIMEOUT_LONG = 30             # 秒，kline-all 等批量请求
TIMEOUT_GBBQ = 600            # 秒，gbbq 全量刷新
TIMEOUT_SNAPSHOT = 900        # 秒，market-snapshot


# ─────────────────────────── 客户端 ───────────────────────────
class StockAPI:
    """TDX 股票数据 API 客户端 — 覆盖全部 40 个端点。"""

    def __init__(self, base_url=BASE_URL):
        self.base_url = base_url.rstrip("/")
        self._s = requests.Session()
        self._s.headers.setdefault("User-Agent", "tdx-api-client/2.0")

    # ═══════════════════════════════════════════════════════════
    # 工具方法
    # ═══════════════════════════════════════════════════════════

    def _get(self, path, params=None, timeout=TIMEOUT_DEFAULT):
        r = self._s.get(f"{self.base_url}{path}", params=params, timeout=timeout)
        r.raise_for_status()
        body = r.json()
        if body["code"] != 0:
            raise RuntimeError(body.get("message", "未知错误"))
        return body["data"]

    def _post(self, path, json_body=None, timeout=TIMEOUT_DEFAULT):
        r = self._s.post(f"{self.base_url}{path}", json=json_body or {}, timeout=timeout)
        r.raise_for_status()
        body = r.json()
        if body["code"] != 0:
            raise RuntimeError(body.get("message", "未知错误"))
        return body["data"]

    # ═══════════════════════════════════════════════════════════
    # 1. 实时行情
    # ═══════════════════════════════════════════════════════════

    def get_quote(self, code):
        """
        获取五档行情（单只或逗号分隔的多只）。
        code: "000001" 或 "000001,600000"
        返回: list[dict]，每只股票的买一~买五、卖一~卖五及 OHLCV
        """
        return self._get("/api/quote", {"code": code})

    def batch_get_quote(self, codes):
        """
        批量获取行情，最多 50 只。
        codes: ["000001", "600000", ...]
        返回: list[dict]
        """
        if len(codes) > 50:
            raise RuntimeError("一次最多 50 只")
        return self._post("/api/batch-quote", {"codes": codes})

    # ═══════════════════════════════════════════════════════════
    # 2. K 线数据
    # ═══════════════════════════════════════════════════════════

    def get_kline(self, code, ktype="day", limit=100):
        """
        获取最近 N 条 K 线。
        ktype: minute1 | minute5 | minute15 | minute30 | hour | day | week | month
        注意:
          - 日/周/月 走同花顺 **前复权**，价格连续无跳空
          - 分钟/小时 走 TDX 原始数据，不复权
          - 需要不复权的日 K → 用 get_kline_history_tdx()
        返回: {"Count": int, "List": [...]}
        """
        return self._get("/api/kline", {"code": code, "type": ktype})

    def get_kline_all_tdx(self, code, ktype="day", limit=0):
        """
        获取股票全量历史 K 线（TDX 原始不复权数据）。
        ktype: minute1|minute5|minute15|minute30|hour|day|week|month|quarter|year
        limit: >0 时截取最近 N 条；0 表示全部
        返回: {"count": int, "list": [...], "meta": {"source":"tdx", ...}}
        注意: Amount 字段有真实值，除权日有跳空
        """
        params = {"code": code, "type": ktype}
        if limit > 0:
            params["limit"] = limit
        return self._get("/api/kline-all/tdx", params, timeout=TIMEOUT_LONG)

    def get_kline_all_ths(self, code, ktype="day", limit=0):
        """
        获取股票全量历史 K 线（同花顺前复权）。
        ktype: day | week | month （同花顺只支持这三种）
        limit: >0 时截取最近 N 条；0 表示全部
        返回: {"count": int, "list": [...], "meta": {"source":"ths", ...}}
        注意: Amount 字段恒为 0；价格已前复权，长期回测用
        """
        params = {"code": code, "type": ktype}
        if limit > 0:
            params["limit"] = limit
        return self._get("/api/kline-all/ths", params, timeout=TIMEOUT_LONG)

    def get_kline_history(self, code, ktype="day", start_date=None, end_date=None):
        """
        获取指定日期范围的 K 线（同花顺前复权）。
        日/周/月走前复权；不支持分钟线。
        返回: {"Count": int, "List": [...]}
        """
        params = {"code": code, "type": ktype}
        if start_date:
            params["start_date"] = start_date
        if end_date:
            params["end_date"] = end_date
        return self._get("/api/kline-history", params, timeout=TIMEOUT_LONG)

    def get_kline_history_tdx(self, code, ktype="day", start_date=None, end_date=None):
        """
        获取指定日期范围的 K 线（TDX 原始不复权）。
        支持全部 K 线类型；Amount 有真实值。
        返回: {"Count": int, "List": [...]}
        """
        params = {"code": code, "type": ktype}
        if start_date:
            params["start_date"] = start_date
        if end_date:
            params["end_date"] = end_date
        return self._get("/api/kline-history-tdx", params, timeout=TIMEOUT_LONG)

    def get_kline_history_ths(self, code, ktype="day", start_date=None, end_date=None):
        """同 get_kline_history，显式别名。"""
        return self.get_kline_history(code, ktype, start_date, end_date)

    # ═══════════════════════════════════════════════════════════
    # 3. 指数 / 板块
    # ═══════════════════════════════════════════════════════════

    def get_index(self, code, ktype="day", limit=100):
        """
        获取指数最近 N 条 K 线。
        code: 必须带前缀 "sh000001" / "sz399001"
        ktype: minute1|minute5|minute15|minute30|hour|day|week|month
        limit: 1-800
        返回: {"Count": int, "List": [...]}
        """
        params = {"code": code, "type": ktype, "limit": limit}
        return self._get("/api/index", params)

    def get_index_all(self, code, ktype="day", limit=0):
        """
        获取指数全量历史 K 线。
        ktype: minute1|minute5|minute15|minute30|hour|day|week|month|quarter|year
        返回: {"count": int, "list": [...]}
        """
        params = {"code": code, "type": ktype}
        if limit > 0:
            params["limit"] = limit
        return self._get("/api/index/all", params, timeout=TIMEOUT_LONG)

    def get_kline_index_history(self, code, start_date=None, end_date=None):
        """
        获取指数/板块历史日 K（指定日期范围）。
        code: 必须显式带交易所前缀 "sh000001"
        返回: {"count": int, "list": [...]}
        """
        params = {"code": code}
        if start_date:
            params["start_date"] = start_date
        if end_date:
            params["end_date"] = end_date
        return self._get("/api/kline-index-history", params, timeout=TIMEOUT_LONG)

    # ═══════════════════════════════════════════════════════════
    # 4. 分时 & 成交
    # ═══════════════════════════════════════════════════════════

    def get_minute(self, code, date=None):
        """
        获取分时数据。
        date: 可选 "20260103" 或 "2026-01-03"，缺省 = 今日
        返回: {"date": str, "Count": int, "List": [...]}
        """
        params = {"code": code}
        if date:
            params["date"] = date
        return self._get("/api/minute", params)

    def get_trade(self, code, date=None):
        """
        获取分时成交。
        date: 可选，缺省 = 今日（最近 1800 条）
        返回: {"Count": int, "List": [...]}
        """
        params = {"code": code}
        if date:
            params["date"] = date
        return self._get("/api/trade", params)

    def get_trade_history(self, code, date, start=0, count=2000):
        """
        获取历史分时成交（分页）。
        date: "20260103" 或 "2026-01-03"
        count: 最大 2000
        返回: {"Count": int, "List": [...]}
        """
        params = {"code": code, "date": date, "start": start, "count": count}
        return self._get("/api/trade-history", params)

    def get_trade_history_full(self, code, start_date=None, end_date=None, limit=0,
                               before=None, include_today=False):
        """
        获取完整历史分时成交（自动按交易日遍历）。
        start_date/end_date: 缺省 start_date=前30天, end_date=今天
        before: 取该日期之前的成交（与 end_date 互斥）
        include_today: 是否包含今日实时成交
        返回: {"code","start_date","end_date","count","truncated","covered_dates","list"}
        """
        params = {"code": code}
        if start_date:
            params["start_date"] = start_date
        if end_date:
            params["end_date"] = end_date
        if before:
            params["before"] = before
        if include_today:
            params["include_today"] = "true"
        if limit > 0:
            params["limit"] = limit
        return self._get("/api/trade-history/full", params, timeout=TIMEOUT_LONG)

    def get_minute_trade_all(self, code, date=None):
        """
        获取全天分时成交（不分页）。
        date: 可选，缺省 = 今日
        返回: {"Count": int, "List": [...]}
        """
        params = {"code": code}
        if date:
            params["date"] = date
        return self._get("/api/minute-trade-all", params)

    # ═══════════════════════════════════════════════════════════
    # 5. 搜索 & 基本信息
    # ═══════════════════════════════════════════════════════════

    def search(self, keyword):
        """
        搜索股票（代码/名称模糊匹配，最多返回 50 条）。
        keyword: "平安" / "000001"
        返回: [{"code","name","exchange"}, ...]
        """
        return self._get("/api/search", {"keyword": keyword})

    def get_stock_info(self, code):
        """
        获取股票综合信息（行情 + 近30天日K + 今日分时）。
        返回: {"quote":{...}, "kline_day":{...}, "minute":{...}}
        """
        return self._get("/api/stock-info", {"code": code})

    # ═══════════════════════════════════════════════════════════
    # 6. 代码列表
    # ═══════════════════════════════════════════════════════════

    def get_all_codes(self, exchange="all"):
        """
        获取股票代码列表（按交易所筛选，含名称）。
        exchange: "sh" | "sz" | "bj" | "all"
        返回: {"total": int, "exchanges": {"sh":n,"sz":n,"bj":n}, "codes":[...]}
        """
        return self._get("/api/codes", {"exchange": exchange})

    def get_stock_codes(self, limit=0, with_prefix=True):
        """
        获取纯股票代码列表（不含名称）。
        limit: >0 截取
        with_prefix: 是否包含 sh/sz/bj 前缀
        返回: {"count": int, "list": ["sh000001", ...]}
        """
        params = {}
        if limit > 0:
            params["limit"] = limit
        if not with_prefix:
            params["prefix"] = "false"
        return self._get("/api/stock-codes", params)

    def get_etf_codes(self, limit=0, with_prefix=True):
        """
        获取 ETF 代码列表。
        返回: {"count": int, "list": ["sh510050", ...]}
        """
        params = {}
        if limit > 0:
            params["limit"] = limit
        if not with_prefix:
            params["prefix"] = "false"
        return self._get("/api/etf-codes", params)

    def get_etf_list(self, exchange=None, limit=None):
        """
        获取 ETF 列表（含名称和最新价）。
        exchange: "sh"|"sz"|"all"，缺省 = 全部
        返回: {"total": int, "list": [{"code","name","exchange","last_price"},...]}
        """
        params = {}
        if exchange:
            params["exchange"] = exchange
        if limit:
            params["limit"] = limit
        return self._get("/api/etf", params)

    # ═══════════════════════════════════════════════════════════
    # 7. 除权除息 & 股本变迁 (gbbq)
    # ═══════════════════════════════════════════════════════════

    def refresh_gbbq(self, codes=None):
        """
        主动刷新 gbbq 缓存。⚠️ 同步阻塞！
        codes: None → 全量（11000+ 只，约 9-15 分钟，需客户端 -m 900）
               ["sh600000", ...] → 单只/批量秒级
        返回: {"success_count": int, "failed_count": int, "failed": {...}, "duration_ms": int}
        """
        body = {}
        if codes:
            body["codes"] = codes
        return self._post("/api/gbbq/refresh", body, timeout=TIMEOUT_GBBQ)

    def get_gbbq(self, code, start_date=None, end_date=None):
        """
        获取个股股本变迁/除权除息记录。
        注意: 必须先用 refresh_gbbq([code]) 拉取该股票数据。
        返回: {
            "code": "sh600000",
            "equity": [{"date","category","float","total"}, ...],   # 股本变迁
            "xrxd":   [{"date","fenhong","peigujia","songzhuangu","peigu"}, ...]  # 除权除息
        }
        """
        params = {"code": code}
        if start_date:
            params["start_date"] = start_date
        if end_date:
            params["end_date"] = end_date
        return self._get("/api/gbbq", params)

    def get_turnover(self, code, start_date=None, end_date=None):
        """
        获取个股换手率序列（%）。
        换手率 = 成交量(手)×100 / 流通股本 ×100
        依赖 gbbq 缓存，缓存空时 turnover=0。
        返回: {"code": str, "count": int, "list": [{"date","turnover","float"}, ...]}
        """
        params = {"code": code}
        if start_date:
            params["start_date"] = start_date
        if end_date:
            params["end_date"] = end_date
        return self._get("/api/turnover", params)

    # ═══════════════════════════════════════════════════════════
    # 8. 市场统计
    # ═══════════════════════════════════════════════════════════

    def get_market_stats(self):
        """
        获取各交易所涨跌家数统计。
        返回: {
            "sh": {"total","up","down","flat"},
            "sz": {...},
            "bj": {...},
            "update_time": str
        }
        """
        return self._get("/api/market-stats")

    def get_market_count(self):
        """
        获取各交易所证券数量。
        返回: {"total": int, "exchanges": [{"exchange":"sh","count":n}, ...]}
        """
        return self._get("/api/market-count")

    def get_market_snapshot(self):
        """
        获取全市场 5300+ 只股票当日 OHLCV 断面。⚠️ 同步阻塞 4-15 分钟！
        建议: 每天 16:00 后调用，避开 15:00 数据回填期。
        返回: {"date":str, "count":int, "list":[{code,open,high,low,close,volume,last_close,change_pct}]}
        """
        return self._get("/api/market-snapshot", timeout=TIMEOUT_SNAPSHOT)

    # ═══════════════════════════════════════════════════════════
    # 9. 交易日 & 收益计算
    # ═══════════════════════════════════════════════════════════

    def get_workday(self, date=None, count=1):
        """
        查询某日是否为交易日，并获取前后交易日。
        count: 前后各取 count 个交易日（最大 30）
        返回: {
            "date": {"iso","numeric"},
            "is_workday": bool,
            "next": [...],      # 之后 count 个交易日
            "previous": [...]   # 之前 count 个交易日
        }
        """
        params = {}
        if date:
            params["date"] = date
        if count > 1:
            params["count"] = count
        return self._get("/api/workday", params)

    def get_workday_range(self, start, end):
        """
        获取指定日期范围内的交易日列表。
        start/end: "20260101" 或 "2026-01-01"
        返回: {"count": int, "list": [{"iso","numeric"}, ...]}
        """
        params = {"start": start, "end": end}
        return self._get("/api/workday/range", params)

    def get_income(self, code, start_date, days=""):
        """
        计算相对指定买入日的 N 日收益率。
        start_date: "2025-01-15" 买入日
        days: "5,10,20,60,120" 逗号分隔，缺省默认使用 [5,10,20,60,120]
        返回: {"count": int, "list": [{"offset","time","rise","rise_rate","source","current"}, ...]}
        """
        params = {"code": code, "start_date": start_date}
        if days:
            params["days"] = days
        return self._get("/api/income", params)

    # ═══════════════════════════════════════════════════════════
    # 10. 任务 & 服务器状态
    # ═══════════════════════════════════════════════════════════

    def create_pull_kline_task(self, codes=None, tables=None, limit=None,
                               start_date=None, directory=None):
        """
        创建 K 线批量入库任务。
        ⚠️ 写入 tdx-api 本地 SQLite (./data/database/kline/{code}.db)。
        不是你自己的外部数据库！
        如需自己写 MySQL → 用 get_kline_all_tdx/get_kline_all_ths 拉取后自行入库。

        codes: 股票代码列表，缺省 = 全市场
        tables: K线类型 ["day","minute","5minute","15minute","30minute","hour",
                         "week","month","quarter","year"]，缺省 = ["day"]
        limit: 并发协程数，缺省 = 1
        start_date: "2020-01-01"，缺省 = 上市首日
        返回: task_id (str)
        """
        body = {}
        if codes:
            body["codes"] = codes
        if tables:
            body["tables"] = tables
        if limit:
            body["limit"] = limit
        if start_date:
            body["start_date"] = start_date
        if directory:
            body["dir"] = directory
        return self._post("/api/tasks/pull-kline", body)

    def create_pull_trade_task(self, code, start_year=None, end_year=None, directory=None):
        """
        创建分时成交入库任务。
        ⚠️ 写入 tdx-api 本地 SQLite (./data/database/trade/{code}.db)。
        start_year/end_year: 年份范围，缺省 = 全部
        返回: task_id (str)
        """
        body = {"code": code}
        if start_year:
            body["start_year"] = start_year
        if end_year:
            body["end_year"] = end_year
        if directory:
            body["dir"] = directory
        return self._post("/api/tasks/pull-trade", body)

    def list_tasks(self):
        """
        列出所有异步任务。
        返回: [{"id","type","status","started_at","ended_at","error"}, ...]
        """
        return self._get("/api/tasks")

    def get_task(self, task_id):
        """
        查询任务详情 / 状态。
        返回: {"id","type","status","started_at","ended_at","error"}
        """
        return self._get(f"/api/tasks/{task_id}")

    def cancel_task(self, task_id):
        """
        取消正在运行的任务。
        返回: {"id","type","status","started_at","ended_at"}
        """
        return requests.delete(
            f"{self.base_url}/api/tasks/{task_id}", timeout=TIMEOUT_DEFAULT
        ).json()["data"]

    # ═══════════════════════════════════════════════════════════
    # 11. 服务器状态
    # ═══════════════════════════════════════════════════════════

    def get_server_status(self):
        """
        获取服务器基本状态。
        返回: {"status","connected","version","uptime"}
        """
        return self._get("/api/server-status")

    def health_check(self):
        """
        健康检查（进程指标）。
        返回: {"status","time","uptime_seconds","gbbq_cache_size","goroutines","memory_mb"}
        """
        return self._get("/api/health")

    def ready_check(self):
        """
        就绪检查（服务能否接收请求）。
        返回: {"ready": bool, "uptime_seconds": int}
        """
        return self._get("/api/ready")


# ═══════════════════════════════════════════════════════════════
# 演示运行
# ═══════════════════════════════════════════════════════════════

def color(text, code="36"):
    """简单 ANSI 着色。"""
    return f"\033[{code}m{text}\033[0m"


def run_examples(mode="full"):
    """运行示例。mode: full | quick | slow"""
    api = StockAPI()

    # ── 公共变量 ──
    STOCK = "000001"           # 平安银行
    INDEX = "sh000001"         # 上证指数
    STOCK_FULL = "sh600000"    # 浦发银行（用于 gbbq）

    examples = []
    skipped = []

    def add(name, fn, *args, **kw):
        examples.append((name, fn, args, kw))

    # 1. 实时行情
    add("五档行情", api.get_quote, STOCK)
    add("批量行情", api.batch_get_quote, [STOCK, "600000"])

    # 2. K线
    add("日K线(前复权)", api.get_kline, STOCK, "day")
    add("分钟K线(不复权)", api.get_kline, STOCK, "minute5")
    add("全量日K(TDX不复权)", api.get_kline_all_tdx, STOCK, "day", 5)
    add("全量日K(同花顺前复权)", api.get_kline_all_ths, STOCK, "day", 5)
    add("历史K线(THS前复权)", api.get_kline_history, STOCK, "day",
        "2025-12-01", "2025-12-31")
    add("历史K线(TDX不复权)", api.get_kline_history_tdx, STOCK, "day",
        "2025-12-01", "2025-12-31")

    # 3. 指数
    add("指数日K", api.get_index, INDEX, "day", 5)
    add("指数全量K", api.get_index_all, INDEX, "day", 5)
    add("指数历史日K", api.get_kline_index_history, INDEX, "2025-12-01", "2025-12-31")

    # 4. 分时 & 成交
    add("分时数据", api.get_minute, STOCK)
    add("分时成交", api.get_trade, STOCK)
    add("全天分时成交", api.get_minute_trade_all, STOCK)

    # 5. 搜索 & 信息
    add("搜索股票", api.search, "平安")
    add("股票综合信息", api.get_stock_info, STOCK)

    # 6. 代码列表
    add("代码列表(sh)", api.get_all_codes, "sh")
    add("纯股票代码(前10)", api.get_stock_codes, 10)
    add("ETF列表(前5)", api.get_etf_list, limit=5)

    # 7. gbbq
    add("单只gbbq刷新", api.refresh_gbbq, [STOCK_FULL])
    add("查询gbbq记录", api.get_gbbq, STOCK_FULL)
    add("换手率序列", api.get_turnover, STOCK_FULL, "2025-11-01", "2025-12-31")

    # 8. 市场统计
    add("市场统计", api.get_market_stats)
    add("市场数量", api.get_market_count)

    # 9. 交易日 & 收益
    add("交易日查询", api.get_workday, "2026-06-03", 3)
    add("交易日范围", api.get_workday_range, "2025-12-01", "2025-12-31")
    add("收益率计算", api.get_income, STOCK, "2025-09-01", "5,10,20")

    # 10. 服务器
    add("服务器状态", api.get_server_status)
    add("健康检查", api.health_check)
    add("就绪检查", api.ready_check)

    if mode == "slow":
        add("全市场断面(⚠️ 4-15min)", api.get_market_snapshot)
        add("全量gbbq刷新(⚠️ 9-15min)", api.refresh_gbbq)
    else:
        skipped.append("全市场断面 GET /api/market-snapshot （加 --slow 才跑）")
        skipped.append("全量gbbq刷新 POST /api/gbbq/refresh （加 --slow 才跑）")

    if mode == "quick":
        examples = examples[:10]  # 只跑前 10 个
        skipped.append("… 其余 30+ 个端点（加 --full 跑全部）")

    # ── 执行 ──
    print(color("=" * 65, "1;36"))
    print(color("  TDX API 使用示例", "1;36"))
    print(color(f"  服务器: {BASE_URL}  模式: {mode}", "36"))
    print(color("=" * 65, "1;36"))

    ok = fail = 0
    for i, (name, fn, args, kw) in enumerate(examples):
        tag = f"[{i+1}/{len(examples)}]"
        try:
            data = fn(*args, **kw)
            label = color(f"✓ {tag} {name}", "32")
            print(f"{label}")

            # 截断展示
            if isinstance(data, dict):
                keys = list(data.keys())[:5]
                preview = f"  keys: {keys}"
                if "count" in data:
                    preview += f", count={data['count']}"
                elif "Count" in data:
                    preview += f", Count={data['Count']}"
                elif "total" in data:
                    preview += f", total={data['total']}"
                print(color(preview, "90"))
            elif isinstance(data, list):
                n = len(data)
                preview = f"  {n} items"
                if n > 0 and isinstance(data[0], dict):
                    preview += f", fields={list(data[0].keys())[:5]}"
                print(color(preview, "90"))
            ok += 1
        except Exception as e:
            label = color(f"✗ {tag} {name}", "31")
            print(f"{label}")
            print(color(f"  {type(e).__name__}: {e}", "31"))
            fail += 1

    if skipped:
        print()
        print(color("已跳过:", "33"))
        for s in skipped:
            print(color(f"  · {s}", "90"))

    print()
    print(color(f"结果: {ok} 通过, {fail} 失败", "1;36" if fail == 0 else "1;33"))

    return fail == 0


# ═══════════════════════════════════════════════════════════════
# 快速参考 — 常用场景组合
# ═══════════════════════════════════════════════════════════════

def print_cheatsheet():
    """打印速查表。"""
    print(color("""
╔══════════════════════════════════════════════════════════════╗
║                    常用场景速查表                              ║
╠══════════════════════════════════════════════════════════════╣
║                                                              ║
║  📊 我要实时行情                                              ║
║     api.get_quote("000001")                                  ║
║     api.batch_get_quote(["000001","600000"])  # ≤50只         ║
║                                                              ║
║  📈 我要日K线                                                 ║
║     看最近100天 (前复权)  → api.get_kline("000001")            ║
║     全量历史 (前复权)     → api.get_kline_all_ths("000001")    ║
║     全量历史 (不复权)     → api.get_kline_all_tdx("000001")    ║
║     指定日期 (前复权)     → api.get_kline_history("000001")    ║
║     指定日期 (不复权)     → api.get_kline_history_tdx("000001")║
║                                                              ║
║  🗄️  我要自己算复权                                            ║
║     ① api.refresh_gbbq(["sh600000"])                         ║
║     ② api.get_gbbq("600000")        # 除权除息记录            ║
║     ③ api.get_kline_all_tdx("600000")  # 原始不复权K线        ║
║     ④ 客户端用 xrxd 记录写复权算法                             ║
║                                                              ║
║  📋 我要全市场代码                                             ║
║     股票 → api.get_stock_codes()       # ~5300+ 只            ║
║     ETF  → api.get_etf_codes()                               ║
║     含名 → api.get_all_codes("all")                          ║
║                                                              ║
║  🕐 我要交易日历                                               ║
║     api.get_workday("2026-06-03")                            ║
║     api.get_workday_range("2025-01-01", "2025-12-31")        ║
║                                                              ║
║  🔄 我要写自己的数据库                                          ║
║     日K → api.get_kline_all_tdx("000001")    # 不复权         ║
║     日K → api.get_kline_all_ths("000001")    # 前复权         ║
║     分钟→ api.get_kline_all_tdx("000001","minute5")          ║
║     日切→ api.get_market_snapshot()          # 全市场断面     ║
║     ⚠️ 不要用 api.create_pull_kline_task()                    ║
║        （那是 tdx-api 自己的内部 SQLite 缓存）                  ║
║                                                              ║
║  🏥 我要监控服务                                               ║
║     api.health_check()   # docker healthcheck                ║
║     api.ready_check()    # k8s readiness probe               ║
║                                                              ║
║  ⚠️  耗时端点（客户端要设大超时）                                 ║
║     api.refresh_gbbq()        # 全量 9-15min, timeout=600    ║
║     api.get_market_snapshot() # 4-15min, timeout=900         ║
║                                                              ║
╚══════════════════════════════════════════════════════════════╝
""", "36"))


# ═══════════════════════════════════════════════════════════════
# 入口
# ═══════════════════════════════════════════════════════════════

if __name__ == "__main__":
    mode = "full"
    if "--slow" in sys.argv:
        mode = "slow"
    elif "--quick" in sys.argv:
        mode = "quick"
    elif "--cheatsheet" in sys.argv:
        print_cheatsheet()
        sys.exit(0)
    elif "--help" in sys.argv or "-h" in sys.argv:
        print(__doc__)
        print("额外参数:")
        print("  --quick      只跑核心 10 个端点")
        print("  --slow       包含 market-snapshot / gbbq-full 等耗时端点")
        print("  --cheatsheet 打印常用场景速查表")
        sys.exit(0)

    print_cheatsheet()
    success = run_examples(mode)
    sys.exit(0 if success else 1)
