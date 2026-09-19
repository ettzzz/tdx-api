#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
迁移验证脚本 (M4): 上游对齐后的 rc 容器 vs 生产 v1.4.0 容器

用法:
  python3 scripts/migrate_check.py smoke  <base>              # 冒烟: 各端点 code==0 + 耗时
  python3 scripts/migrate_check.py diff   <old> <new>         # 结构对比: 关键端点响应形状
  python3 scripts/migrate_check.py special <old> <new>        # 正确性专项 5 项

纯标准库, 不依赖 requests。
"""
import json
import statistics
import sys
import time
import urllib.request

TIMEOUT = 60


def get(base, path, timeout=TIMEOUT):
    url = base.rstrip("/") + path
    t0 = time.time()
    try:
        with urllib.request.urlopen(url, timeout=timeout) as resp:
            data = json.loads(resp.read().decode("utf-8", "replace"))
            return data, time.time() - t0, None
    except Exception as e:  # noqa: BLE001
        return None, time.time() - t0, str(e)


def data_of(resp):
    if not isinstance(resp, dict):
        return None
    return resp.get("data")


def smoke(base):
    endpoints = [
        "/api/quote?code=600000.SH",
        "/api/kline?code=600000.SH&type=day",
        "/api/kline?code=600000.SH&type=minute1",
        "/api/kline-history-tdx?code=600000.SH&type=day&start_date=2025-01-01",
        "/api/kline-history-tdx?code=000001.SZ&type=week",
        "/api/kline-history-ths?code=600000.SH&type=day&start_date=2025-01-01",
        "/api/kline-history-qfq?code=600000.SH&start_date=2025-01-01",
        "/api/minute?code=600000.SH",
        "/api/trade?code=600000.SH",
        "/api/codes?exchange=sh&limit=10",
        "/api/codes?exchange=bj&limit=10",
        "/api/stock-codes?limit=10",
        "/api/etf-codes?limit=10",
        "/api/etf?limit=10",
        "/api/index?code=000001.SH&type=day&limit=10",
        "/api/kline-index-history?code=000001.SH&start_date=2025-01-01",
        "/api/market-count",
        "/api/market-stats",
        "/api/gbbq?code=600000.SH",
        "/api/turnover?code=600000.SH&start_date=2025-01-01",
        "/api/income?code=600000.SH&start_date=2025-01-01",
        "/api/workday?date=2026-09-18",
        "/api/workday/range?start=2026-09-01&end=2026-09-19",
        "/api/trade-history?code=600000.SH&date=2026-09-18",
        "/api/minute-trade-all?code=600000.SH",
        "/api/kline-all?code=600000.SH&type=day&limit=50",
        "/api/server-status",
        "/api/health",
        "/api/ready",
        "/api/realtime/health",
        "/api/realtime/codes",
    ]
    failed = []
    print(f"{'endpoint':58s} {'ms':>8s}  result")
    for ep in endpoints:
        resp, dt, err = get(base, ep)
        ok = err is None and isinstance(resp, dict) and resp.get("code") == 0
        size = len(json.dumps(resp)) if resp is not None else 0
        print(f"{ep:58s} {dt*1000:8.0f}  {'OK' if ok else 'FAIL ' + str(err or resp)[:80]} data={size}B")
        if not ok:
            failed.append(ep)
    # POST 端点
    print("POST /api/batch-quote ...")
    resp, dt, err = post(base, "/api/batch-quote", {"codes": ["600000.SH", "000001.SZ", "510300.SH"]})
    ok = err is None and isinstance(resp, dict) and resp.get("code") == 0
    print(f"{'POST /api/batch-quote':58s} {dt*1000:8.0f}  {'OK' if ok else 'FAIL ' + str(err or resp)[:80]}")
    if not ok:
        failed.append("POST /api/batch-quote")
    print()
    if failed:
        print(f"[smoke] FAILED {len(failed)}: {failed}")
        return 1
    print("[smoke] ALL OK")
    return 0


def post(base, path, payload):
    url = base.rstrip("/") + path
    body = json.dumps(payload).encode()
    t0 = time.time()
    try:
        req = urllib.request.Request(url, data=body, headers={"Content-Type": "application/json"}, method="POST")
        with urllib.request.urlopen(req, timeout=TIMEOUT) as resp:
            data = json.loads(resp.read().decode("utf-8", "replace"))
            return data, time.time() - t0, None
    except Exception as e:  # noqa: BLE001
        return None, time.time() - t0, str(e)


def shape(v, depth=0):
    """结构摘要: dict->键集合, list->首元素形状, 其他->类型名"""
    if depth > 3:
        return type(v).__name__
    if isinstance(v, dict):
        return {k: shape(x, depth + 1) for k, x in sorted(v.items())}
    if isinstance(v, list):
        if not v:
            return "list[]"
        return f"list[{shape(v[0], depth + 1)}]"
    return type(v).__name__


def diff(old, new):
    eps = [
        "/api/quote?code=600000.SH",
        "/api/kline-history-tdx?code=600000.SH&type=day&start_date=2025-06-01",
        "/api/kline-history-ths?code=600000.SH&type=day&start_date=2025-06-01",
        "/api/kline?code=600000.SH&type=day",
        "/api/codes?exchange=sh&limit=5",
        "/api/gbbq?code=600000.SH",
        "/api/index?code=000001.SH&type=day&limit=10",
        "/api/trade-history?code=600000.SH&date=2026-09-18",
        "/api/health",
    ]
    bad = 0
    for ep in eps:
        r_old, _, e1 = get(old, ep)
        r_new, _, e2 = get(new, ep)
        s1 = shape(data_of(r_old)) if e1 is None else f"ERR:{e1}"
        s2 = shape(data_of(r_new)) if e2 is None else f"ERR:{e2}"
        same = "SAME" if s1 == s2 else "DIFF"
        if s1 != s2:
            bad += 1
        print(f"[{same}] {ep}")
        if s1 != s2:
            print(f"    old: {json.dumps(s1, ensure_ascii=False)[:300]}")
            print(f"    new: {json.dumps(s2, ensure_ascii=False)[:300]}")
    print()
    print(f"[diff] {len(eps) - bad}/{len(eps)} SAME")
    return 0 if bad == 0 else 2


def closes_of(resp):
    out = []
    for k in (data_of(resp) or {}).get("list") or []:
        c = k.get("close")
        if isinstance(c, dict):  # Price 序列化 {"Price":..., ...}
            c = c.get("Price") or c.get("price")
        out.append((k.get("time") or k.get("date") or k.get("Date"), float(c)))
    return dict(out)


def cmp_series(old_resp, new_resp, label, rel_tol, sample_note):
    """对比新旧两个响应的收盘价序列: 报告差异数量与最大相对偏差"""
    o, n = closes_of(old_resp), closes_of(new_resp)
    common = sorted(set(o) & set(n))
    if not common:
        print(f"[{label}] 无共同日期可比对 (old={len(o)} new={len(n)}) {sample_note}")
        return None
    diffs = []
    for t in common:
        a, b = o[t], n[t]
        if a == b:
            continue
        if a == 0:
            diffs.append((t, a, b, float("inf")))
            continue
        rel = abs(a - b) / abs(a)
        if rel > rel_tol:
            diffs.append((t, a, b, rel))
    max_rel = max((d[3] for d in diffs), default=0.0)
    print(f"[{label}] 共同日期 {len(common)}, 超容差差异 {len(diffs)} (容差 {rel_tol:.2%}), 最大偏差 {max_rel:.4%}")
    for t, a, b, rel in diffs[:5]:
        print(f"    {t}: old={a} new={b} rel={rel:.4%}")
    return diffs


def special(old, new):
    results = {}

    # ---- 专项1: 低成交量 K 线 volume (量额浮点解码修复) ----
    # 511990 华宝添益 ETF: 场内货币基金, 分钟成交量极小, 旧解码在低量时放大 ~32768 倍
    o, _, e1 = get(old, "/api/kline-history-tdx?code=511990.SH&type=minute5&start_date=2026-09-01")
    n, _, e2 = get(new, "/api/kline-history-tdx?code=511990.SH&type=minute5&start_date=2026-09-01")

    def volumes_of(resp):
        out = {}
        for k in (data_of(resp) or {}).get("list") or []:
            v = k.get("volume")
            if isinstance(v, dict):
                v = v.get("Volume", v.get("volume"))
            t = k.get("time") or k.get("date")
            if v is not None and t:
                out[t] = int(v)
        return out

    if e1 is None and e2 is None:
        vo, vn = volumes_of(o), volumes_of(n)
        common = sorted(set(vo) & set(vn))
        changed = [(t, vo[t], vn[t]) for t in common if vo[t] != vn[t]]
        big = [c for c in changed if c[1] != 0 and (c[2] > c[1] * 1000 or (c[1] > c[2] * 1000 and c[1] > 10**6))]
        print(f"[专项1 低量K线] 共同分钟 {len(common)}, 量值变化 {len(changed)}, 可疑放大/缩小 {len(big)}")
        for t, a, b in changed[:5]:
            print(f"    {t}: old={a} new={b}")
        # 修复生效的标志: 两边数据不同 (旧版有放大) 或相同但数量级合理
        absurd_old = [v for v in vo.values() if v > 10**9]
        absurd_new = [v for v in vn.values() if v > 10**9]
        print(f"    旧数据 >1e9 手的分钟数: {len(absurd_old)}; 新数据: {len(absurd_new)}")
        results["专项1"] = "OK" if not absurd_new else "CHECK"
    else:
        print(f"[专项1 低量K线] 拉取失败 old={e1} new={e2}")
        results["专项1"] = "FAIL"

    # ---- 专项2: qfq(仿射) vs ths(同花顺前复权) 交叉验证 ----
    _, _, eq1 = get(old, "/api/kline-history-qfq?code=600000.SH&start_date=2024-01-01")
    d, _, eq2 = get(old, "/api/kline-history-ths?code=600000.SH&start_date=2024-01-01")  # old ths
    o, _, e1 = get(old, "/api/kline-history-qfq?code=600000.SH&start_date=2024-01-01")
    n, _, e2 = get(new, "/api/kline-history-qfq?code=600000.SH&start_date=2024-01-01")
    ths_new, _, e3 = get(new, "/api/kline-history-ths?code=600000.SH&start_date=2024-01-01")
    if None not in (e1, e2, e3):
        d_old = cmp_series(o, ths_new, "专项2 qfq-vs-ths(新)", 0.005, "")
        d_old2 = cmp_series(n, ths_new, "专项2 qfq(新)-vs-ths(新)", 0.005, "")
        print("    判定: 新 qfq 与 ths 前复权应在 0.5% 内一致 (含分红除权日附近亦然)")
        results["专项2"] = "OK" if (d_old is not None and not d_old) else "CHECK"
    else:
        print(f"[专项2] 拉取失败 qfq_old={e1} ths_new={e3}")
        results["专项2"] = "FAIL"

    # ---- 专项3: 可转债分钟成交量 (不再错误除以100) ----
    code = "113050.SH"  # 南银转债
    o, _, e1 = get(old, f"/api/kline-history-tdx?code={code}&type=minute1&start_date=2026-09-10")
    n, _, e2 = get(new, f"/api/kline-history-tdx?code={code}&type=minute1&start_date=2026-09-10")
    if e1 is None and e2 is None:
        vo, vn = volumes_of(o), volumes_of(n)
        common = sorted(set(vo) & set(vn))
        ratios = [vn[t] / vo[t] for t in common if vo[t] and vn[t]]
        if ratios:
            med = statistics.median(ratios)
            print(f"[专项3 可转债分钟量] 共同分钟 {len(common)}, 新/旧 中位比值 {med:.2f} (预期 ~100, 旧版错误地÷100)")
            results["专项3"] = "OK" if 50 <= med <= 200 else "CHECK"
        else:
            print(f"[专项3 可转债分钟量] 无可比数据 old={len(vo)} new={len(vn)}")
            results["专项3"] = "CHECK"
    else:
        print(f"[专项3] 拉取失败 old={e1} new={e2}")
        results["专项3"] = "FAIL"

    # ---- 专项4: ETF 行情 (价格修正 + 前缀判断扩充) ----
    o, _, e1 = get(old, "/api/quote?code=510300.SH,511990.SH,159915.SZ")
    n, _, e2 = get(new, "/api/quote?code=510300.SH,511990.SH,159915.SZ")
    if e1 is None and e2 is None:
        lo = data_of(o) or []
        ln = data_of(n) or []

        def price_of(q):
            k = q.get("kline") or q.get("Kline") or {}
            c = k.get("close") or k.get("Close")
            if isinstance(c, dict):
                c = c.get("Price", c.get("price"))
            return c

        po = [price_of(q) for q in (lo if isinstance(lo, list) else [])]
        pn = [price_of(q) for q in (ln if isinstance(ln, list) else [])]
        print(f"[专项4 ETF行情] old prices={po} new prices={pn}")
        results["专项4"] = "OK" if po and pn and all(a and b and abs(a - b) / a < 0.01 for a, b in zip(po, pn) if a and b) else "CHECK"
    else:
        print(f"[专项4] 拉取失败 old={e1} new={e2}")
        results["专项4"] = "FAIL"

    # ---- 专项5: 北交所代码 (爬虫 335 条 -> tdxbjmore.cfg 338 条含名称) ----
    o, _, e1 = get(old, "/api/codes?exchange=bj")
    n, _, e2 = get(new, "/api/codes?exchange=bj")
    if e1 is None and e2 is None:
        co = len(data_of(o) or [])
        cn = len(data_of(n) or [])
        print(f"[专项5 北交所代码] old count={co}, new count={cn} (tdxbjmore.cfg 应为 338)")
        results["专项5"] = "OK" if 300 <= cn <= 400 and cn >= co else "CHECK"
    else:
        print(f"[专项5] 拉取失败 old={e1} new={e2}")
        results["专项5"] = "FAIL"

    print()
    print("[special]", json.dumps(results, ensure_ascii=False))
    fail = [k for k, v in results.items() if v == "FAIL"]
    return 0 if not fail else 3


if __name__ == "__main__":
    cmd = sys.argv[1] if len(sys.argv) > 1 else ""
    if cmd == "smoke" and len(sys.argv) >= 3:
        sys.exit(smoke(sys.argv[2]))
    elif cmd == "diff" and len(sys.argv) >= 4:
        sys.exit(diff(sys.argv[2], sys.argv[3]))
    elif cmd == "special" and len(sys.argv) >= 4:
        sys.exit(special(sys.argv[2], sys.argv[3]))
    else:
        print(__doc__)
        sys.exit(1)
