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

const (
	// DefaultGbbqSpec gbbq 默认更新 cron 表达式
	// 工作日 9:00 / 15:00 各更新一次 (与 TDX 复权数据发布节奏对齐)
	DefaultGbbqSpec = "0 0 9,15 * * 1-5"

	// DefaultRetry gbbq 定时任务单次执行失败时的重试次数
	DefaultRetry = 3
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

// WithGbbqRetry 设置定时任务失败重试次数
func WithGbbqRetry(retry int) GbbqOption {
	return func(s *Gbbq) {
		s.retry = retry
	}
}

// WithGbbqSpec 设置 cron 表达式
func WithGbbqSpec(spec string) GbbqOption {
	return func(s *Gbbq) {
		s.spec = spec
	}
}

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
// 不传则回退到 c.GetStockAll() (可能受 TDX 服务器限流影响返回 0)
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
// 流程:应用 options -> 初始化 client -> 初始化 db -> 同步表结构 -> 启动定时器
func NewGbbq(op ...GbbqOption) (*Gbbq, error) {
	s := &Gbbq{
		spec:      DefaultGbbqSpec,
		retry:     DefaultRetry,
		updateKey: "gbbq",
		m:         make(map[string][]*protocol.Gbbq),
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

	// 初始化 Updated (用于判断节点时间是否需要拉取)
	s.updated, err = NewUpdated(s.db, 9, 0)
	if err != nil {
		return nil, err
	}

	// 启动定时器(立即执行一次,失败按 retry 重试)
	err = NewTimer(s.spec, s.retry, s)
	return s, err
}

// Gbbq 股本变迁/除权除息管理器
// 内存缓存 code -> []Gbbq,后台定时从 TDX 拉取并写入 sqlite
type Gbbq struct {
	spec      string
	retry     int
	updateKey string

	c       *Client
	db      *xorm.Engine
	updated *Updated

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

// Update 触发一次完整更新
// 流程:从 db 加载旧数据 -> 检查 Updated 节点 -> 必要时从 TDX 拉取 -> 写回 db -> 刷新内存
func (this *Gbbq) Update() error {
	old, err := this.loading()
	if err != nil {
		return err
	}
	this.sort(old)

	this.mu.Lock()
	this.m = old
	this.mu.Unlock()

	// Updated 节点判断:已更新过则跳过拉取
	updated, err := this.updated.Updated(this.updateKey)
	if err != nil {
		return err
	}
	if updated {
		return nil
	}

	_new, err := this.update()
	if err != nil {
		return err
	}
	this.sort(_new)

	this.mu.Lock()
	this.m = _new
	this.mu.Unlock()
	return nil
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

// update 从 TDX 拉取全市场 gbbq,事务化覆盖写回 sqlite,更新 Updated 时间戳
func (this *Gbbq) update() (map[string][]*protocol.Gbbq, error) {
	// 优先用本地 codes 缓存 (避免 TDX 协议限流返回 0)
	codes := this.codes
	var err error
	if len(codes) == 0 {
		codes, err = this.c.GetStockAll()
		if err != nil {
			return nil, err
		}
	}
	logs.Infof("开始拉取 %d 只股票的 gbbq 数据", len(codes))
	gbbqs := map[string][]*protocol.Gbbq{}
	var resp *protocol.GbbqResp
	for i, code := range codes {
		resp, err = this.c.GetGbbq(code)
		if err != nil {
			logs.Warnf("拉取 %s 失败: %v (已拉取 %d/%d)", code, err, i, len(codes))
			return nil, err
		}
		gbbqs[code] = resp.List
	}
	logs.Infof("gbbq 拉取完成,共 %d 只股票", len(gbbqs))
	err = NewSessionFunc(this.db, func(session *xorm.Session) error {
		// 全量替换:先清空再批量插入
		if _, err = session.Where("1=1").Delete(new(protocol.Gbbq)); err != nil {
			return err
		}
		for _, ls := range gbbqs {
			for _, v := range ls {
				if _, err = session.Insert(v); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err = this.updated.Update(this.updateKey); err != nil {
		return gbbqs, err
	}
	return gbbqs, nil
}
