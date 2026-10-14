package protocol

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func Test_quote_Frame(t *testing.T) {
	//0c0000000001130013003e050500000000000000010000303030303031
	f, err := MQuote.Frame("sz000001")
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(f.Bytes().HEX())
}

// Test_quote_Frame_PadCode6 代码域必须补齐为协议定长 7 字节(市场1字节+代码6字节)。
// 背景: 2026-10-14 港股行情污染事故 —— 港股 5 位代码原样嵌入导致请求帧比服务端
// 预期短 1 字节, 服务端按固定结构解析时吃掉紧随其后的下一帧首字节, 造成同连接
// 后续请求连锁超时(见 tools/hkprobe)。
func Test_quote_Frame_PadCode6(t *testing.T) {
	cases := []struct {
		code    string
		wantLen int // Data 长度: 8(固定头) + 2(count) + (1市场+6代码)/只
		wantHex string
	}{
		{"sz000001", 17, "303030303031"}, // A股 6位不变(市场字节 0x00 在代码域前)
		{"hk00100", 17, "303031303000"},  // 港股 5位右侧补 \x00
		{"hk00700", 17, "303037303000"},  // 港股 00700
		{"usAAPL", 17, "4141504c0000"},   // 美股 4字母补 2 字节
	}
	for _, c := range cases {
		f, err := MQuote.Frame(c.code)
		if err != nil {
			t.Errorf("%s: %v", c.code, err)
			continue
		}
		if got := len(f.Data); got != c.wantLen {
			t.Errorf("%s: Data 长度 = %d, 期望 %d", c.code, got, c.wantLen)
		}
		gotHex := hex.EncodeToString(f.Data[11:]) // 跳过 8 字节固定头 + 2 字节 count + 1 字节市场
		if gotHex != c.wantHex {
			t.Errorf("%s: 代码域 = %s, 期望 %s", c.code, gotHex, c.wantHex)
		}
	}
}

// Test_kline_Frame_PadCode6 K线帧的代码域同样必须补齐。
func Test_kline_Frame_PadCode6(t *testing.T) {
	f, err := MKline.Frame(TypeKlineDay, "hk00700", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	// 2(市场+保留) + 6(代码) + 2(类型) + 2(未知) + 2(start) + 2(count) + 10(保留)
	if got := len(f.Data); got != 26 {
		t.Errorf("Data 长度 = %d, 期望 26", got)
	}
	if !bytes.Equal(f.Data[2:8], []byte("00700\x00")) {
		t.Errorf("代码域 = %X, 期望 00700 补 \\x00", f.Data[2:8])
	}
}
