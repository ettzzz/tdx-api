package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/injoyai/tdx"
	"github.com/injoyai/tdx/protocol"
)

// QuoteTick 单条行情推送 (NDJSON 一行)
type QuoteTick struct {
	Ts          int64      `json:"ts"`                   // unix 秒
	Code        string     `json:"code"`                 // 股票代码 (含交易所前缀)
	Price       float64    `json:"price"`                // 最新价 (元)
	Open        float64    `json:"open"`                 // 今日开盘 (元)
	High        float64    `json:"high"`                 // 今日最高 (元)
	Low         float64    `json:"low"`                  // 今日最低 (元)
	PreClose    float64    `json:"pre_close"`            // 昨日收盘 (元)
	Volume      int64      `json:"volume"`               // 累计成交量 (手)
	Amount      float64    `json:"amount"`               // 累计成交额 (元)
	Bid         [][]any    `json:"bid,omitempty"`        // 5 档买盘 [[价, 量], ...]
	Ask         [][]any    `json:"ask,omitempty"`        // 5 档卖盘
	VolumeRatio *float64   `json:"volume_ratio"`         // 量比 (无数据时为 null)
	RatioBasis  int        `json:"ratio_basis"`          // 实际使用的历史天数 (0=无)
	Error       string     `json:"error,omitempty"`      // 单只拉取失败时填
}

// VolumeWindow 量比历史窗口 (内存, 不落盘)
// Days[i] 是某一天每根 minute K 线的成交量 (手)
// 5 天 × 240 根 ≈ 1200 个 int64, 单只约 10 KB
type VolumeWindow struct {
	Code      string
	Days      [][]int64
	BaseDates []string
	BuiltAt   time.Time
}

// Ratio 当前累计成交量与历史同时段均值的比
// nowMinute: 从 9:30 起算的盘中分钟数 (-1=未开盘, 240=已收盘)
// currentTotalHand: 当前累计成交量 (手, 来自 Quote.TotalHand)
func (w *VolumeWindow) Ratio(nowMinute int, currentTotalHand int64) (ratio float64, basis int) {
	if w == nil || len(w.Days) == 0 || nowMinute < 0 {
		return 0, 0
	}
	var sum int64
	var cnt int
	for _, day := range w.Days {
		// 收盘后 (nowMinute >= len(day)) 或盘中超出 day 长度时, 取该天全天累计
		idx := nowMinute
		if idx >= len(day) {
			idx = len(day) - 1
		}
		if idx < 0 {
			continue
		}
		var cum int64
		for i := 0; i <= idx; i++ {
			cum += day[i]
		}
		sum += cum
		cnt++
	}
	if cnt == 0 || sum == 0 {
		return 0, 0
	}
	avg := sum / int64(cnt)
	if avg == 0 {
		return 0, 0
	}
	return float64(currentTotalHand) / float64(avg), cnt
}

// subscriber 单个 HTTP 客户端订阅
type subscriber struct {
	codes map[string]struct{}
	ch    chan QuoteTick
	ctx   context.Context
}

// Broker 实时行情中心
type Broker struct {
	mu sync.RWMutex

	// 量比窗口
	windows    map[string]*VolumeWindow
	preheating map[string]bool

	// 订阅关系
	subs      map[string][]*subscriber // code -> 订阅者列表
	pollCodes map[string]struct{}      // 后台轮询的股票 union

	// 轮询状态
	lastPollAt  int64
	lastPollErr string
	pollCount   int64

	// 配置
	pollInterval time.Duration
	defaultBasis int
	minInterval  time.Duration

	wg sync.WaitGroup
}

// NewBroker 构造
func NewBroker() *Broker {
	return &Broker{
		windows:      make(map[string]*VolumeWindow),
		preheating:   make(map[string]bool),
		subs:         make(map[string][]*subscriber),
		pollCodes:    make(map[string]struct{}),
		pollInterval: time.Second,
		defaultBasis: 5,
		minInterval:  200 * time.Millisecond,
	}
}

