package tdx

import (
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/injoyai/logs"
	"github.com/injoyai/tdx/lib/xorms"
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

type (
	GbbqOption func(e *Gbbq)

	DialGbbqFunc func(c *Client) (IGbbq, error)
)

func WithGbbqRetry(retry int) GbbqOption {
	return func(s *Gbbq) {
		s.retry = retry
	}
}

func WithGbbqSpec(spec string) GbbqOption {
	return func(s *Gbbq) {
		s.spec = spec
	}
}

func WithGbbqDB(db *xorms.Engine) GbbqOption {
	return func(s *Gbbq) {
		s.db = db
	}
}

func WithGbbqDialDB(dial func() (*xorms.Engine, error)) GbbqOption {
	return func(s *Gbbq) {
		s.dialDB = dial
	}
}

func WithGbbqClient(c *Client) GbbqOption {
	return func(s *Gbbq) {
		s.c = c
	}
}

func WithGbbqDialClient(dial DialClientFunc) GbbqOption {
	return func(s *Gbbq) {
		s.dialClient = dial
	}
}

// WithGbbqCodes 注入股票代码列表 (优先使用本地 codes 缓存)
// 不传则 Refresh(codes=nil) 时回退到 c.GetStockCodeAll() (可能受 TDX 服务器限流影响返回 0)
func WithGbbqCodes(codes []string) GbbqOption {
	return func(s *Gbbq) {
		s.codes = append([]string(nil), codes...)
	}
}

func WithGbbqOption(op ...GbbqOption) GbbqOption {
	return func(s *Gbbq) {
		for _, o := range op {
			if o != nil {
				o(s)
			}
		}
	}
}

// NewGbbq 构造 gbbq 管理器 (上游骨架 + 本地懒加载语义)
// 流程: 应用 options -> 初始化 client -> 初始化 db -> 同步表结构 -> 初始化 updated 节点
//       -> 从 sqlite 加载历史缓存到内存
// 注意: 本函数不触发任何网络拉取, 也不挂 cron 定时任务 (与上游 NewTimer 自动更新不同).
// 全量更新可手动调 Update() (GetGbbqAll 通道); 宽松逐只更新用 Refresh(codes).
func NewGbbq(op ...GbbqOption) (*Gbbq, error) {
	s := &Gbbq{
		spec:      DefaultGbbqSpec,
		retry:     DefaultRetry,
		updateKey: "gbbq",
		dialDB:    nil,
		m:         make(map[string][]*protocol.Gbbq),
	}

	WithGbbqOption(op...)(s)

	var err error

	//初始化客户端
	if s.c == nil {
		if s.dialClient == nil {
			s.dialClient = func() (*Client, error) { return DialDefault() }
		}
		s.c, err = s.dialClient()
		if err != nil {
			return nil, err
		}
	}

	// 初始化数据库
	if s.db == nil {
		if s.dialDB == nil {
			s.dialDB = func() (*xorms.Engine, error) {
				return xorms.NewSqlite(filepath.Join(DefaultDatabaseDir, "gbbq.db"))
			}
		}
		s.db, err = s.dialDB()
		if err != nil {
			return nil, err
		}
	}
	if err = s.db.Sync2(new(protocol.Gbbq)); err != nil {
		return nil, err
	}
	// updated 节点仅服务 Update() 的"当天已更新则跳过"判断; 不挂 NewTimer (启动零网络)
	s.updated, err = NewUpdated(s.db, 9, 0)
	if err != nil {
		return nil, err
	}

	// 懒加载: 从 sqlite 加载历史缓存到内存(秒级), 为空时直接返回不拉网络
	if old, lerr := s.loading(); lerr != nil {
		return nil, lerr
	} else if len(old) > 0 {
		s.sort(old)
		s.mu.Lock()
		s.m = old
		s.mu.Unlock()
		logs.Infof("gbbq 缓存已加载 %d 只股票历史记录\n", len(old))
	} else {
		logs.Infof("gbbq 缓存为空, 数据按需拉取(POST /api/gbbq/refresh)\n")
	}

	return s, nil
}

// Gbbq 股本变迁/除权除息管理器
// 内存缓存 code -> []Gbbq; 更新节奏由调用方通过 Update()/Refresh() 显式触发
type Gbbq struct {
	spec       string
	retry      int
	updateKey  string
	dialDB     DialDBFunc
	dialClient DialClientFunc

	c       *Client
	db      *xorms.Engine
	updated *Updated

	// codes 可选,股票代码列表 (优先使用本地 codes 缓存,避免 TDX 协议限流)
	codes []string

	// m 全市场 gbbq 内存缓存,key 为带前缀的代码 (例 sh600000)
	m  map[string][]*protocol.Gbbq
	mu sync.RWMutex
}

// IsEmpty 判断是否为未初始化的空实现（NewManage 默认塞入的 &Gbbq{}）。
// 空实现的 GetEquity/GetTurnover 等恒返回零值，与"已初始化但无数据"不同。
func (this *Gbbq) IsEmpty() bool {
	if this == nil {
		return true
	}
	return this.db == nil && this.c == nil && this.m == nil
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
		//读取过来的是15:00,但是今天就生效了,把小时归零,方便判断
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

// GetFactors 计算一组 K 线的复权因子(上游仿射模型: QFQMul/QFQAdd/HFQMul/HFQAdd)
// 入参 ks 必须按时间升序,内部会先排序再按日期匹配 XRXD
func (this *Gbbq) GetFactors(code string, ks protocol.Klines) []*protocol.Factor {
	return this.GetXRXDs(code).Pre(ks).Factors()
}

// QFQ 把已获取的不复权日线 ks 转为前复权日线(对齐通达信桌面端, 四舍五入到分)。
// 已有 K 线时用此方法; 若要一步到位拉取+复权用 QFQKlineDay。
func (this *Gbbq) QFQ(code string, ks protocol.Klines) protocol.Klines {
	return protocol.ApplyQFQ(ks, this.GetFactors(code, ks))
}

// HFQ 把已获取的不复权日线转为后复权日线。见 QFQ。
func (this *Gbbq) HFQ(code string, ks protocol.Klines) protocol.Klines {
	return protocol.ApplyHFQ(ks, this.GetFactors(code, ks))
}

// QFQKlineDay 一站式获取前复权日线(全量历史): 拉取不复权日K + 前复权(对齐通达信)。
func (this *Gbbq) QFQKlineDay(code string) (protocol.Klines, error) {
	resp, err := this.c.GetKlineDayAll(code)
	if err != nil {
		return nil, err
	}
	return this.QFQ(code, resp.List), nil
}

// HFQKlineDay 一站式获取后复权日线(全量历史)。见 QFQKlineDay。
func (this *Gbbq) HFQKlineDay(code string) (protocol.Klines, error) {
	resp, err := this.c.GetKlineDayAll(code)
	if err != nil {
		return nil, err
	}
	return this.HFQ(code, resp.List), nil
}

// GetTurnover 计算指定时间点指定成交量的换手率(%)
func (this *Gbbq) GetTurnover(code string, t time.Time, volume int64) float64 {
	x := this.GetEquity(code, t)
	if x == nil {
		return 0
	}
	return x.Turnover(volume)
}

// Update 全量更新(上游语义): GetGbbqAll 一次性拉取全市场 -> 清库重插。
// 当天已更新过(9:00 节点)则只重载缓存不拉网络。
// 注意: 该通道依赖 TDX 服务器代码列表, 可能受协议限流影响; 宽松逐只更新用 Refresh()。
func (this *Gbbq) Update() error {
	old, err := this.loading()
	if err != nil {
		return err
	}

	this.sort(old)
	this.mu.Lock()
	this.m = old
	this.mu.Unlock()

	updated, err := this.updated.Updated(this.updateKey)
	if err == nil && updated {
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
	if err := this.db.SessionFunc(func(session *xorm.Session) error {
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

// Refresh 主动触发 gbbq 更新(本地宽松模式, 与上游 Update() 的全量通道互补).
// codes: 要刷新的股票代码列表; 支持以下写法:
//   - 带前缀: "sh600000" / "sz000001"
//   - 不带前缀: "600000" / "000001"(内部 AddPrefix 补前缀)
//   - 大小写不敏感
//
// 为空/为 nil = 全量(从 this.codes 读,由 WithGbbqCodes 注入;未注入时回退到 c.GetStockCodeAll())
// 返回值:
//   - success: 成功刷新的股票代码列表(统一带前缀小写)
//   - failed:  code -> 错误信息(不阻塞后续股票)
//   - err:     仅在初始化阶段(代码列表获取失败等)才返回
//
// 注意: 单只股票拉取失败不影响其他股票,符合"宽松模式"语义
func (this *Gbbq) Refresh(codes []string) (success []string, failed map[string]error, err error) {
	failed = make(map[string]error)

	// codes 为空/为 nil 时,回退到 this.codes(由 WithGbbqCodes 注入);都没有再走 GetStockCodeAll
	if len(codes) == 0 {
		if len(this.codes) == 0 {
			listed, gerr := this.c.GetStockCodeAll()
			if gerr != nil {
				return nil, failed, gerr
			}
			codes = listed
		} else {
			codes = this.codes
		}
	}

	total := len(codes)
	logs.Infof("[gbbq.Refresh] 开始拉取 %d 只股票的 gbbq 数据\n", total)

	success = make([]string, 0, total)
	for i, code := range codes {
		fullCode := protocol.AddPrefix(code)
		if _, qerr := this.FetchOne(fullCode); qerr != nil {
			logs.Warnf("[gbbq.Refresh] 拉取 %s 失败: %v (已拉取 %d/%d)\n", fullCode, qerr, i, total)
			failed[fullCode] = qerr
			continue
		}
		success = append(success, fullCode)
		if (i+1)%100 == 0 || i+1 == total {
			logs.Infof("[gbbq.Refresh] 进度 %d/%d (成功=%d, 失败=%d)\n", i+1, total, len(success), len(failed))
		}
	}
	logs.Infof("[gbbq.Refresh] 完成, 共 %d 只 (成功=%d, 失败=%d)\n", total, len(success), len(failed))
	return success, failed, nil
}

func (this *Gbbq) sort(m map[string][]*protocol.Gbbq) {
	for _, v := range m {
		sort.Slice(v, func(i, j int) bool {
			return v[i].Time.Before(v[j].Time)
		})
	}
}

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

func (this *Gbbq) update() (map[string][]*protocol.Gbbq, error) {
	gbbqs, err := this.c.GetGbbqAll()
	if err != nil {
		return nil, err
	}
	err = this.db.SessionFunc(func(session *xorm.Session) error {
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
	err = this.updated.Update(this.updateKey)
	return gbbqs, err
}
