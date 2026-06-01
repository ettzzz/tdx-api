package main

import (
	"fmt"

	"github.com/injoyai/logs"
	"github.com/injoyai/tdx"
	"github.com/injoyai/tdx/protocol"
)

// GetGbbq 演示如何通过 Client.GetGbbq 拉取单只股票的股本变迁 + 除权除息.
//
// 返回 *GbbqResp,其中每条 Gbbq 记录按 Category 分类:
//   - IsEquity() == true  股本变化(流通/总股本),用 Equity() 取
//   - IsXRXD()  == true  除权除息(分红/配股/送转),用 XRXD() 取
//
// 运行:
//   go run ./example/GetGbbq
func main() {
	c, err := tdx.DialDefault()
	if err != nil {
		logs.Err(err)
		return
	}
	defer c.Close()

	code := "sz000001"
	fullCode := protocol.AddPrefix(code)

	// Client.GetGbbq 内部会自动 AddPrefix,所以既可以传 sz000001 也可以传 sh600000
	resp, err := c.GetGbbq(code)
	if err != nil {
		logs.Err(err)
		return
	}

	fmt.Printf("股票 %s 共有 %d 条 gbbq 记录\n", fullCode, resp.Count)
	fmt.Println("------------------------------------------------------------")

	for _, g := range resp.List {
		if g == nil {
			continue
		}
		switch {
		case g.IsEquity():
			e := g.Equity()
			fmt.Printf("  [股本变化]  %s  流通=%d  总股本=%d  分类=%d\n",
				g.Time.Format("2006-01-02 15:04"),
				e.Float, e.Total, e.Category,
			)
		case g.IsXRXD():
			x := g.XRXD()
			fmt.Printf("  [除权除息]  %s  分红=%.2f  配股价=%.2f  送转股=%.2f  配股=%.2f  (每10股)\n",
				g.Time.Format("2006-01-02 15:04"),
				x.Fenhong, x.Peigujia, x.Songzhuangu, x.Peigu,
			)
		default:
			fmt.Printf("  [其他]      %s  分类=%d  C1=%.4f C2=%.4f C3=%.4f C4=%.4f\n",
				g.Time.Format("2006-01-02 15:04"),
				g.Category, g.C1, g.C2, g.C3, g.C4,
			)
		}
	}

	// 仅返回前 5 条
	show := 5
	if len(resp.List) < show {
		show = len(resp.List)
	}
	if show > 0 {
		fmt.Println()
		fmt.Println("演示前复权换算(取最近一条除权除息):")
		// 简单取最近一条 XRXD 演示
		var last *protocol.XRXD
		for _, g := range resp.List {
			if g.IsXRXD() {
				last = g.XRXD()
			}
		}
		if last != nil {
			// 假设某日昨收 10.00 元(10000 厘)
			demo := protocol.Price(10000)
			adj := last.Pre(demo)
			fmt.Printf("  假设昨收 10.00 元(10000 厘),除权后价 = %s (%.2f 元)\n",
				adj, adj.Float64())
		}
	}
}
