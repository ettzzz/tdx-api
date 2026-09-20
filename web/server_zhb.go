package main

// zhb 盘后数据缓存: 统一管理 zhb.zip 配置包、block_*.dat 板块文件与 tdxhy.cfg 行业归属的
// 下载、解析与内存缓存。/api/blocks、/api/tdx-stat、/api/tdx-stat2、/api/tdx-hy 四个端点
// 共享同一份缓存; POST /api/blocks/refresh 强制刷新。
//
// 刷新语义(盘后数据每交易日一份): 缓存数据日期 statDate(取 tdxstat.cfg 行内 Date 最大值)
// 不等于「最近一个已收盘交易日」时判定过期, 下一次 GET 自动重新下载, mutex 双检查防并发击穿。
// 全量刷新 = zhb.zip(实测约2s, 4.4MB) + 4 个板块文件 + tdxhy.cfg(实测约0.25s),
// 单连接顺序执行总计约 6-8s, 客户端超时建议 ≥30s。
// 宽松模式: 单个数据源失败不阻断整体, 失败原因记录在 snapshot.errors 随响应透出,
// 且旧快照保留继续服务(旧数据好过无数据)。
//
// 成分码编码实测(2026-09-20, 沪深300 抽样): block_*.dat 为 6 位纯代码;
// spblock.dat 为 7 位"市场标志+代码"(如 0000009=深000009), 出口统一归一为 6 位。

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/injoyai/tdx"
	"github.com/injoyai/tdx/protocol"
)

// zhbBlockTypes 板块文件清单与展示顺序(block_zs.dat 无指数代码, 置末尾)。
var zhbBlockTypes = []struct {
	key      string // /api/blocks?type= 参数值
	file     string // 通达信文件名
	typeName string // 中文类型名
}{
	{"gn", protocol.BlockFileGN, "概念板块"},
	{"hy", protocol.BlockFileHY, "行业板块"},
	{"fg", protocol.BlockFileFG, "风格地域板块"},
	{"zs", protocol.BlockFileZS, "指数板块"},
}

// zhbBlockTypeNames ?type= 合法值集合(含 sp/spblock.dat 与 sw/申万合成板块)。
var zhbBlockTypeNames = map[string]bool{"gn": true, "hy": true, "fg": true, "zs": true, "sp": true, "sw": true}

// zhbSnapshot 一次刷新产出的不可变快照, 读端经 atomic.Pointer 无锁获取。
type zhbSnapshot struct {
	fetchedAt time.Time                    // 刷新完成时刻
	statDate  string                       // 数据日期 YYYYMMDD(tdxstat.cfg 行内 Date 最大值)
	files     map[string][]byte            // zhb.zip 解压条目原始字节(备未来扩展解析)
	blocks    map[string][]*protocol.Block // 板块, key: gn/hy/fg/zs
	sp        []*protocol.SpBlock          // 专业板块(中证2000/1000/500 等, 来自 zhb.zip)
	sw        []*protocol.Block            // 申万行业板块(881 系, 由 tdxzs3.cfg × tdxhy.SwHy 合成)
	stat      []*protocol.TdxStat          // 全市场盘后统计(tdxstat.cfg)
	stat2     []*protocol.TdxStat2         // 全市场资金流向+板块归属(tdxstat2.cfg)
	hy        []*protocol.TdxHy            // 全市场行业归属(tdxhy.cfg)
	errors    map[string]string            // 各数据源最近一次刷新的失败原因(空 map=全部成功)
}

var (
	zhbPtr        atomic.Pointer[zhbSnapshot] // 当前快照, nil=尚未加载
	zhbMu         sync.Mutex                  // 刷新互斥(防并发击穿)
	zhbFieldStat  = tdxStatFieldNames()       // tdxstat.cfg 35 槽位说明(见下)
	zhbFieldStat2 = tdxStat2FieldNames()      // tdxstat2.cfg 21 槽位说明
)

