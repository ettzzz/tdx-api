package protocol

import (
	"encoding/hex"
	"testing"
	"time"
)

func Test_stockKline_Frame(t *testing.T) {
	//预期0c02000000001c001c002d050000303030303031 0900 0100 0000 0a00 00000000000000000000
	//   0c00000000011c001c002d050000313030303030 0900 0000 0000 0a00 00000000000000000000
	f, _ := MKline.Frame(TypeKlineDay, "sz000001", 0, 10)
	t.Log(f.Bytes().HEX())
}

func Test_stockKline_Decode(t *testing.T) {
	s := "0a0078da340198b8018404bc055ee8b3e949ad2b094f79da34010af801a002cc0260dec949859ded4e7ada34016882028e04e603b8f91e4a111f394f7dda3401e401c20200f604f84d2b4ad4d0444f7eda3401721eaa0268d87bc549ee80e34e7fda34011e288601c601d08db849230ed54e80da3401727c32da013023584999a0784e81da3401147c0ad001d0fa86498d989a4e84da34015e6800d60278c28e491ca6a14e85da340154d001b801da01403e924989d6a54e"
	bs, err := hex.DecodeString(s)
	if err != nil {
		t.Error(err)
		return
	}
	resp, err := MKline.Decode(bs, KlineCache{
		Type: 9,
		Kind: "",
	})
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(len(resp.List))
	for _, v := range resp.List {
		t.Log(v)
	}
}

func TestKlines_ApplyQFQ(t *testing.T) {
	// 构造测试数据
	baseTime := time.Date(2025, 5, 20, 15, 0, 0, 0, time.Local)

	klines := Klines{
		{Time: baseTime, Open: 1000, High: 1100, Low: 900, Close: 1050, Volume: 1000, Amount: 1050000},
		{Time: baseTime.AddDate(0, 0, 1), Open: 1050, High: 1150, Low: 950, Close: 1100, Volume: 1200, Amount: 1320000},
	}

	factors := []*Factor{
		{Time: baseTime, QFQ: 0.8},                       // 除权日，因子 0.8
		{Time: baseTime.AddDate(0, 0, 1), QFQ: 1.0}, // 次日无调整
	}

	result := klines.ApplyQFQ(factors)

	if len(result) != 2 {
		t.Fatalf("expected 2 klines, got %d", len(result))
	}

	// 第一天：应用 0.8 因子
	k0 := result[0]
	if k0.Open != 800 { // 1000 * 0.8
		t.Errorf("day1 open: expected 800, got %d", k0.Open)
	}
	if k0.Close != 840 { // 1050 * 0.8
		t.Errorf("day1 close: expected 840, got %d", k0.Close)
	}

	// 第二天：因子 1.0，不变
	k1 := result[1]
	if k1.Open != 1050 {
		t.Errorf("day2 open: expected 1050, got %d", k1.Open)
	}
}

func TestKlines_ApplyQFQ_EmptyFactors(t *testing.T) {
	klines := Klines{
		{Time: time.Now(), Open: 1000, Close: 1050},
	}

	// 空因子，返回原始数据
	result := klines.ApplyQFQ([]*Factor{})
	if len(result) != 1 {
		t.Fatalf("expected 1 kline, got %d", len(result))
	}
	if result[0].Open != 1000 {
		t.Errorf("expected unchanged open 1000, got %d", result[0].Open)
	}
}

func TestKlines_ApplyQFQ_NilKlines(t *testing.T) {
	now := time.Now()
	klines := Klines{nil, {Time: now, Open: 1000, Close: 1050}}
	factors := []*Factor{{Time: now, QFQ: 0.9}}

	result := klines.ApplyQFQ(factors)
	if result[0] != nil {
		t.Error("expected nil kline to remain nil")
	}
	if result[1].Open != 900 { // 1000 * 0.9
		t.Errorf("expected 900, got %d", result[1].Open)
	}
}
