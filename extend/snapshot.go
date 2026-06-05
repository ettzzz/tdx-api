package extend

import (
	"sync"

	"github.com/injoyai/tdx"
	"github.com/injoyai/tdx/protocol"
)

// PullSnapshotChunkSize 每次分配给一个 goroutine 处理的股票数量.
// 与 manager.Pool(4) 配合, 4 路并发 × 64 = 256 只/轮,
// 5300+ 只约 21 轮, 单次握手 RTT 摊销可忽略.
const PullSnapshotChunkSize = 64

// PullDaySnapshotForCodes 使用 manager.Pool 并发拉取一组股票"当天"日 K (count=1).
// 返回: 成功的 code -> Kline; 失败的 code 列表; 首个错误 (用于日志).
// 并发策略: 1 goroutine / chunk, 实际并发受 Pool 容量限制 (默认 4 连接).
// 适合 web handler 调用, 因为 GetDaySnapshot 在 Pool 连接上是串行的,
// 拆成多 chunk + 多 goroutine 才能吃满 4 路.
func PullDaySnapshotForCodes(m *tdx.Manage, codes []string) (map[string]*protocol.Kline, []string, error) {
	result := make(map[string]*protocol.Kline, len(codes))
	failed := make([]string, 0, len(codes))
	if m == nil || len(codes) == 0 {
		return result, failed, nil
	}

	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup

	for i := 0; i < len(codes); i += PullSnapshotChunkSize {
		end := i + PullSnapshotChunkSize
		if end > len(codes) {
			end = len(codes)
		}
		chunk := codes[i:end]

		wg.Add(1)
		go func(chunk []string) {
			defer wg.Done()
			// Pool.Do 阻塞等连接; 4 路同时跑, 后续 goroutine 排队.
			var chunkResult map[string]*protocol.Kline
			var chunkFailed []string
			var chunkErr error
			poolErr := m.Pool.Do(func(c *tdx.Client) error {
				chunkResult, chunkFailed, chunkErr = c.GetDaySnapshot(chunk)
				return nil
			})
			mu.Lock()
			defer mu.Unlock()
			if poolErr != nil {
				// Pool 拿不到连接 (例如已关闭) -> 整 chunk 视为失败
				failed = append(failed, chunk...)
				if firstErr == nil {
					firstErr = poolErr
				}
				return
			}
			for k, v := range chunkResult {
				result[k] = v
			}
			failed = append(failed, chunkFailed...)
			if chunkErr != nil && firstErr == nil {
				firstErr = chunkErr
			}
		}(chunk)
	}
	wg.Wait()

	return result, failed, firstErr
}