// zhbGet 获取缓存快照; 过期(force=false)或强制(force=true)时同步刷新。
func zhbGet(force bool) (*zhbSnapshot, error) {
	if !force {
		if snap := zhbPtr.Load(); snap != nil && snap.statDate == zhbExpectedDate() {
			return snap, nil
		}
	}
	zhbMu.Lock()
	defer zhbMu.Unlock()
	// 双检查: 等锁期间可能已被并发请求刷新
	if !force {
		if snap := zhbPtr.Load(); snap != nil && snap.statDate == zhbExpectedDate() {
			return snap, nil
		}
	}
	snap, err := zhbRefresh()
	if err != nil {
		// 全量失败(如 zhb.zip 下载失败): 保留旧快照继续服务
		if old := zhbPtr.Load(); old != nil {
			return old, fmt.Errorf("刷新失败, 返回旧缓存(%s): %w", old.statDate, err)
		}
		return nil, err
	}
	zhbPtr.Store(snap)
	return snap, nil
}

// zhbRefresh 执行一次全量下载+解析, 产出新快照(调用方持有 zhbMu)。
func zhbRefresh() (*zhbSnapshot, error) {
	snap := &zhbSnapshot{errors: map[string]string{}}
	start := time.Now()
	err := manager.Do(func(c *tdx.Client) error {
		// 1. zhb.zip 总包(一次下载, 下列配置文件均在其中)
		files, err := c.GetZHBFiles()
		if err != nil {
			return fmt.Errorf("下载 zhb.zip: %w", err)
		}
		snap.files = files
		zs := protocol.ParseTdxZs(files[protocol.FileTdxZs])
		bk := protocol.ParseTdxBk(files[protocol.FileTdxBk])

		// 2. 全市场统计/资金/专业板块(均来自 zhb.zip, 解析为内存操作)
		if data, ok := files[protocol.FileTdxStat]; ok {
			snap.stat = protocol.ParseTdxStat(data)
			snap.statDate = zhbMaxStatDate(snap.stat)
		} else {
			snap.errors["tdxstat"] = "zhb.zip 中缺少 " + protocol.FileTdxStat
		}
		if data, ok := files[protocol.FileTdxStat2]; ok {
			snap.stat2 = protocol.ParseTdxStat2(data)
		} else {
			snap.errors["tdxstat2"] = "zhb.zip 中缺少 " + protocol.FileTdxStat2
		}
		if data, ok := files[protocol.FileSpBlock]; ok {
			snap.sp = protocol.ParseSpBlock(data)
		} else {
			snap.errors["sp"] = "zhb.zip 中缺少 " + protocol.FileSpBlock
		}

		// 3. 板块文件(独立于 zhb.zip 逐个下载, 行业板块部分服务器不提供, 宽松处理)
		snap.blocks = map[string][]*protocol.Block{}
		for _, t := range zhbBlockTypes {
			raw, err := c.GetBlockFileRaw(t.file)
			if err != nil {
				snap.errors[t.key] = err.Error()
				continue
			}
			bs := protocol.ParseBlockFile(raw)
			protocol.FillBlockIndexAlias(bs, zs, bk) // 板块名 →(tdxzs/tdxbk)→ 880xxx 指数代码
			snap.blocks[t.key] = bs
		}

		// 4. 行业归属(tdxhy.cfg, 独立下载, 不在 zhb.zip 内)
		rawHy, err := c.GetBlockFileRaw(protocol.FileTdxHy)
		if err != nil {
			snap.errors["tdxhy"] = err.Error()
		} else {
			snap.hy = protocol.ParseTdxHy(rawHy)
		}

		// 5. 行业板块合成: block_hy.dat 当前各地域服务器均不再提供(2026-09-20 实测 6 台全部无数据,
		//    上游"部分服务器提供"的注释已过时)。改由 tdxzs.cfg(Type=2 条目的 Ref 即通达信行业 T 码)
		//    与 tdxhy.cfg(个股 T 码, 层级编码) 在服务端前缀匹配合成, 成分覆盖率 100%。
		if len(snap.blocks["hy"]) == 0 {
			snap.blocks["hy"] = zhbSynthHyBlocks(zs, snap.hy)
			delete(snap.errors, "hy")
		}

		// 6. 申万行业板块合成(881 系): tdxzs3.cfg 中代码 881xxx 且 Ref 为 X 码(申万行业, 层级
		//    编码 X一级/X二级/X三级)的条目 × tdxhy.cfg 个股 SwHy 前缀匹配。
		//    实测 467 板块(一级30/二级128/三级309), 成分命中 98.4%(未命中为个别空 SwHy)。
		if zs3Data, ok := files[protocol.FileTdxZs3]; ok {
			snap.sw = zhbSynthSwBlocks(protocol.ParseTdxZs(zs3Data), snap.hy)
		} else {
			snap.errors["sw"] = "zhb.zip 中缺少 " + protocol.FileTdxZs3
		}
		return nil
	})
	snap.fetchedAt = time.Now()
	if err != nil {
		return nil, err
	}
	log.Printf("[zhb] 盘后缓存刷新完成: statDate=%s 耗时=%v 错误源=%d",
		snap.statDate, time.Since(start).Round(time.Millisecond), len(snap.errors))
	return snap, nil
}

