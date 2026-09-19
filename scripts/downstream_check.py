#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
下游兼容性对比 (M4.5): 逐字段模拟 stock_scrapper TdxApiClient 的全部调用,
对比生产 v1.4.0 (8080) 与迁移 rc (8081) 的返回内容差异。

下游消费端点 (clients/tdx_api_client.py):
  1. GET  /api/ready                 -> {ready, uptime_seconds}
  2. GET  /api/stock-codes           -> {list: [sh600000, ...]}
  3. POST /api/gbbq/refresh {codes}  -> {success_count, failed_count, failed, duration_ms}
  4. GET  /api/gbbq?code=sh600000    -> {code, equity:[{date,float,total,category}], xrxd:[{date,fenhong,peigu,peigujia,songzhuangu}]}
  5. GET  /api/market-snapshot       -> {date, count, list:[{code,open,high,low,close,volume,last_close,change_pct}]}
  6. GET  /api/kline-history-tdx     -> {count, List:[{Time,Open,High,Low,Close,Volume,...}]}
  7. GET  /api/kline-all/tdx         -> {count, list, meta}
  8. GET  /api/index/all             -> {count, list}
  9. GET  /api/kline-index-history   -> {count, List|list}
 10. GET  /api/workday?date&count=1  -> {is_workday}

用法:
  python3 scripts/downstream_check.py <old_base> <new_base>              # 快速项 (不含 snapshot)
  python3 scripts/downstream_check.py --snap old.json new.json           # 对比已抓好的快照文件
  python3 scripts/downstream_check.py <old> <new> --full                 # 含实时 snapshot 对比 (3-15min/端)

