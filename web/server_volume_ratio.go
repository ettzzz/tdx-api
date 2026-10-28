package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/injoyai/tdx"
	"github.com/injoyai/tdx/protocol"
)

// ===== 量比 (通达信线性口径) =====
//
// 量比(t) = 当日累计成交量(t) ÷ (前N个交易日平均每分钟成交量 × 已开市分钟数(t))
//
//   - 分母只需日级数据: 5日总量 ÷ (N×240), 一次日K请求即得, 不依赖任何日内粒度数据
//   - 分子按 5 秒粒度取当日累计成交量; 与通达信口径在收盘时收敛 (当日总量 ÷ 5日平均每分钟量×240)
//   - 集合竞价量 (09:25) 计入分子, 从 t=0 起生效 (与分时 bar 第一根含竞价量一致)
//   - 停牌日无日K bar, 基准日按个股实际交易日滚动
//
// 两个端点, 均为无状态计算 (无缓存表/无落库, 分母仅日内内存备忘):
//   - GET /api/volume-ratio          实盘快照: ≤20只, 批量 quote + 分母备忘, 下游按 ≥5s 节奏轮询
//   - GET /api/volume-ratio/history  回测序列: 某日全天 5 秒粒度量比, 由分时成交 tick (3秒快照栅格) 重建

const (
	vrTradingSeconds = 240 * 60 // 全天交易秒数
	vrMaxCodes       = 20       // 实盘端点单次最大股票数 (下游 5s/次 × 20 只, 过载可下调)
	vrBasisDays      = 5        // 基准交易日数 (通达信口径)
)

// vrDenominator 量比分母: 目标日之前 ≤N 个交易日的平均每分钟成交量 (手/分钟)
type vrDenominator struct {
	day       string   // 目标日 (YYYYMMDD)
	avgPerMin float64  // 平均每分钟量 (手)
	basisDays int      // 实际基准天数 (新股/次新不足 5 日时 < 5)
	dates     []string // 基准日列表 (YYYYMMDD, 升序)
}

var (
	vrDenMu    sync.Mutex
	vrDenCache = map[string]*vrDenominator{} // code6 → 分母 (仅实时端点用, 当日有效)
)

// vrGetDenominator 取分母; 实时 (目标日=今天) 走日内备忘, 历史 (任意目标日) 每次现算
func vrGetDenominator(c *tdx.Client, code6, targetDay string) (*vrDenominator, error) {
	today := time.Now().Format("20060102")
	if targetDay == today {
		vrDenMu.Lock()
		d, ok := vrDenCache[code6]
		vrDenMu.Unlock()
		if ok && d.day == targetDay {
			return d, nil
		}
	}
	d, err := vrComputeDenominator(c, code6, targetDay)
	if err != nil {
		return nil, err
	}
	if targetDay == today {
		vrDenMu.Lock()
		vrDenCache[code6] = d
		vrDenMu.Unlock()
	}
	return d, nil
}

// vrComputeDenominator 日K 取目标日之前 ≤5 个交易日总量 (排除目标日本身, 盘中目标日的当日 bar 亦被排除)。
// 从最近往回翻页直到收集满 5 根 —— 目标日早于最近 800 根 (约 3 年) 或个股长期停牌时,
// 单次请求不够, 必须续翻; 停牌日无 bar, 基准日天然按个股实际交易日滚动。
func vrComputeDenominator(c *tdx.Client, code6, targetDay string) (*vrDenominator, error) {
	type bar struct {
		day string
		vol int64
	}
	var bars []bar
	const pageSize = uint16(800) // TDX 单帧日K上限
	const maxPages = 10          // 8000 根 ≈ 32 年, 覆盖全部 A 股历史
	for start := uint16(0); start < pageSize*maxPages; start += pageSize {
		resp, err := c.GetKline(protocol.TypeKlineDay, code6, start, pageSize)
		if err != nil {
			return nil, err
		}
		for _, k := range resp.List {
			if d := k.Time.Format("20060102"); d < targetDay {
				bars = append(bars, bar{d, k.Volume})
			}
		}
		// 收集满即停: 第 1 页含全部最近 800 根, 若其中已有 ≥5 根早于目标日,
		// 目标日前最近的 5 根必在其中, 更老的页无需再翻
		if len(bars) >= vrBasisDays || resp.Count < pageSize {
			break
		}
	}
	if len(bars) == 0 {
		return nil, fmt.Errorf("目标日 %s 之前无日K数据 (早于上市日或参数有误)", targetDay)
	}
	sort.Slice(bars, func(i, j int) bool { return bars[i].day < bars[j].day })
	if len(bars) > vrBasisDays {
		bars = bars[len(bars)-vrBasisDays:]
	}
	var total int64
	dates := make([]string, 0, len(bars))
	for _, b := range bars {
		total += b.vol
		dates = append(dates, b.day)
	}
	return &vrDenominator{
		day:       targetDay,
		avgPerMin: float64(total) / float64(len(bars)*240),
		basisDays: len(bars),
		dates:     dates,
	}, nil
}