// zhbSynthHyBlocks 合成行业板块成分: tdxzs.cfg 中 Ref 为 T 码的条目即行业板块(如
// 煤炭|880301|ref=T0101), 个股 TdxHy.T 码以 Ref 为前缀(含相等)即为其成员——T 码为层级编码,
// 三级行业(T010101)自动归入一/二级行业(T0101/T0101xx)。
func zhbSynthHyBlocks(zs []*protocol.TdxZs, hy []*protocol.TdxHy) []*protocol.Block {
	type plate struct {
		z     *protocol.TdxZs
		codes []string
	}
	plates := make([]*plate, 0, 160)
	idx := map[string]*plate{} // ref(T码) → 板块
	for _, z := range zs {
		if len(z.Ref) < 2 || z.Ref[0] != 'T' {
			continue
		}
		p := &plate{z: z}
		plates = append(plates, p)
		idx[z.Ref] = p
	}
	for _, v := range hy {
		if len(v.TdxHy) < 2 {
			continue
		}
		for ref, p := range idx {
			if strings.HasPrefix(v.TdxHy, ref) {
				p.codes = append(p.codes, v.Code)
			}
		}
	}
	out := make([]*protocol.Block, 0, len(plates))
	for _, p := range plates {
		out = append(out, &protocol.Block{
			Name:  p.z.Name,
			Index: p.z.Code, // 880xxx, 直接来自 tdxzs.cfg
			Type:  p.z.Type,
			Codes: p.codes,
		})
	}
	return out
}

// zhbSynthSwBlocks 合成申万行业板块(881 系): tdxzs3.cfg 中代码 881xxx 且 Ref 为 X 码的条目
// 即申万板块(如 煤炭|881001|ref=X10), 个股 SwHy 以 Ref 为前缀(含相等)即为其成员——
// X 码为层级编码, 三级行业(X100101)自动归入一级/二级行业(X10/X1001)。
func zhbSynthSwBlocks(zs []*protocol.TdxZs, hy []*protocol.TdxHy) []*protocol.Block {
	type plate struct {
		z     *protocol.TdxZs
		codes []string
	}
	plates := make([]*plate, 0, 500)
	idx := map[string]*plate{} // ref(X码) → 板块
	for _, z := range zs {
		if len(z.Code) < 3 || z.Code[:3] != "881" || len(z.Ref) < 2 || z.Ref[0] != 'X' {
			continue
		}
		p := &plate{z: z}
		plates = append(plates, p)
		idx[z.Ref] = p
	}
	for _, v := range hy {
		if len(v.SwHy) < 2 {
			continue
		}
		for ref, p := range idx {
			if strings.HasPrefix(v.SwHy, ref) {
				p.codes = append(p.codes, v.Code)
			}
		}
	}
	out := make([]*protocol.Block, 0, len(plates))
	for _, p := range plates {
		out = append(out, &protocol.Block{
			Name:  p.z.Name,
			Index: p.z.Code, // 881xxx, 直接来自 tdxzs3.cfg
			Type:  p.z.Type,
			Codes: p.codes,
		})
	}
	return out
}

// zhbExpectedDate 最近一个已收盘交易日的日期(YYYYMMDD)。
// 16:00 为收盘数据就绪线: 当日 16:00 前最新数据仍属上一交易日。
// 优先用 Workday 交易日历(长假正确), 不可用时回退自然日规则(周一~周五)。
func zhbExpectedDate() string {
	now := time.Now()
	for i := 0; i < 20; i++ { // 最多回看 20 天(春节等长假兜底)
		d := now.AddDate(0, 0, -i)
		if i == 0 && now.Before(time.Date(d.Year(), d.Month(), d.Day(), 16, 0, 0, 0, d.Location())) {
			continue // 当日尚未收盘
		}
		if manager != nil && manager.Workday != nil {
			if manager.Workday.Is(d) {
				return d.Format("20060102")
			}
			continue
		}
		if wd := d.Weekday(); wd >= time.Monday && wd <= time.Friday {
			return d.Format("20060102")
		}
	}
	return ""
}