// Subscribe 注册订阅, 返回 channel 和取消函数
// 入参 codes 接受 "600000.SH" / "SH600000" / "600000" 三种格式, 内部统一归一为 6 位数字
func (b *Broker) Subscribe(ctx context.Context, codes []string, ratioBasis int) (<-chan QuoteTick, func()) {
	sub := &subscriber{
		codes: make(map[string]struct{}, len(codes)),
		ch:    make(chan QuoteTick, 16),
		ctx:   ctx,
	}
	normalized := make([]string, 0, len(codes))
	for _, c := range codes {
		n := normalizeCode(c)
		if n == "" {
			continue
		}
		sub.codes[n] = struct{}{}
		normalized = append(normalized, n)
	}

	b.mu.Lock()
	for _, c := range normalized {
		b.subs[c] = append(b.subs[c], sub)
		b.pollCodes[c] = struct{}{}
		// 按需触发预热
		if _, ok := b.windows[c]; !ok {
			if _, busy := b.preheating[c]; !busy {
				b.preheating[c] = true
				go b.preheat(c, ratioBasis)
			}
		}
	}
	b.mu.Unlock()

	cancelled := false
	cancel := func() {
		if cancelled {
			return
		}
		cancelled = true
		b.mu.Lock()
		for _, c := range normalized {
			list := b.subs[c]
			for i, s := range list {
				if s == sub {
					b.subs[c] = append(list[:i], list[i+1:]...)
					break
				}
			}
			if len(b.subs[c]) == 0 {
				delete(b.subs, c)
			}
		}
		b.mu.Unlock()
	}

	return sub.ch, cancel
}

// fanout 发送一条 tick 到所有订阅该 code 的客户端
// tick.Code 可能是用户友好格式 (e.g. "600000.SH"), 需要 normalize 回 6 位数字去查 sub.codes
func (b *Broker) fanout(tick QuoteTick) {
	key := normalizeCode(tick.Code)
	b.mu.RLock()
	subs := b.subs[key]
	snapshot := make([]*subscriber, len(subs))
	copy(snapshot, subs)
	b.mu.RUnlock()

	for _, s := range snapshot {
		if _, ok := s.codes[key]; !ok {
			continue
		}
		select {
		case s.ch <- tick:
		case <-s.ctx.Done():
		default:
			// 客户端消费太慢, 丢当帧 (不影响其他订阅者)
		}
	}
}

// pollOnce 执行一次轮询
func (b *Broker) pollOnce() {
	b.mu.RLock()
	codes := make([]string, 0, len(b.pollCodes))
	for c := range b.pollCodes {
		codes = append(codes, c)
	}
	b.mu.RUnlock()

	if len(codes) == 0 {
		return
	}

	// 复制一份给 GetQuote: client.GetQuote 内部会修改入参 slice (client.go:302-312),
	// 会把 6 位数字补成 "SH600000", 污染我们后面用的 codes
	getQuoteCodes := make([]string, len(codes))
	copy(getQuoteCodes, codes)

	var quotes protocol.QuotesResp
	err := manager.Pool.Do(func(c *tdx.Client) error {
		var e error
		quotes, e = c.GetQuote(getQuoteCodes...)
		return e
	})

	b.mu.Lock()
	b.lastPollAt = time.Now().Unix()
	b.pollCount++
	if err != nil {
		b.lastPollErr = err.Error()
	} else {
		b.lastPollErr = ""
	}
	b.mu.Unlock()

	if err != nil {
		// 整批失败: 给每个订阅的 code 推一个 error tick
		ts := time.Now().Unix()
		for _, code := range codes {
			b.fanout(QuoteTick{Ts: ts, Code: withExchangeSuffix(code), Error: "upstream_unavailable"})
		}
		return
	}

	// 用 6 位数字 code -> quote 索引 (TDX 返回的 Quote.Code 是 6 位 ascii, 不含交易所前缀)
	quoteMap := make(map[string]*protocol.Quote, len(quotes))
	for _, q := range quotes {
		quoteMap[q.Code] = q
	}

	nowMinute := currentTradingMinute()

	// 复制一份 windows map (避免持锁调用 Ratio)
	b.mu.RLock()
	wins := make(map[string]*VolumeWindow, len(b.windows))
	for c, w := range b.windows {
		wins[c] = w
	}
	b.mu.RUnlock()

	ts := time.Now().Unix()
	for _, code := range codes {
		q, ok := quoteMap[code]
		if !ok {
			b.fanout(QuoteTick{Ts: ts, Code: withExchangeSuffix(code), Error: "decode_failed"})
			continue
		}
		tick := QuoteTick{
			Ts:       ts,
			Code:     withExchangeSuffix(code),
			Price:    q.K.Close.Float64(),
			Open:     q.K.Open.Float64(),
			High:     q.K.High.Float64(),
			Low:      q.K.Low.Float64(),
			PreClose: q.K.Last.Float64(),
			Volume:   int64(q.TotalHand),
			Amount:   q.Amount,
			Bid:      levelsToJSON(q.BuyLevel),
			Ask:      levelsToJSON(q.SellLevel),
		}
		if w, ok := wins[code]; ok {
			ratio, basis := w.Ratio(nowMinute, int64(q.TotalHand))
			if basis > 0 {
				r := ratio
				tick.VolumeRatio = &r
				tick.RatioBasis = basis
			}
		}
		b.fanout(tick)
	}
}

