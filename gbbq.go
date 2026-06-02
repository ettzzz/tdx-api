package tdx

import (
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/injoyai/logs"
	"github.com/injoyai/tdx/protocol"
	"xorm.io/xorm"
)

// IGbbq gbbq 查询对外接口
// 由 Gbbq 结构体实现,方便替换 mock 或其他实现
type IGbbq interface {
	GetEquity(code string, t time.Time) *protocol.Equity
	GetTurnover(code string, t time.Time, volume int64) float64
	GetXRXDs(code string) protocol.XRXDs
	GetFactors(code string, ks protocol.Klines) []*protocol.Factor
}

type GbbqOption func(s *Gbbq)

// WithGbbqDB 注入已准备好的 xorm 引擎
// 不传则默认在 DefaultDatabaseDir/gbbq.db 创建 sqlite 引擎
func WithGbbqDB(db *xorm.Engine) GbbqOption {
	return func(s *Gbbq) {
		s.db = db
	}
}

// WithGbbqClient 注入已建立好的 TDX 客户端
// 不传则 NewGbbq 内部自动 DialDefault
func WithGbbqClient(c *Client) GbbqOption {
	return func(s *Gbbq) {
		s.c = c
	}
}

// WithGbbqCodes 注入股票代码列表 (优先使用本地 codes 缓存)
// 不传则 Refresh(codes=nil) 时回退到 c.GetStockAll() (可能受 TDX 服务器限流影响返回 0)
func WithGbbqCodes(codes []string) GbbqOption {
	return func(s *Gbbq) {
		s.codes = append([]string(nil), codes...)
	}
}

// WithGbbqOption 组合多个 option
func WithGbbqOption(op ...GbbqOption) GbbqOption {
	return func(s *Gbbq) {
		for _, o := range op {
			if o != nil {
				o(s)
			}
		}
	}
}

// NewGbbq 构造 gbbq 管理器
// 流程:应用 options -> 初始化 client -> 初始化 db -> 同步表结构 -> 加载历史缓存到内存
// 注意:本函数不再启动 cron 自动更新,也不再触发网络拉取.
// 数据更新需要调用方显式调用 Refresh(codes),详细见 PLAN_v2 §3.
func NewGbbq(op ...GbbqOption) (*Gbbq, error) {
	s := &Gbbq{
		m: make(map[string][]*protocol.Gbbq),
	}
	WithGbbqOption(op...)(s)

	var err error

	// 初始化客户端
	if s.c == nil {
		s.c, err = DialDefault()
		if err != nil {
			return nil, err
		}
	}

	// 初始化数据库(sqlite)
	if s.db == nil {
		s.db, err = xorm.NewEngine("sqlite", filepath.Join(DefaultDatabaseDir, "gbbq.db"))
		if err != nil {
			return nil, err
		}
	}
	if err = s.db.Sync2(new(protocol.Gbbq)); err != nil {
		return nil, err
	}

	// 加载历史缓存到内存(秒级),为空时直接返回
	if old, lerr := s.loading(); lerr != nil {
		return nil, lerr
	} else if len(old) > 0 {
		s.sort(old)
		s.mu.Lock()
		s.m = old
		s.mu.Unlock()
		logs.Infof("gbbq 缓存已加载 %d 只股票历史记录", len(old))
	} else {
		logs.Infof("gbbq 缓存为空,数据按需拉取")
	}
	return s, nil
}

// Gbbq 股本变迁/除权除息管理器
// 内存缓存 code -> []Gbbq; 刷新节奏由调用方通过 Refresh() 触发,后台不再有 cron 自动拉取
type Gbbq struct {
	c  *Client
	db *xorm.Engine

	// codes 可选,股票代码列表 (优先使用本地 codes 缓存,避免 TDX 协议限流)
	codes []string

	// m 全市场 gbbq 内存缓存,key 为带前缀的代码 (例 sh600000)
	m  map[string][]*protocol.Gbbq
	mu sync.RWMutex
}

// All 返回全市场 gbbq 缓存的快照(浅拷贝,内部切片仍共享)
func (this *Gbbq) All() map[string][]*protocol.Gbbq {
	m := make(map[string][]*protocol.Gbbq)
	this.mu.RLock()
	defer this.mu.RUnlock()
	for k, v := range this.m {
		m[k] = v
	}
	return m
}

// GetEquity 获取指定时间点上生效的股本信息(流通/总股本)
// TDX 推送的时间是 15:00,通过 IntegerDay 归零为当日 00:00 与参数 t 比较
func (this *Gbbq) GetEquity(code string, t time.Time) *protocol.Equity {
	code = protocol.AddPrefix(code)
	this.mu.RLock()
	ls := this.m[code]
	this.mu.RUnlock()
	for i := len(ls) - 1; i >= 0; i-- {
		v := ls[i]
		// 读过来的股本变迁时间是 15:00,但当天就已生效,这里把小时归零方便比较
		if v.IsEquity() && t.Unix() >= IntegerDay(v.Time).Unix() {
			return v.Equity()
		}
	}
	return nil
}

// GetXRXDs 获取指定股票的全部除权除息记录
func (this *Gbbq) GetXRXDs(code string) protocol.XRXDs {
	code = protocol.AddPrefix(code)
	this.mu.RLock()
	ls := this.m[code]
	this.mu.RUnlock()
	res := protocol.XRXDs{}
	for _, v := range ls {
		if v.IsXRXD() {
			res = append(res, v.XRXD())
		}
	}
	return res
}