// zhbMaxStatDate 取 stat 行内 Date 最大值(个别行 Date 可能为空)。
func zhbMaxStatDate(stat []*protocol.TdxStat) string {
	max := ""
	for _, s := range stat {
		if s.Date > max {
			max = s.Date
		}
	}
	return max
}

// tdxStatFieldNames tdxstat.cfg 35 个原始字段槽位说明(0 基)。
// 已核验槽位对应 protocol.TdxStat 结构注释(protocol/model_stat.go), 其余为未命名/未核验。
func tdxStatFieldNames() []string {
	names := make([]string, 35)
	for i := range names {
		names[i] = fmt.Sprintf("未知字段[%d]", i)
	}
	known := map[int]string{
		1:  "market(市场 0=深 1=沪 2=京)",
		2:  "code(证券代码)",
		4:  "pettm(市盈率TTM, 已核验)",
		5:  "date(数据日期)",
		6:  "trend_days(连涨连跌天数, 已核验)",
		7:  "change_pct(涨跌幅%, 已核验)",
		8:  "未知(实测非短周期涨跌幅)",
		10: "pe_static(静态市盈率, 已核验)",
		11: "div_yield(股息率%, 通达信口径, 已核验)",
		14: "未知(实测非成交量/额/市值)",
		18: "chg20(20日涨跌幅%, 已核验)",
		20: "chg60(60日涨跌幅%, 已核验)",
		21: "chg_ytd(年初至今涨跌幅%, 已核验)",
		24: "未知(实测非成交量/额/市值)",
		27: "近5根涨跌幅(含当日变体)",
		28: "chg5(5日涨跌幅%, 已核验)",
		29: "近10根涨跌幅(含当日变体)",
		30: "chg10(10日涨跌幅%, 已核验)",
	}
	for i, name := range known {
		names[i] = name
	}
	return names
}

// tdxStat2FieldNames tdxstat2.cfg 21 个原始字段槽位说明(0 基), 对应 protocol.TdxStat2 注释。
func tdxStat2FieldNames() []string {
	names := make([]string, 21)
	for i := range names {
		names[i] = fmt.Sprintf("未知字段[%d]", i)
	}
	known := map[int]string{
		0:  "market(市场)",
		1:  "code(证券代码)",
		2:  "date(数据日期)",
		3:  "amount_today(今日成交额万元, 已核验)",
		5:  "amount_prev(昨日成交额万元, 已核验)",
		13: "block_index(所属/领涨板块指数代码, 已核验)",
		16: "ipo_price(IPO发行价, 已核验)",
		17: "high_52w(52周最高价, 已核验)",
		18: "low_52w(52周最低价, 已核验)",
	}
	for i, name := range known {
		names[i] = name
	}
	return names
}

// zhbStripMarketCode 剥离 7 位"市场标志+代码"的首位市场字符, 归一为 6 位代码。
// block_*.dat 已是 6 位(直接返回); spblock.dat 为 7 位(去首位)。
func zhbStripMarketCode(code string) string {
	if len(code) == 7 {
		return code[1:]
	}
	return code
}

// zhbBlockTypeName 返回板块类型中文名(未知类型回退原始值)。
func zhbBlockTypeName(key string) string {
	switch key {
	case "sp":
		return "专业板块"
	case "sw":
		return "申万行业"
	}
	for _, t := range zhbBlockTypes {
		if t.key == key {
			return t.typeName
		}
	}
	return key
}

// zhbQueryTypes 解析 ?type= 参数, 空=全量。非法值返回错误信息。
func zhbQueryTypes(param string) (map[string]bool, string) {
	param = strings.TrimSpace(param)
	if param == "" {
		return nil, ""
	}
	want := map[string]bool{}
	for _, k := range strings.Split(param, ",") {
		k = strings.ToLower(strings.TrimSpace(k))
		if !zhbBlockTypeNames[k] {
			return nil, fmt.Sprintf("未知板块类型 %q, 可选: gn,hy,fg,zs,sp", k)
		}
		want[k] = true
	}
	return want, ""
}
