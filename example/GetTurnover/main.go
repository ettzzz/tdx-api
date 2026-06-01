package main

import (
	"fmt"

	"github.com/injoyai/logs"
	"github.com/injoyai/tdx"
)

// GetTurnover 演示如何结合 K 线与 gbbq 内存缓存计算个股换手率.
//
// 换手率 = 成交股数 / 流通股本 * 100%.
// K 线成交量单位是"手"(1 手 = 100 股),需要 ×100 转成"股"再参与计算.
//
// gbbq 管理器启动时会按 cron 表达式(DefaultGbbqSpec = 工作日 9:00/15:00)定时
// 拉取全市场股本变迁.首次启动若 Updated 节点未落库,Update() 会同步拉取全市场
// 数据(数千只股票,可能持续数分钟),本示例中不调用,直接读取内存缓存.
//
// 运行:
//   go run ./example/GetTurnover
func main() {
	c, err := tdx.DialDefault()
	if err != nil {
		logs.Err(err)
		return
	}
	defer c.Close()

	// 构造 gbbq 管理器.不显式传 *Client 时内部会自动 DialDefault,
	// 演示场景下复用同一个客户端即可(此时 Gbbq 不接管连接生命周期).
	gbbq, err := tdx.NewGbbq(tdx.WithGbbqClient(c))
	if err != nil {
		logs.Err(err)
		return
	}

	code := "sz000001"

	// 取全量日 K(不复权,换手率与价格无关).
	klines, err := c.GetKlineDayAll(code)
	if err != nil {
		logs.Err(err)
		return
	}
	if len(klines.List) == 0 {
		fmt.Println("无K线数据")
		return
	}

	// 演示 5 个交易日的换手率
	sample := klines.List
	if len(sample) > 5 {
		sample = sample[:5]
	}

	fmt.Printf("股票 %s 换手率演示 (取最近 %d 个交易日):\n", code, len(sample))
	fmt.Println("------------------------------------------------------------")
	for _, k := range sample {
		if k == nil {
			continue
		}
		// 取当日生效的流通股本
		eq := gbbq.GetEquity(code, k.Time)
		if eq == nil {
			fmt.Printf("  %s  无股本数据 (gbbq 缓存未同步)\n",
				k.Time.Format("2006-01-02"))
			continue
		}
		// K 线 Volume 单位为"手",需 ×100 转为"股"
		turnover := eq.Turnover(k.Volume * 100)
		fmt.Printf("  %s  成交量=%d手  流通股本=%d股  换手率=%.2f%%\n",
			k.Time.Format("2006-01-02"),
			k.Volume,
			eq.Float,
			turnover,
		)
	}
}