// Run 启动后台 polling (一个独立 goroutine, 占 1 个 Pool slot 由 pollOnce 内部获取)
func (b *Broker) Run(ctx context.Context) {
	b.wg.Add(1)
	defer b.wg.Done()
	ticker := time.NewTicker(b.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[realtime] poll panic: %v", r)
						b.mu.Lock()
						b.lastPollErr = fmt.Sprintf("panic: %v", r)
						b.mu.Unlock()
					}
				}()
				b.pollOnce()
			}()
		}
	}
}

// preheat 拉历史 minute K 线构建量比窗口
func (b *Broker) preheat(code string, basisDays int) {
	defer func() {
		b.mu.Lock()
		delete(b.preheating, code)
		b.mu.Unlock()
	}()

	if basisDays <= 0 {
		basisDays = b.defaultBasis
	}
	if basisDays > 20 {
		basisDays = 20
	}

	var resp *protocol.KlineResp
	var err error
	for retry := 0; retry < 3; retry++ {
		// 直接复用 manager.Pool: 它本身是 4 slot, fan-out 模式下
		// 预热任务和 pollOnce 竞争同一个池, 互不阻塞是预期的
		err = manager.Pool.Do(func(c *tdx.Client) error {
			var e error
			resp, e = c.GetKlineMinuteAll(code)
			return e
		})
		if err == nil {
			break
		}
		time.Sleep(time.Duration(retry+1) * 500 * time.Millisecond)
	}
	if err != nil {
		log.Printf("[realtime] preheat %s failed after retries: %v", code, err)
		return
	}

	today := time.Now().Format("20060102")
	dayMap := map[string][]*protocol.Kline{}
	for _, k := range resp.List {
		d := k.Time.Format("20060102")
		if d == today {
			continue // 跳过今天盘中数据
		}
		dayMap[d] = append(dayMap[d], k)
	}

	var sortedDates []string
	for d := range dayMap {
		sortedDates = append(sortedDates, d)
	}
	sort.Strings(sortedDates)
	if len(sortedDates) > basisDays {
		sortedDates = sortedDates[len(sortedDates)-basisDays:]
	}

	win := &VolumeWindow{
		Code:      code,
		Days:      make([][]int64, 0, len(sortedDates)),
		BaseDates: make([]string, 0, len(sortedDates)),
		BuiltAt:   time.Now(),
	}
	for _, d := range sortedDates {
		dayKlines := dayMap[d]
		sort.Slice(dayKlines, func(i, j int) bool {
			return dayKlines[i].Time.Before(dayKlines[j].Time)
		})
		vols := make([]int64, len(dayKlines))
		for i, k := range dayKlines {
			vols[i] = k.Volume
		}
		win.Days = append(win.Days, vols)
		win.BaseDates = append(win.BaseDates, d)
	}

	b.mu.Lock()
	b.windows[code] = win
	b.mu.Unlock()
	log.Printf("[realtime] preheat %s done: %d days (%v)", code, len(win.Days), win.BaseDates)
}

