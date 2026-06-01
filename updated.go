package tdx

import (
	"time"

	"github.com/injoyai/logs"
	"github.com/robfig/cron/v3"
	"xorm.io/xorm"
)

// Updater 提供 Update 方法的对象
// gbbq/codes 等管理器都实现该接口
type Updater interface {
	Update() error
}

// NewTimer 启动一个 cron 定时器,立即执行一次更新,失败按 retry 次数重试
// spec 例如 "0 0 9,15 * * 1-5" (工作日 9:00 / 15:00)
// retry 失败后重试次数(<0 表示只跑一次),重试间隔 5 分钟
func NewTimer(spec string, retry int, up Updater) error {
	// 立即更新一次
	if err := up.Update(); err != nil {
		return err
	}
	cr := cron.New(cron.WithSeconds())
	_, err := cr.AddFunc(spec, func() {
		for i := 0; i == 0 || i < retry; i++ {
			if err := up.Update(); err != nil {
				logs.Err(err)
				<-time.After(time.Minute * 5)
			} else {
				break
			}
		}
	})
	if err != nil {
		return err
	}
	cr.Start()
	return nil
}

// NewUpdated 构造 Updated 实例,同步 update 表
// hour, minute 为该 update 的"每日节点"时间,用于判断当天是否已更新
func NewUpdated(db *xorm.Engine, hour, minute int) (*Updated, error) {
	err := db.Sync2(new(UpdateModel))
	if err != nil {
		return nil, err
	}
	return &Updated{db: db, hour: hour, minute: minute}, nil
}

// Updated 记录某个 key 的最近一次更新时间
// 用于 NewTimer 启动时判断"今日节点之前/之后是否已经更新过",避免重复拉取
type Updated struct {
	db     *xorm.Engine
	hour   int
	minute int
}

// Update 把 key 对应的更新时间戳刷新为当前时间
func (this *Updated) Update(key string) error {
	_, err := this.db.Where("`Key`=?", key).Update(&UpdateModel{Time: time.Now().Unix()})
	return err
}

// Updated 返回 key 对应的更新记录是否存在
// 内部用节点的 hour/minute 字段判断"是否在最近一个节点之后更新过"
// 返回 (true, nil) 表示数据库中已存在该 key 的记录
func (this *Updated) Updated(key string) (bool, error) {
	update := new(UpdateModel)
	// 查询或者插入一条数据
	has, err := this.db.Where("`Key`=?", key).Get(update)
	if err != nil {
		return true, err
	} else if !has {
		update.Key = key
		if _, err = this.db.Insert(update); err != nil {
			return true, err
		}
		return false, nil
	}
	// 判断是否在节点之后更新过
	now := time.Now()
	node := time.Date(now.Year(), now.Month(), now.Day(), this.hour, this.minute, 0, 0, time.Local)
	updateTime := time.Unix(update.Time, 0)
	if now.Sub(node) > 0 {
		// 当前时间在节点之后,且更新时间在节点之前,说明今天还没更新
		if updateTime.Sub(node) < 0 {
			return false, nil
		}
	} else {
		// 当前时间在节点之前,且更新时间在上个节点之前
		if updateTime.Sub(node.Add(-time.Hour*24)) < 0 {
			return false, nil
		}
	}
	return true, nil
}