纯标准库。
"""
import json
import sys
import time
import urllib.parse
import urllib.request

TIMEOUT = 60


def get(base, path, params=None, timeout=TIMEOUT):
    url = base.rstrip("/") + path
    if params:
        url += "?" + urllib.parse.urlencode(params)
    try:
        with urllib.request.urlopen(url, timeout=timeout) as resp:
            body = json.loads(resp.read().decode("utf-8", "replace"))
            if body.get("code") != 0:
                return None, f"code={body.get('code')} msg={body.get('message')}"
            return body.get("data"), None
    except Exception as e:  # noqa: BLE001
        return None, str(e)


def post(base, path, body, timeout=TIMEOUT):
    url = base.rstrip("/") + path
    try:
        req = urllib.request.Request(
            url, data=json.dumps(body).encode(),
            headers={"Content-Type": "application/json"}, method="POST")
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = json.loads(resp.read().decode("utf-8", "replace"))
            if body.get("code") != 0:
                return None, f"code={body.get('code')} msg={body.get('message')}"
            return body.get("data"), None
    except Exception as e:  # noqa: BLE001
        return None, str(e)


VERDICT = {}


def report(name, ok, detail=""):
    VERDICT[name] = "OK" if ok else "FAIL"
    print(f"[{'OK' if ok else 'FAIL'}] {name}" + (f"  {detail}" if detail else ""))


# ── 1. workday (is_trading_day) ──────────────────────────────────────────
def check_workday(old, new):
    ok_all, details = True, []
    for date, expect in [("20260919", False), ("20260918", True), ("2026-09-18", True)]:
        do, e1 = get(old, "/api/workday", {"date": date, "count": 1})
        dn, e2 = get(new, "/api/workday", {"date": date, "count": 1})
        vo = None if e1 else do.get("is_workday")
        vn = None if e2 else dn.get("is_workday")
        same = vo == vn == expect and e1 is None and e2 is None
        ok_all &= same
        details.append(f"{date}: old={vo} new={vn} expect={expect}")
    report("1.workday(is_trading_day)", ok_all, "; ".join(details))


# ── 2. ready ─────────────────────────────────────────────────────────────
def check_ready(old, new):
    do, e1 = get(old, "/api/ready")
    dn, e2 = get(new, "/api/ready")
    ok = e1 is None and e2 is None and set(do or {}) >= {"ready"} and set(dn or {}) >= {"ready"} \
        and do.get("ready") is True and dn.get("ready") is True
    report("2.ready_check", ok, f"old={do} new={dn}" if not ok else "")


# ── 3. stock-codes ───────────────────────────────────────────────────────
def check_stock_codes(old, new):
    do, e1 = get(old, "/api/stock-codes")
    dn, e2 = get(new, "/api/stock-codes")
    if e1 or e2:
        report("3.stock_codes", False, f"old_err={e1} new_err={e2}")
        return
    lo, ln = set(do["list"]), set(dn["list"])
    added, removed = ln - lo, lo - ln
    # 前缀/格式校验: 下游依赖 sh/sz/bj 小写前缀
    fmt_bad = [c for c in list(ln)[:100] if c[:2].lower() not in ("sh", "sz", "bj")]
    ok = not fmt_bad
    detail = f"old={len(lo)} new={len(ln)} 新增={len(added)} 移除={len(removed)} 格式异常={len(fmt_bad)}"
    if added:
        detail += f" 新增样本={sorted(added)[:5]}"
    if removed:
        detail += f" 移除样本={sorted(removed)[:5]}"
    report("3.stock_codes", ok, detail)


# ── 4. gbbq refresh + get ────────────────────────────────────────────────
GBBQ_CODES = ["sh600000", "sh600519", "sz000001", "sz000651", "sh601318"]


def check_gbbq(old, new, do_refresh=True):
    if do_refresh:
        ro, e1 = post(old, "/api/gbbq/refresh", {"codes": GBBQ_CODES}, timeout=300)
        rn, e2 = post(new, "/api/gbbq/refresh", {"codes": GBBQ_CODES}, timeout=300)
        ok = e1 is None and e2 is None
        detail = f"refresh old(success={ro.get('success_count') if ro else e1}) " \
                 f"new(success={rn.get('success_count') if rn else e2})"
        report("4a.gbbq_refresh", ok, detail)
        if not ok:
            return

    xrxd_bad = equity_bad = 0
    for code in GBBQ_CODES:
        do, e1 = get(old, "/api/gbbq", {"code": code})
        dn, e2 = get(new, "/api/gbbq", {"code": code})
        if e1 or e2:
            report(f"4b.gbbq_get[{code}]", False, f"old_err={e1} new_err={e2}")
            return
        # 下游消费字段: xrxd {date,fenhong,peigu,peigujia,songzhuangu} equity {date,float,total}
        xo = sorted((d.get("date"), d.get("fenhong"), d.get("peigu"), d.get("peigujia"), d.get("songzhuangu"))
                    for d in do.get("xrxd") or [])
        xn = sorted((d.get("date"), d.get("fenhong"), d.get("peigu"), d.get("peigujia"), d.get("songzhuangu"))
                    for d in dn.get("xrxd") or [])
        eo = sorted((d.get("date"), d.get("float"), d.get("total")) for d in do.get("equity") or [])
        en = sorted((d.get("date"), d.get("float"), d.get("total")) for d in dn.get("equity") or [])
        if xo != xn:
            xrxd_bad += 1
            print(f"    xrxd 差异 [{code}]: old={len(xo)}条 new={len(xn)}条")
            for a, b in zip(xo, xn):
                if a != b:
                    print(f"      old={a} new={b}")
                    break
        if eo != en:
            equity_bad += 1
            print(f"    equity 差异 [{code}]: old={len(eo)}条 new={len(en)}条")
            for a, b in zip(eo, en):
                if a != b:
                    print(f"      old={a} new={b}")
                    break
    report("4b.gbbq_get(逐字段)", xrxd_bad == 0 and equity_bad == 0,
           f"{len(GBBQ_CODES)} 只中 xrxd 差异 {xrxd_bad}, equity 差异 {equity_bad}")


# ── 5/6/7. kline-history-tdx / index-history / kline-all / index-all ─────
def cmp_kline_lists(lo, ln, keys=("Time", "Open", "High", "Low", "Close", "Volume")):
    """按 Time 对齐逐条比较; 返回 (共同条数, {key: 差异条数}, 差异样本)"""
    mo = {str(r.get("Time")): r for r in lo or []}
    mn = {str(r.get("Time")): r for r in ln or []}
    common = sorted(set(mo) & set(mn))
    bad = {k: 0 for k in keys}
    samples = []
    for t in common:
        a, b = mo[t], mn[t]
        for k in keys:
            if a.get(k) != b.get(k):
                bad[k] += 1
                if len(samples) < 5:
                    samples.append((t, k, a.get(k), b.get(k)))
    return len(common), bad, samples, sorted(set(mo) - set(mn)), sorted(set(mn) - set(mo))


KLINE_CODES = ["sh600000", "sz000001", "sh600519", "sh510300", "sh110092"]
RANGE = ("2026-08-01", "2026-09-18")


def check_kline_history(old, new):
    total_bad = 0
    for code in KLINE_CODES:
        do, e1 = get(old, "/api/kline-history-tdx",
                     {"code": code, "type": "day", "start_date": RANGE[0], "end_date": RANGE[1]})
        dn, e2 = get(new, "/api/kline-history-tdx",
                     {"code": code, "type": "day", "start_date": RANGE[0], "end_date": RANGE[1]})
        if e1 or e2:
            report(f"5.kline-history[{code}]", False, f"old_err={e1} new_err={e2}")
            total_bad += 1
            continue
        n, bad, samples, only_o, only_n = cmp_kline_lists(
            do.get("List") or do.get("list"), dn.get("List") or dn.get("list"))
        amount_bad = bad.pop("Amount", 0) if "Amount" in bad else 0
        ohlcv_bad = sum(bad.values())
        total_bad += ohlcv_bad
        note = f"{code}: 共同 {n} 条, OHLCV差异 {ohlcv_bad}, Amount差异 {amount_bad}(浮点修复预期)"
        if only_o or only_n:
            note += f", 仅旧 {len(only_o)} 仅新 {len(only_n)}"
        print(f"    {note}")
        for t, k, a, b in samples:
            print(f"      {t} {k}: old={a} new={b}")
    report("5.kline-history-tdx(逐字段)", total_bad == 0)


def check_kline_history_week(old, new):
    do, e1 = get(old, "/api/kline-history-tdx",
                 {"code": "sh600000", "type": "week", "start_date": "2026-01-01"})
    dn, e2 = get(new, "/api/kline-history-tdx",
                 {"code": "sh600000", "type": "week", "start_date": "2026-01-01"})
    if e1 or e2:
        report("6.kline-history-week", False, f"old_err={e1} new_err={e2}")
        return
    n, bad, samples, _, _ = cmp_kline_lists(do.get("List") or do.get("list"), dn.get("List") or dn.get("list"))
    report("6.kline-history-week", sum(bad.values()) == 0, f"共同 {n} 条, 差异 {sum(bad.values())}")


# 真实 d_plate_base 代码 (index 8 只 + concept/industry 样本)
PLATE_CODES = ["sh000001", "sz399001", "sz399006", "sh000688", "sh000016",
               "sh000300", "sh000905", "sh000852", "sh999999",
               "sh880501", "sh881002", "sh880666"]


def check_kline_index_history(old, new):
    total_bad = 0
    for code in PLATE_CODES:
        do, e1 = get(old, "/api/kline-index-history",
                     {"code": code, "start_date": RANGE[0], "end_date": RANGE[1]})
        dn, e2 = get(new, "/api/kline-index-history",
                     {"code": code, "start_date": RANGE[0], "end_date": RANGE[1]})
        if e1 or e2:
            report(f"7.kline-index-history[{code}]", False, f"old_err={e1} new_err={e2}")
            total_bad += 1
            continue
        n, bad, samples, only_o, only_n = cmp_kline_lists(
            do.get("List") or do.get("list"), dn.get("List") or dn.get("list"))
        bad.pop("Amount", None)
        ohlcv_bad = sum(bad.values())
        total_bad += ohlcv_bad
        if ohlcv_bad or only_o or only_n:
            print(f"    {code}: 共同 {n}, OHLCV差异 {ohlcv_bad}, 仅旧 {len(only_o)} 仅新 {len(only_n)}")
            for t, k, a, b in samples[:3]:
                print(f"      {t} {k}: old={a} new={b}")
    # 下游 _add_market_prefix_for_index 的大写裸码路径
    do, e1 = get(old, "/api/kline-index-history", {"code": "SZ399001", "start_date": RANGE[0]})
    dn, e2 = get(new, "/api/kline-index-history", {"code": "SZ399001", "start_date": RANGE[0]})
    up_ok = (e1 is None) == (e2 is None)
    report("7.kline-index-history(板块+指数, 逐字段)", total_bad == 0,
           f"大写SZ399001: old_err={e1} new_err={e2}" if not up_ok else "含大写裸码路径行为一致")


def check_kline_all(old, new):
    for name, ep, code in [("8a.kline-all/tdx", "/api/kline-all/tdx", "sh600000"),
                           ("8b.index/all", "/api/index/all", "sh880666")]:
        do, e1 = get(old, ep, {"code": code, "type": "day", "limit": 30}, timeout=120)
        dn, e2 = get(new, ep, {"code": code, "type": "day", "limit": 30}, timeout=120)
        if e1 or e2:
            report(name, False, f"old_err={e1} new_err={e2}")
            continue
        n, bad, samples, only_o, only_n = cmp_kline_lists(
            do.get("list") or do.get("List"), dn.get("list") or dn.get("List"))
        bad.pop("Amount", None)
        report(name, sum(bad.values()) == 0,
               f"共同 {n}, 差异 {sum(bad.values())}, 仅旧 {len(only_o)} 仅新 {len(only_n)}")


# ── 9. market-snapshot ───────────────────────────────────────────────────
def check_snapshot_data(do, dn, label=""):
    # 兼容两种输入: 完整响应 {code,message,data} 或已是 data 字典
    if isinstance(do, str):
        do = json.load(open(do))
    if isinstance(dn, str):
        dn = json.load(open(dn))
    if isinstance(do, dict) and "data" in do:
        do = do["data"]
    if isinstance(dn, dict) and "data" in dn:
        dn = dn["data"]
    date_ok = do.get("date") == dn.get("date")
    lo = {r["code"]: r for r in do.get("list") or []}
    ln = {r["code"]: r for r in dn.get("list") or []}
    common = set(lo) & set(ln)
    bad = {k: 0 for k in ("open", "high", "low", "close", "volume", "last_close", "change_pct")}
    samples = []
    for c in common:
        a, b = lo[c], ln[c]
        for k in bad:
            av, bv = a.get(k), b.get(k)
            if av is None and bv is None:
                continue
            if isinstance(av, (int, float)) and isinstance(bv, (int, float)):
                if abs(av - bv) > max(abs(av), abs(bv)) * 1e-9:
                    bad[k] += 1
                    if len(samples) < 5:
                        samples.append((c, k, av, bv))
            elif av != bv:
                bad[k] += 1
                if len(samples) < 5:
                    samples.append((c, k, av, bv))
    total_bad = sum(bad.values())
    print(f"[snapshot{label}] date: old={do.get('date')} new={dn.get('date')} ({'一致' if date_ok else '不一致'})")
    print(f"[snapshot{label}] 只数: old={len(lo)} new={len(ln)} 共同={len(common)} "
          f"仅旧={len(set(lo)-set(ln))} 仅新={len(set(ln)-set(lo))}")
    print(f"[snapshot{label}] 逐字段差异: {bad}")
    for c, k, a, b in samples:
        print(f"    {c} {k}: old={a} new={b}")
    report(f"9.market-snapshot{label}", total_bad == 0 and date_ok and len(set(lo) ^ set(ln)) < 50,
           f"总差异 {total_bad}")


# ── main ─────────────────────────────────────────────────────────────────
def main():
    if len(sys.argv) >= 4 and sys.argv[1] == "--snap":
        check_snapshot_data(sys.argv[2], sys.argv[3])
        print("\n[verdict]", json.dumps(VERDICT, ensure_ascii=False))
        return 0 if all(v == "OK" for v in VERDICT.values()) else 1

    old, new = sys.argv[1], sys.argv[2]
    full = "--full" in sys.argv
    check_workday(old, new)
    check_ready(old, new)
    check_stock_codes(old, new)
    check_gbbq(old, new, do_refresh=not full)  # --full 时 snapshot 先跑, gbbq refresh 放最后避免抢连接
    check_kline_history(old, new)
    check_kline_history_week(old, new)
    check_kline_index_history(old, new)
    check_kline_all(old, new)
    if full:
        so, e1 = get(old, "/api/market-snapshot", timeout=1800)
        sn, e2 = get(new, "/api/market-snapshot", timeout=1800)
        if e1 or e2:
            report("9.market-snapshot", False, f"old_err={e1} new_err={e2}")
        else:
            check_snapshot_data(so, sn)
    print("\n[verdict]", json.dumps(VERDICT, ensure_ascii=False))
    return 0 if all(v == "OK" for v in VERDICT.values()) else 1


if __name__ == "__main__":
    sys.exit(main())