// PreheatCodes 主动预热一组股票
// 入参 codes 接受 "600000.SH" / "SH600000" / "600000" 任意格式, 内部归一化
func (b *Broker) PreheatCodes(codes []string, basisDays int) {
	for _, raw := range codes {
		c := normalizeCode(raw)
		if c == "" {
			continue
		}
		b.mu.Lock()
		if _, ok := b.windows[c]; ok {
			b.mu.Unlock()
			continue
		}
		if _, busy := b.preheating[c]; busy {
			b.mu.Unlock()
			continue
		}
		b.preheating[c] = true
		b.mu.Unlock()
		go b.preheat(c, basisDays)
	}
}

// WindowedCodes 返回已预热量比窗口的股票
func (b *Broker) WindowedCodes() []map[string]any {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]map[string]any, 0, len(b.windows))
	for code, w := range b.windows {
		out = append(out, map[string]any{
			"code":        code,
			"days":        len(w.Days),
			"base_dates":  w.BaseDates,
			"built_at":    w.BuiltAt.Unix(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["code"].(string) < out[j]["code"].(string)
	})
	return out
}

// SetInterval 设置轮询间隔 (>=200ms)
func (b *Broker) SetInterval(d time.Duration) {
	if d < b.minInterval {
		d = b.minInterval
	}
	b.mu.Lock()
	b.pollInterval = d
	b.mu.Unlock()
}

// Stats 返回 broker 状态
func (b *Broker) Stats() map[string]any {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subCount := 0
	for _, list := range b.subs {
		subCount += len(list)
	}
	return map[string]any{
		"window_count":     len(b.windows),
		"preheating_count": len(b.preheating),
		"subscriber_total": subCount,
		"subscriber_codes": len(b.subs),
		"poll_codes":       len(b.pollCodes),
		"last_poll_at":     b.lastPollAt,
		"last_poll_err":    b.lastPollErr,
		"poll_count":       b.pollCount,
		"poll_interval_ms": b.pollInterval.Milliseconds(),
	}
}

// 辅助函数

// normalizeCode 把任意格式股票代码归一化为 6 位数字
// 支持入参: "600000.SH" / "sh600000" / "600000" → "600000"
// TDX 协议层和 quote 返回都用 6 位数字做 key; 用户友好格式 (.SH/.SZ 后缀) 在出口处还原
func normalizeCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	// 处理 "600000.SH" 格式: 转成 "SH600000" 再交给 AddPrefix
	if i := strings.Index(code, "."); i >= 0 {
		exchange := code[i+1:] // "SH"
		number := code[:i]     // "600000"
		if len(number) == 6 && (exchange == "SH" || exchange == "SZ" || exchange == "BJ") {
			code = exchange + number
		}
	}
	code = protocol.AddPrefix(code) // SH600000 不变; 600000 → SH600000
	if len(code) == 8 {
		code = code[2:] // SH600000 → 600000
	}
	return code
}

// withExchangeSuffix 给 6 位数字加交易所后缀 (AddPrefix 的反向)
// 仅在出口处用, 让 NDJSON 返回用户友好的 "600000.SH" 格式
func withExchangeSuffix(code6 string) string {
	if len(code6) != 6 {
		return code6
	}
	switch {
	case strings.HasPrefix(code6, "6"),
		strings.HasPrefix(code6, "510"), strings.HasPrefix(code6, "511"),
		strings.HasPrefix(code6, "512"), strings.HasPrefix(code6, "513"),
		strings.HasPrefix(code6, "515"):
		return code6 + ".SH"
	case strings.HasPrefix(code6, "0"),
		strings.HasPrefix(code6, "30"),
		strings.HasPrefix(code6, "159"):
		return code6 + ".SZ"
	case strings.HasPrefix(code6, "8"),
		strings.HasPrefix(code6, "92"), strings.HasPrefix(code6, "43"):
		return code6 + ".BJ"
	}
	return code6
}

func levelsToJSON(levels protocol.PriceLevels) [][]any {
	out := make([][]any, 0, len(levels))
	for _, l := range levels {
		out = append(out, []any{l.Price.Float64(), l.Number})
	}
	return out
}

