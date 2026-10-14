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
