package tdx

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/injoyai/ios"
	"github.com/injoyai/ios/module/tcp"
	"github.com/injoyai/logs"
)

func NewTCPDial(addr string) ios.DialFunc {
	if !strings.Contains(addr, ":") {
		addr += ":7709"
	}
	return tcp.NewDial(addr)
}

func NewHostDial(hosts []string) ios.DialFunc {
	if len(hosts) == 0 {
		hosts = Hosts
	}
	index := 0

	return func(ctx context.Context) (ios.ReadWriteCloser, string, error) {
		defer func() { index++ }()
		if index >= len(hosts) {
			index = 0
		}
		addr := hosts[index]
		if !strings.Contains(addr, ":") {
			addr += ":7709"
		}
		c, err := net.Dial("tcp", addr)
		return c, addr, err
	}
}

func NewRandomDial(hosts []string) ios.DialFunc {
	if len(hosts) == 0 {
		hosts = Hosts
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return func(ctx context.Context) (ios.ReadWriteCloser, string, error) {
		addr := hosts[r.Intn(len(hosts))]
		if !strings.Contains(addr, ":") {
			addr += ":7709"
		}
		c, err := net.Dial("tcp", addr)
		return c, addr, err
	}
}

func NewRangeDial(hosts []string) ios.DialFunc {
	if len(hosts) == 0 {
		hosts = Hosts
	}
	return func(ctx context.Context) (c ios.ReadWriteCloser, _ string, err error) {
		for i, addr := range hosts {
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			default:
			}
			if !strings.Contains(addr, ":") {
				addr += ":7709"
			}
			c, err = net.Dial("tcp", addr)
			if err == nil {
				return c, addr, nil
			}
			if i < len(hosts)-1 {
				//最后一个错误返回出去
				logs.Err(err, "等待2秒后尝试下一个服务地址...")
				<-time.After(time.Second * 2)
			}
		}
		return
	}
}

// raceDialTimeout 单台主机并发拨号的超时时间。
// 全部主机同时竞速, 最坏窗口 ≈ 该值(串行 range-dial 为 每台2s超时+2s间隔, 41台最坏 ~2.5 分钟)。
const raceDialTimeout = time.Second * 2

// NewRaceDial 并发竞速拨号: 每次调用对全部主机同时发起 TCP 拨号(net.DialTimeout), 取最先成功者, 其余连接立即关闭。
// 替代串行逐台重试的 NewRangeDial, 把"最快主机当次不可达"的重拨窗口从 ~20s(08:46 实测) 压到 ~2s。
//
// hosts 为空时, 每次调用动态读取包级 Hosts(经 hostSnapshot) —— 这样 SortHosts 的
// 启动测速与 web 层周期重测回写能实时生效, 而 DialHostsRange 是调用时一次性快照。
//
// 日志: 开始(台数)/每台失败/竞速成功(地址+耗时)/全部失败, 补齐重拨事件的可观测性。
func NewRaceDial(hosts []string) ios.DialFunc {
	return func(ctx context.Context) (ios.ReadWriteCloser, string, error) {
		if len(hosts) == 0 {
			hosts = hostSnapshot()
		}
		if len(hosts) == 0 {
			return nil, "", errors.New("无可用服务器地址")
		}

		type result struct {
			c    ios.ReadWriteCloser
			addr string
		}
		ch := make(chan result, 1) //只有第一个成功者写入

		//已建立的连接登记表: 竞速结束后关闭所有输家连接(含晚到成功者), 防止 fd 泄漏
		var mu sync.Mutex
		settled := false
		conns := map[string]ios.ReadWriteCloser{}

		start := time.Now()
		logs.Warnf("[重拨] 开始并发竞速拨号 %d 台主机...", len(hosts))

		for _, host := range hosts {
			go func(host string) {
				addr := host
				if !strings.Contains(addr, ":") {
					addr += ":7709"
				}
				c, err := net.DialTimeout("tcp", addr, raceDialTimeout)
				if err != nil {
					logs.Err(err, "[重拨] 主机拨号失败")
					return
				}
				mu.Lock()
				if settled { //竞速已出结果, 本机是输家
					mu.Unlock()
					c.Close()
					return
				}
				conns[addr] = c
				mu.Unlock()
				select {
				case ch <- result{c: c, addr: addr}:
				default: //已有更快者胜出
				}
			}(host)
		}

		timer := time.NewTimer(raceDialTimeout + time.Second)
		defer timer.Stop()
		settle := func(keep string) { //出结果: 关闭所有输家连接(含晚到成功者), 防止 fd 泄漏
			mu.Lock()
			settled = true
			for addr, c := range conns {
				if addr != keep {
					c.Close()
				}
			}
			mu.Unlock()
		}
		select {
		case <-ctx.Done():
			settle("")
			return nil, "", ctx.Err()
		case <-timer.C:
			settle("")
			logs.Errf("[重拨] 全部 %d 台主机竞速拨号失败(耗时 %s)", len(hosts), time.Since(start))
			return nil, "", fmt.Errorf("全部 %d 台主机竞速拨号失败", len(hosts))
		case r := <-ch:
			settle(r.addr)
			logs.Warnf("[重拨] 竞速拨号成功: %s (耗时 %s)", r.addr, time.Since(start))
			return r.c, r.addr, nil
		}
	}
}

// NewExRangeDial 遍历扩展行情(TdxExHq)服务地址进行连接,成功则结束遍历(端口 7727)
func NewExRangeDial(hosts []string) ios.DialFunc {
	if len(hosts) == 0 {
		hosts = ExHosts
	}
	return func(ctx context.Context) (c ios.ReadWriteCloser, _ string, err error) {
		for i, addr := range hosts {
			select {
			case <-ctx.Done():
				return nil, "", ctx.Err()
			default:
			}
			if !strings.Contains(addr, ":") {
				addr += ":" + ExPort
			}
			c, err = net.Dial("tcp", addr)
			if err == nil {
				return c, addr, nil
			}
			if i < len(hosts)-1 {
				//最后一个错误返回出去
				logs.Err(err, "等待2秒后尝试下一个扩展行情服务地址...")
				<-time.After(time.Second * 2)
			}
		}
		return
	}
}