// GetXRXDMap 以日期(yyyy-MM-dd)为 key 返回 XRXD 字典,便于按日查表
func (this *Gbbq) GetXRXDMap(code string) map[string]*protocol.XRXD {
	code = protocol.AddPrefix(code)
	this.mu.RLock()
	ls := this.m[code]
	this.mu.RUnlock()
	res := map[string]*protocol.XRXD{}
	for _, v := range ls {
		if v.IsXRXD() {
			res[v.Time.Format(time.DateOnly)] = v.XRXD()
		}
	}
	return res
}

// GetFactors 计算一组 K 线的复权因子
// 入参 ks 必须按时间升序,内部会先排序再按日期匹配 XRXD
func (this *Gbbq) GetFactors(code string, ks protocol.Klines) []*protocol.Factor {
	return this.GetXRXDs(code).Pre(ks).Factors()
}

// GetTurnover 计算指定时间点指定成交量的换手率(%)
func (this *Gbbq) GetTurnover(code string, t time.Time, volume int64) float64 {
	x := this.GetEquity(code, t)
	if x == nil {
		return 0
	}
	return x.Turnover(volume)
}

// FetchOne 从 TDX 拉取单只股票的 gbbq 记录, 写入 db 与内存 map。
// 主要供 Refresh 内部使用,也允许库使用者主动补拉单只股票。
// 返回该股票最新的记录数。
func (this *Gbbq) FetchOne(code string) (int, error) {
	fullCode := protocol.AddPrefix(code)
	resp, err := this.c.GetGbbq(fullCode)
	if err != nil {
		return 0, err
	}
	// 写 db (只删该 code 的旧记录, 再插入新的, 事务化)
	if err := NewSessionFunc(this.db, func(session *xorm.Session) error {
		if _, err := session.Where("code=?", fullCode).Delete(new(protocol.Gbbq)); err != nil {
			return err
		}
		for _, v := range resp.List {
			if _, err := session.Insert(v); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	// 刷新内存 map: 删旧加新
	this.mu.Lock()
	delete(this.m, fullCode)
	this.m[fullCode] = resp.List
	this.mu.Unlock()
	return len(resp.List), nil
}

// Refresh 主动触发 gbbq 更新(替代原 Update).
// codes: 要刷新的股票代码列表; 支持以下写法:
//   - 带前缀: "sh600000" / "sz000001"
//   - 不带前缀: "600000" / "000001"(内部 AddPrefix 补前缀)
//   - 大小写不敏感
//
// 为空/为 nil = 全量(从 this.codes 读,由 WithGbbqCodes 注入;未注入时回退到 c.GetStockAll())
// 返回值:
//   - success: 成功刷新的股票代码列表(统一带前缀小写)
//   - failed:  code -> 错误信息(不阻塞后续股票)
//   - err:     仅在初始化阶段(GetStockAll 失败等)才返回
//
// 注意: 单只股票拉取失败不影响其他股票,符合"宽松模式"语义
func (this *Gbbq) Refresh(codes []string) (success []string, failed map[string]error, err error) {
	failed = make(map[string]error)

	// codes 为空/为 nil 时,回退到 this.codes(由 WithGbbqCodes 注入);都没有再走 GetStockAll
	if len(codes) == 0 {
		if len(this.codes) == 0 {
			listed, gerr := this.c.GetStockAll()
			if gerr != nil {
				return nil, failed, gerr
			}
			codes = listed
		} else {
			codes = this.codes
		}
	}

	total := len(codes)
	logs.Infof("[gbbq.Refresh] 开始拉取 %d 只股票的 gbbq 数据", total)

	success = make([]string, 0, total)
	for i, code := range codes {
		fullCode := protocol.AddPrefix(code)
		if _, qerr := this.FetchOne(fullCode); qerr != nil {
			logs.Warnf("[gbbq.Refresh] 拉取 %s 失败: %v (已拉取 %d/%d)", fullCode, qerr, i, total)
			failed[fullCode] = qerr
			continue
		}
		success = append(success, fullCode)
		if (i+1)%100 == 0 || i+1 == total {
			logs.Infof("[gbbq.Refresh] 进度 %d/%d (成功=%d, 失败=%d)", i+1, total, len(success), len(failed))
		}
	}
	logs.Infof("[gbbq.Refresh] 完成, 共 %d 只 (成功=%d, 失败=%d)", total, len(success), len(failed))
	return success, failed, nil
}

// sort 按时间升序排序每个股票的 gbbq 列表
func (this *Gbbq) sort(m map[string][]*protocol.Gbbq) {
	for _, v := range m {
		sort.Slice(v, func(i, j int) bool {
			return v[i].Time.Before(v[j].Time)
		})
	}
}

// loading 从 sqlite 加载全市场 gbbq,返回 code -> []Gbbq
func (this *Gbbq) loading() (map[string][]*protocol.Gbbq, error) {
	list := []*protocol.Gbbq(nil)
	if err := this.db.Asc("Time").Find(&list); err != nil {
		return nil, err
	}
	m := map[string][]*protocol.Gbbq{}
	for _, v := range list {
		m[v.Code] = append(m[v.Code], v)
	}
	return m, nil
}