// codeIndex 返回 code 在 order 中的下标 (实时端点内部用, n≤20 无需 map)
func codeIndex(order []string, code string) int {
	for i, c := range order {
		if c == code {
			return i
		}
	}
	return -1
}

// vrAShare 校验代码属于沪深京 (标准行情/日K/分时成交的覆盖范围)。
// GetQuote 对非沪深京市场是整批拒绝, 必须逐只前置过滤, 避免一只坏码拖垮整批。
func vrAShare(code6 string) bool {
	ex, _, err := protocol.DecodeCode(protocol.AddPrefix(code6))
	if err != nil {
		return false
	}
	return ex == protocol.ExchangeSH || ex == protocol.ExchangeSZ || ex == protocol.ExchangeBJ
}

// vrElapsedTradingSeconds 自 09:30 起的已开市秒数, 午休冻结在 7200, 收盘后封顶 14400
// 返回 0 表示未开盘或集合竞价阶段 (09:25-09:30: 累计量已含竞价, 但分母为 0, 量比给 null)
func vrElapsedTradingSeconds(now time.Time) float64 {
	sec := now.Hour()*3600 + now.Minute()*60 + now.Second()
	switch {
	case sec < 34200: // < 09:30
		return 0
	case sec < 41400: // 早盘 09:30-11:30
		return float64(sec - 34200)
	case sec < 46800: // 午休 11:30-13:00
		return 7200
	case sec < 54000: // 午盘 13:00-15:00
		return float64(7200 + sec - 46800)
	default: // ≥ 15:00
		return vrTradingSeconds
	}
}

// ===== 实盘快照端点 =====

// vrRealtimeItem 单只股票的量比快照
type vrRealtimeItem struct {
	Code           string   `json:"code"`                      // 600000.SH 用户友好格式
	Ts             int64    `json:"ts"`                        // unix 秒
	CumVolume      int64    `json:"cum_volume"`                // 当日累计成交量 (手, 含集合竞价)
	ElapsedSeconds int      `json:"elapsed_seconds"`           // 已开市秒数 (0=未开盘/竞价中)
	AvgPerMin      float64  `json:"avg_per_min"`               // 前5日平均每分钟量 (手)
	BasisDays      int      `json:"basis_days"`                // 实际基准天数
	Ratio          *float64 `json:"ratio"`                     // 量比 (未开盘/无基准/非当日行情时为 null)
	Error          string   `json:"error,omitempty"`           // 单只失败时填
}

