package main

// 港股行情路由 (2026-10-14 港股行情超时+连接污染移交单)。
//
// 背景: 标准行情主机(7709)不提供港股, 港股代码的请求过去发出畸形短帧
// (代码域 5 字节而非协议要求的 6 字节), 服务端按固定结构解析会吃掉紧随
// 其后下一帧的首字节, 造成同连接后续 A 股请求连锁超时(污染恰好持续一个请求)。
// 现协议层已补齐代码域(protocol.PadCode6), 且实测补齐后的港股请求会被
// 标准主机静默映射成深市错票数据(见 tools/hkprobe) —— 因此港股代码
// 绝不能发给标准行情连接, 统一路由到扩展行情主机(7727, TdxExHq, 市场号 31)。
//
// 扩展行情使用独立 TCP 连接, 与 A 股主连接(全局 client)物理隔离, 故障互不传染。

import (
	"fmt"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/injoyai/tdx"
	"github.com/injoyai/tdx/protocol"
)

var (
	hkMu     sync.Mutex
	hkClient *tdx.Client // 懒加载的扩展行情连接, 失效时重拨
)

// hkDialBudget 扩展行情拨号预算: ExRangeDial 串行轮询 16 台主机,
// 全部不可达时最坏 30s+, 不能让 HTTP 请求干等, 超时按瞬态失败返回。
const hkDialBudget = 8 * time.Second

// init 港股连接预热: 后台建立扩展行情连接, 失败不影响启动
// (首次港股请求会再次触发拨号)。
func init() {
	go func() {
		if _, err := getHKClient(); err != nil {
			log.Printf("[港股] 扩展行情预热失败(后续请求会重试): %v", err)
		} else {
			log.Printf("[港股] 扩展行情连接已预热")
		}
	}()
}

// getHKClient 获取扩展行情连接, 必要时拨号(带预算超时)。
// 调用方须持有 hkMu。
func getHKClient() (*tdx.Client, error) {
	if hkClient != nil {
		return hkClient, nil
	}
	type dialResult struct {
		cli *tdx.Client
		err error
	}
	ch := make(chan dialResult, 1)
	go func() {
		cli, err := tdx.DialExHqDefault(tdx.WithDebug(false))
		ch <- dialResult{cli, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("扩展行情源连接失败: %w", r.err)
		}
		hkClient = r.cli
		return hkClient, nil
	case <-time.After(hkDialBudget):
		return nil, fmt.Errorf("扩展行情源连接超时(%s)", hkDialBudget)
	}
}

// resetHKClient 丢弃当前扩展行情连接(连接假死/请求失败时), 下次请求重拨。
func resetHKClient() {
	if hkClient != nil {
		hkClient.CloseAll()
		hkClient = nil
	}
}

// hkQuotes 批量获取港股五档报价(逐只, 串行走扩展行情单连接)。
// codes 须已是港股代码(DecodeCode 判定为 ExchangeHK)。
func hkQuotes(codes []string) (protocol.QuotesResp, error) {
	hkMu.Lock()
	defer hkMu.Unlock()

	out := make(protocol.QuotesResp, 0, len(codes))
	for _, code := range codes {
		_, body, err := protocol.DecodeCode(code)
		if err != nil {
			return nil, err
		}
		q, err := hkQuoteOnce(body)
		if err != nil {
			return nil, fmt.Errorf("港股 %s 行情获取失败: %w", code, err)
		}
		out = append(out, q)
	}
	return out, nil
}

// hkQuoteOnce 单只港股五档, 请求失败时重拨一次再试
// (扩展行情连接可能因长时间空闲被服务端断开)。
func hkQuoteOnce(body string) (*protocol.Quote, error) {
	cli, err := getHKClient()
	if err != nil {
		return nil, err
	}
	q, err := cli.ExQuote(protocol.ExchangeHK.Uint8(), body)
	if err != nil {
		resetHKClient()
		if cli, err = getHKClient(); err != nil {
			return nil, err
		}
		if q, err = cli.ExQuote(protocol.ExchangeHK.Uint8(), body); err != nil {
			return nil, err
		}
	}
	return exQuoteToQuote(body, q), nil
}

