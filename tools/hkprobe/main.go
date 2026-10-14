package main

// hkprobe 港股行情诊断工具(临时, 不随服务部署):
// 验证 2026-10-14 下游移交单的「港股 quote 超时 + 连接污染」机制。
// 运行: go run ./tools/hkprobe
//
// 实验设计:
//  连接1: 畸形港股帧(market=31+5字节代码, 现网行为) → 紧接A股 → 单发1字节0x00 → 再发A股
//         (若 0x00 后A股立刻恢复, 证明服务端按固定结构解析、缺1字节导致流失步;
//          若 0x00 无效需等数秒, 则是服务端对畸形请求的连接级惩罚)
//  连接2: 补齐6字节(\x00填充)的港股帧 → 紧接A股 → 再发A股 (畸形消除后是否仍污染)
//  连接3: 补齐6字节(空格填充)的港股帧 → 同上

import (
	"fmt"
	"time"

	"github.com/injoyai/tdx"
	"github.com/injoyai/tdx/protocol"
)

func main() {
	fmt.Println("===== 连接1: 现网畸形帧 (market=31, code=5字节) =====")
	cli1 := dial()
	seqMalformed(cli1)
	cli1.CloseAll()

	fmt.Println("\n===== 连接2: 补齐6字节 \\x00 填充 =====")
	cli2 := dial()
	seqPadded(cli2, 0x00)
	cli2.CloseAll()

	fmt.Println("\n===== 连接3: 补齐6字节 空格填充 =====")
	cli3 := dial()
	seqPadded(cli3, 0x20)
	cli3.CloseAll()
}

func dial() *tdx.Client {
	cli, err := tdx.DialDefault()
	if err != nil {
		panic(err)
	}
	cli.SetTimeout(3 * time.Second)
	time.Sleep(300 * time.Millisecond) // 等握手完成
	return cli
}

// q 尝试取一只代码的行情, 打印结果与耗时
func q(cli *tdx.Client, tag, code string) {
	start := time.Now()
	quotes, err := cli.GetQuote(code)
	el := time.Since(start)
	if err != nil {
		fmt.Printf("  [%s] %-9s -> 失败(%v) %.3fs\n", tag, code, err, el.Seconds())
		return
	}
	if len(quotes) == 0 {
		fmt.Printf("  [%s] %-9s -> 空响应 %.3fs\n", tag, code, el.Seconds())
		return
	}
	fmt.Printf("  [%s] %-9s -> 成功 Code=%s Last=%s %.3fs\n", tag, code, quotes[0].Code, quotes[0].Kline.Last, el.Seconds())
}

// rawQuote 手工构建 quote 请求帧, code 按给定字节数原样嵌入(不补齐), 用于对照实验
func rawQuote(pairs ...[2]string) *protocol.Frame {
	payload := protocol.Bytes(uint16(len(pairs)))
	for _, p := range pairs {
		mkt, code := p[0], p[1]
		payload = append(payload, mkt[0])
		payload = append(payload, code...)
	}
	return &protocol.Frame{
		Control: protocol.Control01,
		Type:    protocol.TypeQuote,
		Data:    append([]byte{0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}, payload...),
	}
}

func sendRaw(cli *tdx.Client, tag string, f *protocol.Frame) {
	start := time.Now()
	resp, err := cli.SendFrame(f)
	el := time.Since(start)
	if err != nil {
		fmt.Printf("  [%s] raw(% X ...) -> 失败(%v) %.3fs\n", tag, f.Data[8:12], err, el.Seconds())
		return
	}
	fmt.Printf("  [%s] raw(% X ...) -> 响应 %T %.3fs\n", tag, f.Data[8:12], resp, el.Seconds())
}

func seqMalformed(cli *tdx.Client) {
	// A: 与现网一致的畸形帧(market=0x1F + 5字节代码, 每代码块6字节而非协议要求的7字节)
	sendRaw(cli, "A 畸形HK", rawQuote([2]string{string([]byte{0x1F}), "00100"}))
	// B: 干净的A股帧, 预期被污染
	sendRaw(cli, "B 紧接A股", rawQuote([2]string{string([]byte{0x00}), "002657"}))
	// C: 再来一帧干净A股, 预期流已重新对齐、恢复正常(污染只持续一帧)
	time.Sleep(500 * time.Millisecond)
	sendRaw(cli, "C 再发A股", rawQuote([2]string{string([]byte{0x00}), "002657"}))
}

func seqPadded(cli *tdx.Client, pad byte) {
	code6 := []byte{'0', '0', '1', '0', '0', pad}
	f := rawQuote([2]string{string([]byte{0x1F}), string(code6)})
	start := time.Now()
	resp, err := cli.SendFrame(f)
	el := time.Since(start)
	if err != nil {
		fmt.Printf("  [P1 填充HK] -> 失败(%v) %.3fs\n", err, el.Seconds())
	} else if quotes, ok := resp.(protocol.QuotesResp); !ok {
		fmt.Printf("  [P1 填充HK] -> 响应 %T %.3fs\n", resp, el.Seconds())
	} else {
		fmt.Printf("  [P1 填充HK] -> QuotesResp n=%d %.3fs\n", len(quotes), el.Seconds())
		for _, qt := range quotes {
			k := qt.Kline
			fmt.Printf("      mkt=%d code=%q last=%d open=%d high=%d low=%d vol=%d\n",
				qt.Exchange, qt.Code, k.Last, k.Open, k.High, k.Low, k.Volume)
			fmt.Printf("      卖五档: %v / %v / %v / %v / %v\n",
				qt.SellLevel[0].Price, qt.SellLevel[1].Price, qt.SellLevel[2].Price, qt.SellLevel[3].Price, qt.SellLevel[4].Price)
			fmt.Printf("      买五档: %v / %v / %v / %v / %v\n",
				qt.BuyLevel[0].Price, qt.BuyLevel[1].Price, qt.BuyLevel[2].Price, qt.BuyLevel[3].Price, qt.BuyLevel[4].Price)
		}
	}
	q(cli, "P2 紧接A股", "sz002657")
	time.Sleep(500 * time.Millisecond)
	q(cli, "P3 再发A股", "sz002657")
}