// handleVolumeRatio GET /api/volume-ratio?codes=600000.SH,000001.SZ
// 同步快照, 无订阅无状态; 下游按 ≥5s 节奏轮询, 单次 ≤20 只。
// 分母当日内存备忘: 首次每只 1 个日K请求, 之后每次轮询只剩 1 个批量 quote 请求。
func handleVolumeRatio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, "只支持GET")
		return
	}
	codeParam := r.URL.Query().Get("codes")
	if codeParam == "" {
		errorResponse(w, "codes 参数不能为空")
		return
	}
	// 归一化 + 去重 (保序)
	seen := map[string]struct{}{}
	var order []string
	for _, c := range splitCodes(codeParam) {
		n := normalizeCode(c)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		order = append(order, n)
	}
	if len(order) == 0 {
		errorResponse(w, "codes 参数无效")
		return
	}
	if len(order) > vrMaxCodes {
		errorResponse(w, fmt.Sprintf("codes 最多 %d 只", vrMaxCodes))
		return
	}

	now := time.Now()
	today := now.Format("20060102")
	elapsed := vrElapsedTradingSeconds(now)

	items := make([]*vrRealtimeItem, len(order))
	for i, code := range order {
		items[i] = &vrRealtimeItem{Code: withExchangeSuffix(code), Ts: now.Unix()}
	}

	// 注意错误文案契约: 含 "超时" 时下游判为瞬态重试, 保持 "获取行情失败: %v" 格式
	err := manager.Do(func(c *tdx.Client) error {
		// 市场过滤: 非沪深京按只隔离, 不拖垮整批
		var polled []string
		for i, code := range order {
			if !vrAShare(code) {
				items[i].Error = "非沪深京市场, 标准行情不支持"
				continue
			}
			polled = append(polled, code)
		}
		// 分母 (首查每只 1 个日K请求, 当日备忘)
		for _, code := range polled {
			i := codeIndex(order, code)
			d, e := vrGetDenominator(c, code, today)
			if e != nil {
				items[i].Error = "基准数据获取失败: " + e.Error()
				continue
			}
			items[i].AvgPerMin = d.avgPerMin
			items[i].BasisDays = d.basisDays
		}
		// 批量行情 (GetQuote 内部会改写入参 slice, 传入副本)
		quoteCodes := make([]string, len(polled))
		for i, code := range polled {
			quoteCodes[i] = protocol.AddPrefix(code)
		}
		if len(quoteCodes) == 0 {
			return nil
		}
		quotes, e := c.GetQuote(quoteCodes...)
		if e != nil {
			return e
		}
		quoteMap := make(map[string]*protocol.Quote, len(quotes))
		for _, q := range quotes {
			quoteMap[q.Code] = q
		}
		for _, code := range polled {
			i := codeIndex(order, code)
			if items[i].Error != "" {
				continue
			}
			q, ok := quoteMap[code]
			if !ok {
				items[i].Error = "无行情数据"
				continue
			}
			// 非当日行情防御 (盘前未滚动/节假日): quote 还停留在上一交易日
			if q.Kline.Time.Format("20060102") != today {
				items[i].Error = "非当日行情 (未开盘或非交易日)"
				items[i].CumVolume = q.Kline.Volume
				continue
			}
			items[i].CumVolume = q.Kline.Volume
			items[i].ElapsedSeconds = int(elapsed)
			if items[i].AvgPerMin > 0 && elapsed > 0 {
				ratio := float64(q.Kline.Volume) / (items[i].AvgPerMin * elapsed / 60)
				items[i].Ratio = &ratio
			}
		}
		return nil
	})
	if err != nil {
		errorResponse(w, fmt.Sprintf("获取行情失败: %v", err))
		return
	}
	successResponse(w, map[string]any{
		"ts":              now.Unix(),
		"elapsed_seconds": int(elapsed),
		"list":            items,
	})
}

// ===== 历史序列端点 =====

// vrSeriesPoint 量比序列点 (5s/1m 两种粒度共用, 字段语义随 granularity 变化)
type vrSeriesPoint struct {
	Time   string  `json:"time"`   // 5s: "09:30:05".."15:00:00" 桶右边界; 1m: "09:31:00".."15:00:00"
	Ratio  float64 `json:"ratio"`  // 量比 (通达信线性口径)
	Volume int64   `json:"volume"` // 该桶成交量 (手) = 桶间累计量增量
}

// handleVolumeRatioHistory GET /api/volume-ratio/history?code=300613.SZ&date=2026-10-28[&granularity=5s|1m]
// 某日全天量比序列: granularity=5s (默认, 2880 点) / 1m (240 个整分钟点)。
// 分子: 分时成交 tick (3 秒快照栅格, 分钟内第 k 条置于 [k*3, k*3+3) 窗口);
//
//	分钟边界只计入已完结窗口, 使边界累计精确等于分时 bar (已实测对齐, 全日 240 点两小数一致)。
//	集合竞价 (09:25) 全部置于 t=0 起的首窗口, 从第一桶生效。
//
// 分母: 目标日之前 ≤5 个交易日的平均每分钟量 (日K, 每次现算, 无备忘)。
// 1m 与 5s 同一条计算路径, 1m 即 5s 序列的整分钟边界点 (累计口径下边界点即分钟值)。
// 当日盘中数据不可靠 (历史 tick 端点盘后才完整), 盘中请用 /api/volume-ratio。
func handleVolumeRatioHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, "只支持GET")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		errorResponse(w, "code 参数不能为空")
		return
	}
	code6 := normalizeCode(code)
	if code6 == "" {
		errorResponse(w, "code 参数无效")
		return
	}
	if !vrAShare(code6) {
		errorResponse(w, "非沪深京市场, 标准行情不支持量比")
		return
	}
	date := r.URL.Query().Get("date")
	if date == "" {
		errorResponse(w, "date 参数不能为空")
		return
	}
	dateCompact, err := vrParseDate(date)
	if err != nil {
		errorResponse(w, "date 参数无效 (支持 2026-10-28 / 20261028)")
		return
	}
	granularity := r.URL.Query().Get("granularity")
	if granularity == "" {
		granularity = "5s"
	}
	if granularity != "5s" && granularity != "1m" {
		errorResponse(w, "granularity 仅支持 5s / 1m")
		return
	}

	var denom *vrDenominator
	var ticks protocol.Trades
	if err := manager.Do(func(c *tdx.Client) error {
		d, e := vrComputeDenominator(c, code6, dateCompact)
		if e != nil {
			return e
		}
		denom = d
		tr, e := c.GetHistoryMinuteTradeDay(dateCompact, code6)
		if e != nil {
			return e
		}
		ticks = tr.List
		return nil
	}); err != nil {
		errorResponse(w, fmt.Sprintf("获取量比历史数据失败: %v", err))
		return
	}
	if len(ticks) == 0 {
		errorResponse(w, "该日期无分时成交数据 (非交易日或超出服务器保留范围)")
		return
	}

	list, err := vrBuildSeries5s(ticks, denom)
	if err != nil {
		errorResponse(w, "量比序列计算失败: "+err.Error())
		return
	}
	// 1m: 取整分钟边界点 (累计口径下边界点即分钟值, 与分时 bar 直算精确一致)
	if granularity == "1m" {
		filtered := make([]vrSeriesPoint, 0, 240)
		for _, p := range list {
			if strings.HasSuffix(p.Time, ":00") {
				filtered = append(filtered, p)
			}
		}
		list = filtered
	}
	successResponse(w, map[string]any{
		"code":        withExchangeSuffix(code6),
		"date":        dateCompact,
		"granularity": granularity,
		"avg_per_min": denom.avgPerMin,
		"basis_days":  denom.basisDays,
		"basis_dates": denom.dates,
		"count":       len(list),
		"list":        list,
	})
}