// exQuoteToQuote 扩展行情报价 → 标准行情 Quote 结构,
// 输出字段与 /api/quote 的 A 股条目保持同一形状(价格为厘)。
func exQuoteToQuote(code string, q *protocol.ExQuote) *protocol.Quote {
	out := &protocol.Quote{
		Exchange: protocol.ExchangeHK,
		Code:     code,
		Kline: &protocol.Kline{
			Last:   protocol.Price(q.Price * 100),
			Open:   protocol.Price(q.Open * 100),
			High:   protocol.Price(q.High * 100),
			Low:    protocol.Price(q.Low * 100),
			Close:  protocol.Price(q.PreClose * 100),
			Volume: int64(q.ZongLiang),
			Time:   time.Now(),
		},
	}
	for i := 0; i < 5; i++ {
		out.BuyLevel[i] = protocol.PriceLevel{
			Buy:    true,
			Price:  protocol.Price(q.Bid[i] * 100),
			Number: int(q.BidVol[i]),
		}
		out.SellLevel[i] = protocol.PriceLevel{
			Price:  protocol.Price(q.Ask[i] * 100),
			Number: int(q.AskVol[i]),
		}
	}
	return out
}

// ---- 港股日K ----
//
// 港股没有 gbbq 数据(2026-10-14 实证: 标准协议 gbbq 对 hk00700 返回 count=0,
// gbbq 即 A 股除权除息机制), 故港股无前复权能力; 日线统一走 ExHq ExBars
// (category 9=日K, 不复权原始价)。/api/kline-history{,-ths,-qfq,-tdx} 对港股
// 统一分流到这里, 语义一致(不复权), 也堵住了短帧修复前"错票数据"的通道。

// isHKCode 是否港股代码(裸 5 位纯数字 / hk 前缀 / .HK 后缀)。
func isHKCode(code string) bool {
	ex, _, err := protocol.DecodeCode(code)
	return err == nil && ex == protocol.ExchangeHK
}

// hkKlineDayAll 港股日K全量(升序), ExHq 源, 不复权。
// ExBars 单次上限 800 根, start 递增向历史回溯; 分页边界可能重叠, 按日期去重。
func hkKlineDayAll(code string) ([]*protocol.Kline, error) {
	_, body, err := protocol.DecodeCode(code)
	if err != nil {
		return nil, err
	}

	hkMu.Lock()
	defer hkMu.Unlock()

	cli, err := getHKClient()
	if err != nil {
		return nil, err
	}

	const (
		pageSize = uint16(800)
		maxPages = 8 // 800×8 ≈ 25 年日K, 远超持仓跟踪所需
	)
	seen := map[string]bool{}
	out := []*protocol.Kline(nil)
	for page := 0; page < maxPages; page++ {
		bars, err := cli.ExBars(9, protocol.ExchangeHK.Uint8(), body, uint16(page) * pageSize, pageSize)
		if err != nil {
			// 连接可能已假死: 重拨一次再试本页
			resetHKClient()
			if cli, err = getHKClient(); err != nil {
				return nil, err
			}
			if bars, err = cli.ExBars(9, protocol.ExchangeHK.Uint8(), body, uint16(page) * pageSize, pageSize); err != nil {
				return nil, err
			}
		}
		for _, b := range bars {
			t, perr := time.ParseInLocation("2006-01-02 15:04", b.Datetime, time.Local)
			if perr != nil {
				continue
			}
			key := t.Format(time.DateOnly)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, &protocol.Kline{
				Time:   t,
				Open:   protocol.Price(b.Open * 100),
				High:   protocol.Price(b.High * 100),
				Low:    protocol.Price(b.Low * 100),
				Close:  protocol.Price(b.Close * 100),
				Last:   0, // 港股日K无昨收字段, 恒 0; 昨收请取前一交易日 Close
				Volume: int64(b.Trade),
				Amount: protocol.Price(b.Amount * 100),
			})
		}
		if len(bars) < int(pageSize) {
			break
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out, nil
}

// handleHKKlineHistory 港股历史K线(ExHq 源, 不复权)。
// day/week/month 统一由日K合成(week/month 复用 A 股转换函数);
// 响应形状与 A 股 kline-history 端点一致(KlineResp), 日期过滤语义相同。
func handleHKKlineHistory(w http.ResponseWriter, code, klineType string, start, end time.Time) {
	days, err := hkKlineDayAll(code)
	if err != nil {
		log.Printf("[港股] %s 日K获取失败: %v", code, err)
		errorResponse(w, fmt.Sprintf("获取K线失败: 港股 %s 日线获取失败: %v", code, err))
		return
	}
	if len(days) == 0 {
		errorResponse(w, "获取K线失败: 无数据")
		return
	}

	resp := &protocol.KlineResp{List: days, Count: uint16(len(days))}
	switch klineType {
	case "week":
		resp = convertToWeekKline(resp)
	case "month":
		resp = convertToMonthKline(resp)
	}

	filtered := make([]*protocol.Kline, 0, len(resp.List))
	for _, k := range resp.List {
		if k != nil && inDateRange(k.Time, start, end) {
			filtered = append(filtered, k)
		}
	}
	resp.List = filtered
	resp.Count = uint16(len(filtered))

	successResponse(w, resp)
}
