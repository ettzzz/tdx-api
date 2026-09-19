package protocol

import "testing"

// TestAddPrefixDotSuffix 本地补丁回归测试: 点后缀写法 (对齐旧版 tdx-api 行为)。
// 后缀显式指定交易所且优先于数字推断; 无法识别的后缀回退数字推断。
func TestAddPrefixDotSuffix(t *testing.T) {
	cases := []struct{ in, want string }{
		{"600000.SH", "sh600000"},
		{"000001.SH", "sh000001"}, // 上证指数: 不因 0 开头误判为 sz000001(平安银行)
		{"000001.SZ", "sz000001"},
		{"510300.SH", "sh510300"},
		{"159915.SZ", "sz159915"},
		{"920000.BJ", "bj920000"},
		{"113050.SH", "sh113050"}, // 沪市可转债
		{"600000.sh", "sh600000"}, // 小写后缀
		{"sh600000", "sh600000"},  // 已带前缀, 原样
		{"600000", "sh600000"},    // 无后缀, 数字推断
		{"600000.XX", "sh600000"}, // 无法识别的后缀, 回退数字推断
	}
	for _, c := range cases {
		if got := AddPrefix(c.in); got != c.want {
			t.Errorf("AddPrefix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
