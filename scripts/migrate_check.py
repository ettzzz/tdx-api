#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
迁移验证脚本 (M4): 上游对齐后的 rc 容器 vs 生产 v1.4.0 容器

用法:
  python3 scripts/migrate_check.py smoke  <base>              # 冒烟: 各端点 code==0 + 耗时
  python3 scripts/migrate_check.py diff   <old> <new>         # 结构对比: 关键端点响应形状
  python3 scripts/migrate_check.py special <old> <new>        # 正确性专项 5 项

纯标准库, 不依赖 requests。
代码格式统一用 sh600000 (新旧容器都接受); 新容器额外兼容 600000.SH 点后缀。
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


def data_of(resp):
    if not isinstance(resp, dict):
        return None
    return resp.get("data")


def dget(d, *keys, default=None):
    """大小写不敏感取键: K线类字段新旧 JSON 大小写不同"""
    for k in keys:
        if isinstance(d, dict) and k in d:
            return d[k]
    lk = {str(k).lower(): v for k, v in d.items()} if isinstance(d, dict) else {}
    for k in keys:
        if k.lower() in lk:
            return lk[k.lower()]
    return default


def shape(v, depth=0):
    if depth > 3:
        return type(v).__name__
    if isinstance(v, dict):
        return {k: shape(x, depth + 1) for k, x in sorted(v.items())}
    if isinstance(v, list):
        if not v:
            return "list[]"
        return f"list[{shape(v[0], depth + 1)}]"
    return type(v).__name__