// vrParseDate 归一化日期入参 → YYYYMMDD
func vrParseDate(s string) (string, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "-", "")
	if len(s) != 8 {
		return "", fmt.Errorf("日期长度无效")
	}
	if _, err := time.Parse("20060102", s); err != nil {
		return "", err
	}
	return s, nil
}

// vrBuildSeries5s 由全天 tick 重建 5 秒粒度量比序列
// 时间语义: tick 只带 HH:MM, 无秒; 服务器按 3 秒快照推送, 分钟内第 k 条置于
// [k*3, k*3+3) 窗口 (满密度分钟下与真实时间对齐, 稀疏分钟存在栅格不确定度)。
// 累计语义: 标签 s 处只计入 start+3 ≤ s 的已完结窗口 → 分钟边界精确等于分时 bar。
func vrBuildSeries5s(ticks protocol.Trades, denom *vrDenominator) ([]vrSeriesPoint, error) {
	if denom == nil || denom.avgPerMin <= 0 {
		return nil, fmt.Errorf("基准数据无效")
	}
	type tp struct {
		start int   // 窗口起点, 距 09:30:00 的秒数
		vol   int64 // 手
	}
	var pts []tp
	minuteK := map[int]int{} // 分钟 → 已见快照条数
	for _, t := range ticks {
		hm := t.Time.Hour()*60 + t.Time.Minute()
		var start int
		switch {
		case hm == 565: // 09:25 集合竞价: 同时成交, 全部置于首窗口
			start = 0
		case hm >= 570 && hm <= 690: // 早盘 09:30-11:30
			if hm == 690 { // 11:30 标签 = 早盘最后一个 3 秒窗口 [7197, 7200)
				start = 7197
			} else {
				start = (hm - 570) * 60
				k := minuteK[hm]
				minuteK[hm]++
				if over := k * 3; over < 57 {
					start += over
				} else {
					start += 57
				}
			}
		case hm >= 780 && hm <= 900: // 午盘 13:00-15:00
			if hm == 900 { // 15:00 标签 (收盘集合竞价) = 午盘最后一个窗口 [14397, 14400)
				start = 14397
			} else {
				start = 7200 + (hm-780)*60
				k := minuteK[hm]
				minuteK[hm]++
				if over := k * 3; over < 57 {
					start += over
				} else {
					start += 57
				}
			}
		default:
			continue
		}
		pts = append(pts, tp{start, int64(t.Volume)})
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].start < pts[j].start })

	out := make([]vrSeriesPoint, 0, vrTradingSeconds/5)
	i, cum, prev := 0, int64(0), int64(0)
	for s := 5; s <= vrTradingSeconds; s += 5 {
		for i < len(pts) && pts[i].start+3 <= s {
			cum += pts[i].vol
			i++
		}
		// 交易秒 → 墙钟标签: 早盘 09:30+s; 午盘在 11:30 之后跳过午休 2 小时
		var clock int
		if s <= 7200 {
			clock = 34200 + s
		} else {
			clock = 46800 + (s - 7200)
		}
		out = append(out, vrSeriesPoint{
			Time:   fmt.Sprintf("%02d:%02d:%02d", clock/3600, clock%3600/60, clock%60),
			Ratio:  float64(cum) / (denom.avgPerMin * float64(s) / 60),
			Volume: cum - prev,
		})
		prev = cum
	}
	return out, nil
}