// currentTradingMinute 计算当前交易分钟数 (从 9:30 起算, 含午休截断)
// 返回 -1 表示未开盘, 240 表示已收盘
func currentTradingMinute() int {
	now := time.Now()
	h, m, _ := now.Clock()
	if h < 9 || (h == 9 && m < 30) {
		return -1
	}
	if h >= 15 {
		return 240
	}
	var minutes int
	switch {
	case h < 11, h == 11 && m <= 30:
		// 早盘 9:30-11:30
		minutes = (h-9)*60 + (m - 30)
	case h < 13:
		// 午休 11:30-13:00: 截止到 120 分钟 (早盘收盘)
		minutes = 120
	default:
		// 午盘 13:00-15:00
		minutes = 120 + (h-13)*60 + m
	}
	if minutes > 240 {
		minutes = 240
	}
	return minutes
}

// realtimeBroker 全局实例
var realtimeBroker *Broker

// initRealtime 启动实时行情 broker
func initRealtime() {
	realtimeBroker = NewBroker()
	go realtimeBroker.Run(context.Background())
	log.Println("[realtime] broker 启动 (poll interval =", realtimeBroker.pollInterval, ")")
}

// HTTP handlers

// handleRealtimeQuote NDJSON 流式行情推送
// GET /api/realtime/quote?codes=600000.SH,000001.SZ&ratio_basis=5
// 轮询节奏由 broker 内部 ticker 控制 (默认 1 秒, 见 Broker.pollInterval)
func handleRealtimeQuote(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		errorResponse(w, "只支持GET")
		return
	}
	codeParam := r.URL.Query().Get("codes")
	if codeParam == "" {
		errorResponse(w, "codes 参数不能为空")
		return
	}
	codes := splitCodes(codeParam)
	if len(codes) == 0 {
		errorResponse(w, "codes 参数不能为空")
		return
	}
	if len(codes) > 50 {
		errorResponse(w, "codes 最多 50 只")
		return
	}

	ratioBasis := 5
	if s := r.URL.Query().Get("ratio_basis"); s != "" {
		if v, err := strconv.Atoi(s); err == nil && v >= 1 && v <= 20 {
			ratioBasis = v
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		errorResponse(w, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, cancelSub := realtimeBroker.Subscribe(r.Context(), codes, ratioBasis)
	defer cancelSub()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// 注意: json.NewEncoder 是 buffered 的, 不会立即写入 socket
	// 必须直接 json.Marshal + Write + Flush 才能实时推送
	writeLine := func(v any) bool {
		b, err := json.Marshal(v)
		if err != nil {
			return false
		}
		if _, err := w.Write(b); err != nil {
			return false
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case tick, ok := <-ch:
			if !ok {
				return
			}
			if !writeLine(tick) {
				return
			}
		case <-heartbeat.C:
			// 周期心跳行, 防止反向代理超时断开
			fmt.Fprintf(w, "{\"type\":\"heartbeat\",\"ts\":%d}\n", time.Now().Unix())
			flusher.Flush()
		}
	}
}

// handleRealtimeHealth broker 状态
func handleRealtimeHealth(w http.ResponseWriter, r *http.Request) {
	if realtimeBroker == nil {
		errorResponse(w, "broker 未初始化")
		return
	}
	successResponse(w, realtimeBroker.Stats())
}

// handleRealtimePreheat 主动预热量比窗口
// POST /api/realtime/preheat  body: {"codes":["..."], "ratio_basis":5}
func handleRealtimePreheat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		errorResponse(w, "只支持POST")
		return
	}
	var req struct {
		Codes      []string `json:"codes"`
		RatioBasis int      `json:"ratio_basis"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errorResponse(w, "请求体解析失败: "+err.Error())
		return
	}
	if len(req.Codes) == 0 {
		errorResponse(w, "codes 不能为空")
		return
	}
	realtimeBroker.PreheatCodes(req.Codes, req.RatioBasis)
	successResponse(w, map[string]any{
		"requested":   req.Codes,
		"preheating":  true,
		"stats":       realtimeBroker.Stats(),
	})
}

// handleRealtimeCodes 列出已预热量比窗口的股票
func handleRealtimeCodes(w http.ResponseWriter, r *http.Request) {
	if realtimeBroker == nil {
		errorResponse(w, "broker 未初始化")
		return
	}
	successResponse(w, realtimeBroker.WindowedCodes())
}