def smoke(base):
    endpoints = [
        "/api/quote?code=sh600000",
        "/api/kline?code=sh600000&type=day",
        "/api/kline?code=sh600000&type=minute1",
        "/api/kline-history-tdx?code=sh600000&type=day&start_date=2025-01-01",
        "/api/kline-history-tdx?code=sz000001&type=week",
        "/api/kline-history-ths?code=sh600000&type=day&start_date=2025-01-01",
        "/api/kline-history-qfq?code=sh600000&start_date=2025-01-01",
        "/api/minute?code=sh600000",
        "/api/trade?code=sh600000",
        "/api/codes?exchange=sh&limit=10",
        "/api/codes?exchange=bj&limit=10",
        "/api/stock-codes?limit=10",
        "/api/etf-codes?limit=10",
        "/api/etf?limit=10",
        "/api/index?code=sh000001&type=day&limit=10",
        "/api/kline-index-history?code=sh000001&start_date=2025-01-01",
        "/api/market-count",
        "/api/market-stats",
        "/api/gbbq?code=sh600000",
        "/api/turnover?code=sh600000&start_date=2025-01-01",
        "/api/income?code=sh600000&start_date=2025-01-01",
        "/api/workday?date=2026-09-18",
        "/api/workday/range?start=2026-09-01&end=2026-09-19",
        "/api/trade-history?code=sh600000&date=2026-09-18",
        "/api/minute-trade-all?code=sh600000",
        "/api/kline-all?code=sh600000&type=day&limit=50",
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
    print("POST /api/batch-quote ...")
    resp, dt, err = post(base, "/api/batch-quote", {"codes": ["sh600000", "sz000001", "sh510300"]})
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


def diff(old, new):
    eps = [
        "/api/quote?code=sh600000",
        "/api/kline-history-tdx?code=sh600000&type=day&start_date=2025-06-01",
        "/api/kline-history-ths?code=sh600000&type=day&start_date=2025-06-01",
        "/api/kline?code=sh600000&type=day",
        "/api/codes?exchange=sh&limit=5",
        "/api/gbbq?code=sh600000",
        "/api/index?code=sh000001&type=day&limit=10",
        "/api/trade-history?code=sh600000&date=2026-09-18",
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
    print(f"[diff] {len(eps) - bad}/{len(eps)} SAME (预期 DIFF: quote K->Kline, K线新增 Order 字段)")
    return 0 if bad == 0 else 2


def closes_of(resp):
    """K线列表 -> {日期: 收盘价(厘)}"""
    data = data_of(resp) or {}
    out = {}
    for k in dget(data, "list", default=[]) or []:
        t = dget(k, "time", "date")
        c = dget(k, "close")
        if t is not None and c is not None:
            try:
                out[str(t)[:10]] = float(c)
            except (TypeError, ValueError):
                pass
    return out


def volumes_of(resp):
    """K线列表 -> {日期时间: 成交量}"""
    data = data_of(resp) or {}
    out = {}
    for k in dget(data, "list", default=[]) or []:
        t = dget(k, "time", "date")
        v = dget(k, "volume")
        if t is not None and v is not None:
            try:
                out[str(t)] = int(v)
            except (TypeError, ValueError):
                pass
    return out


def cmp_series(a_map, b_map, label, rel_tol):
    common = sorted(set(a_map) & set(b_map))
    if not common:
        print(f"[{label}] 无共同日期可比对 (a={len(a_map)} b={len(b_map)})")
        return None
    diffs = []
    for t in common:
        a, b = a_map[t], b_map[t]
        if a == b:
            continue
        rel = abs(a - b) / abs(a) if a else float("inf")
        if rel > rel_tol:
            diffs.append((t, a, b, rel))
    max_rel = max((d[3] for d in diffs), default=0.0)
    print(f"[{label}] 共同日期 {len(common)}, 超容差差异 {len(diffs)} (容差 {rel_tol:.2%}), 最大偏差 {max_rel:.4%}")
    for t, a, b, rel in diffs[:5]:
        print(f"    {t}: a={a} b={b} rel={rel:.4%}")
    return diffs


def special(old, new):
    results = {}

    # ---- 专项1: 低成交量 K 线 volume (量额浮点解码修复) ----
    # sh511990 华宝添益 ETF: 分钟成交量极小, 旧解码在低量时放大 ~32768 倍
    o, _, e1 = get(old, "/api/kline-history-tdx?code=sh511990&type=minute5&start_date=2026-09-01")
    n, _, e2 = get(new, "/api/kline-history-tdx?code=sh511990&type=minute5&start_date=2026-09-01")
    if e1 is None and e2 is None:
        vo, vn = volumes_of(o), volumes_of(n)
        common = sorted(set(vo) & set(vn))
        changed = [(t, vo[t], vn[t]) for t in common if vo[t] != vn[t]]
        absurd = lambda seq: [v for v in seq if v > 10**9]  # noqa: E731  1e9 手显然荒谬(全A一天约几亿手)
        ao, an = absurd(vo.values()), absurd(vn.values())
        print(f"[专项1 低量K线] 共同分钟 {len(common)}, 量值变化 {len(changed)}, >1e9手: 旧 {len(ao)} / 新 {len(an)}")
        for t, a, b in changed[:5]:
            print(f"    {t}: old={a} new={b}")
        results["专项1"] = "OK" if not an else "CHECK"
    else:
        print(f"[专项1 低量K线] 拉取失败 old={e1} new={e2}")
        results["专项1"] = "FAIL"

    # ---- 专项2: qfq(仿射) vs ths(同花顺前复权) 交叉验证 (都在新容器上) ----
    # 注: 老容器 gbbq 缓存为空, qfq 无法在旧容器上出数, 只能新容器内部交叉验证
    n_qfq, _, e1 = get(new, "/api/kline-history-qfq?code=sh600000&start_date=2024-01-01")
    n_ths, _, e2 = get(new, "/api/kline-history-ths?code=sh600000&start_date=2024-01-01")
    if e1 is None and e2 is None:
        d = cmp_series(closes_of(n_qfq), closes_of(n_ths), "专项2 qfq(新,仿射)-vs-ths(新)", 0.005)
        print("    判定: 仿射前复权与同花顺前复权应在 0.5% 内一致")
        results["专项2"] = "OK" if (d is not None and not d) else "CHECK"
    else:
        print(f"[专项2] 拉取失败 qfq={e1} ths={e2}")
        results["专项2"] = "FAIL"

    # ---- 专项3: 可转债分钟成交量 (不再错误除以100) ----
    code = "sh110092"  # 南银转债
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

    # ---- 专项4: ETF 行情 (新旧都取收盘价, 应一致) ----
    o, _, e1 = get(old, "/api/quote?code=sh510300,sh511990,sz159915")
    n, _, e2 = get(new, "/api/quote?code=sh510300,sh511990,sz159915")
    if e1 is None and e2 is None:
        lo = data_of(o) or []
        ln = data_of(n) or []

        def price_of(q):
            k = dget(q, "kline", "K")  # 旧 K / 新 Kline
            return dget(k, "close") if k else None

        po = [price_of(q) for q in (lo if isinstance(lo, list) else [])]
        pn = [price_of(q) for q in (ln if isinstance(ln, list) else [])]
        ok = len(po) == len(pn) and po and all(
            a is not None and b is not None and abs(a - b) / a < 0.01 for a, b in zip(po, pn)
        )
        print(f"[专项4 ETF行情] old closes={po} new closes={pn}")
        results["专项4"] = "OK" if ok else "CHECK"
    else:
        print(f"[专项4] 拉取失败 old={e1} new={e2}")
        results["专项4"] = "FAIL"

    # ---- 专项5: 北交所代码 (官网爬虫 345 -> tdxbjmore.cfg ~349, 均为 300-400 区间) ----
    o, _, e1 = get(old, "/api/codes?exchange=bj")
    n, _, e2 = get(new, "/api/codes?exchange=bj")
    if e1 is None and e2 is None:
        co = dget(data_of(o) or {}, "total", default=0)
        cn = dget(data_of(n) or {}, "total", default=0)
        print(f"[专项5 北交所代码] old total={co}, new total={cn} (tdxbjmore.cfg 通道)")
        results["专项5"] = "OK" if 300 <= cn <= 400 else "CHECK"
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
