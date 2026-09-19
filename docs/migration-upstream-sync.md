# 迁移文档：根模块对齐上游 injoyai/tdx HEAD（路线 A）

- 分支：`feat/upstream-sync`（worktree：`/home/orangepi/git_repos/tdx-api-migrate`）
- 基线：main @ f975193（2026-09-19）
- 上游基点：`upstream/master` = 2b3dcae（2026-09-05）
- 方式：两边 git 历史无关，不做 merge/rebase；按文件从 upstream checkout + 手工重放本地补丁
- 原则：根模块 = 上游原样 + 最小补丁集；web 层 44 条路由语义不变；不触碰运行中的 `tdx-stock-web:v1.4.0`（8080）
- 本机构建：`PATH=/home/orangepi/go-local/go/bin:$PATH GOPATH=/home/orangepi/go GOPROXY=https://goproxy.cn,direct`（go1.23.0，GOTOOLCHAIN=auto 自动拉 1.25）

## 本地补丁集（迁移后必须存活的东西）

| 补丁 | 位置 | 说明 |
|---|---|---|
| GetDaySnapshot | client.go | 全市场快照底层，支撑 extend/snapshot.go |
| CST 固定时区 | protocol/model_trade.go, model_history_trade.go | 上游仍依赖服务器本地时区，我们是对的 |
| gbbq 懒加载 + Refresh/FetchOne/WithGbbqCodes | gbbq.go | 防限流 + /api/gbbq/refresh 依赖；叠加在上游仿射模型之上 |
| pull-trade 年份范围 | extend/pull-trade.go | StartYear/EndYear |
| 防限流的 codes 缓存语义 | codes.go | 核对上游 CodesBase 是否已满足 |
| 保留的本地独有文件 | extend/snapshot.go 等 | 见下 |

## 批次与进度

- [ ] **批1** protocol 全量共享文件 + 新增 8 模型、ExHq 三件套、lib/、hosts.go、dial.go、pool.go、
      extend/{local,model_kline,model-factor,spider-sina,pull,httpserver,codes-server,util,income,codes-bj}
- [ ] **批1** model_trade.go / model_history_trade.go 重放 CST 补丁
- [ ] **批2** client.go(+GetDaySnapshot)、codes.go、manage.go、workday.go、updated.go、
      extend/pull-kline.go、extend/pull-trade.go(+年份补丁)、extend/spider-ths.go、go.mod/go.sum
- [ ] **批2** gbbq.go 合并（上游仿射骨架 + 本地懒加载/Refresh）
- [ ] **批3** 删 client_bj_code.go（北交所走 tdxbjmore.cfg）、extend/ths-factor.go、extend/pull-kline-mysql.go
- [ ] **M2** web 适配：q.K→q.Kline(server_realtime:277)、NewManage Option 化(server.go:50)、
      NewCodesSqlite(server.go:41)、GetFactors 仿射应用(server_api_extended:308)、
      DoIncomes 入参(:1120)、NewPullKline 新签名(server.go:562)、PullDaySnapshotForCodes(:1746)、
      GetTHSDayKline 类型(server.go:190)
- [ ] **M3** 缓存兼容：全新卷 + 拷贝 tdx-api_tdx-data 卷验证
- [ ] **M4** Docker 隔离测试：v2.0.0-rc1 镜像 + compose project `tdx-api-migrate`（8081、独立卷、
      container_name 必须覆盖为 tdx-stock-web-rc）+ 冒烟 + 双容器对比 + 5 项正确性专项
      （低量分钟K线 / qfq vs ths 交叉 / 可转债分钟量 / ETF quote / 北交所 codes）
- [ ] **M5** 合并 main 打 tag v2.0.0 —— 需用户确认后才执行，动 8080 前汇报

## 已知上游破坏性变更（web 适配点依据）

1. `protocol.K` 与 `protocol.Kline` 合并（01fa21c）：`Quote.K` → `Quote.Kline`
2. Manage/Codes/Workday/Gbbq 全部 Option 化 + 接口化（ICodes/IGbbq/IPool）
3. 复权改仿射模型（026e64a）：`Factor` 新增 QFQMul/QFQAdd/HFQMul/HFQAdd，包级 `protocol.ApplyQFQ`
4. `extend/pull-kline.go` 存储模型改为 per-code sqlite 文件
5. 北交所代码改走 `GetCodeAll(ExchangeBJ)` → zhb.zip 的 tdxbjmore.cfg
6. `IsBJStock` 收窄为 92 开头（43/8 不再算 BJ，M4 北交所专项确认）

## 回滚

任意时刻：主 repo 的 main 与运行容器未受影响；rc 容器 `docker compose -p tdx-api-migrate down`；
镜像 `tdx-stock-web:v2.0.0-rc1` 删除即净。
