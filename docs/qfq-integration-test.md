# 仿射 QFQ 集成测试设计

## 测试用例

### 1. 基本功能测试
- 选择一只已知有除权除息的股票（如 sz002222 福晶科技）
- 调用 `/api/kline-history-qfq?code=sz002222&type=day`
- 验证返回数据格式正确

### 2. 与 THS QFQ 一致性测试
- 对同一只股票，分别调用：
  - `/api/kline-history-qfq?code=sh600000&type=day` (仿射)
  - `/api/kline-history?code=sh600000&type=day` (THS)
- 比较 OHLCV 数据，允许 ±1 厘的舍入误差

### 3. 边界测试
- 空 gbbq 缓存：返回错误提示
- 无除权除息事件的股票：返回原始数据
- 周/月 K 线转换：验证正确性

### 4. 日期范围测试
- 指定 start_date / end_date
- 验证过滤正确

## 手动验证命令

```bash
# 启动服务
cd /home/orangepi/git_repos/tdx-api-sprint3
go run . &

# 基本测试
curl -s "http://localhost:8080/api/kline-history-qfq?code=sz002222&type=day" | jq '.data.list[0:3]'

# 与 THS 对比
curl -s "http://localhost:8080/api/kline-history-qfq?code=sh600000&type=day" | jq '.data.list[-3:]' > /tmp/affine.json
curl -s "http://localhost:8080/api/kline-history?code=sh600000&type=day" | jq '.data.list[-3:]' > /tmp/ths.json
diff /tmp/affine.json /tmp/ths.json
```
