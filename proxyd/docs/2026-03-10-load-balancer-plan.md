# Load Balancer Implementation Plan

> Design: [2026-03-10-load-balancer-design.md](./2026-03-10-load-balancer-design.md)

## Step 1: Config 层改动

**文件**: `proxyd/config.go`

1. 新增常量 `LoadBalancerRoutingStrategy RoutingStrategy = "load_balancer"`
2. `BackendGroupConfig` 新增字段:
   - `StickySessionEnabled bool` `toml:"sticky_session_enabled"`
   - `StickySessionHashKey string` `toml:"sticky_session_hash_key"`
   - `StickyVirtualNodes int` `toml:"sticky_virtual_nodes"`
3. `ValidateRoutingStrategy()` 增加 `case LoadBalancerRoutingStrategy: return true`

**验证**: 单元测试解析带新字段的 TOML 配置

## Step 2: 一致性哈希实现

**新文件**: `proxyd/consistent_hash.go`

实现 `ConsistentHash` 结构:
- `NewConsistentHash(virtualNodes int) *ConsistentHash`
- `Update(backends []*Backend)` — 重建哈希环（带 change detection）
- `GetOrderedBackends(clientKey string, candidates []*Backend) []*Backend` — 返回按一致性哈希排序的 backend 列表

**新文件**: `proxyd/consistent_hash_test.go`

测试:
- 相同 key 始终返回相同顺序
- backend 增删后只影响部分 key 的映射
- 空候选列表的边界情况
- 虚拟节点数量对分布均匀性的影响

## Step 3: ConsensusPoller relaxed 模式

**文件**: `proxyd/consensus_poller.go`

1. `ConsensusPoller` 新增 `relaxedMode bool` 字段
2. `NewConsensusPoller()` 接收 relaxedMode 参数
3. relaxed 模式下 `UpdateBackendGroupConsensus()` 的行为变化:
   - 不计算 consensus block number / hash（不需要所有节点 block hash 一致）
   - consensus group = 所有 block lag 在阈值内且 peer count 足够的节点
   - ban 策略更宽松（只 ban 完全离线或落后太多的节点）
   - 不更新 ConsensusTracker（不需要 consensus block numbers）

**验证**: 现有 consensus_aware 测试仍然通过（非 relaxed 模式不受影响）

## Step 4: BackendGroup 扩展

**文件**: `proxyd/backend.go`

1. `BackendGroup` 新增字段:
   ```go
   stickySessionEnabled bool
   stickyHashKey        string
   consistentHash       *ConsistentHash
   ```

2. `orderedBackendsForRequest()` 签名改为 `orderedBackendsForRequest(ctx context.Context)`:
   - 所有调用方需要传入 ctx
   - 新增 `bg.consistentHash != nil` 分支，调用 `bg.loadBalancedGroup(ctx)`

3. 新增 `loadBalancedGroup(ctx context.Context) []*Backend`:
   ```
   1. bg.Consensus.GetConsensusGroup()
   2. 分层 healthy / degraded（复用现有逻辑）
   3. if stickySessionEnabled:
        xff := GetXForwardedFor(ctx)
        orderedHealthy := bg.consistentHash.GetOrderedBackends(xff, healthy)
      else:
        随机 shuffle healthy
   4. return append(orderedHealthy, degraded...)
   ```

4. `Forward()` 改动:
   - 调用 `bg.orderedBackendsForRequest(ctx)` 传入 ctx
   - 当 `routing_strategy == "load_balancer"` 时跳过 `OverwriteConsensusResponses`
   - 具体: `if bg.Consensus != nil && bg.GetRoutingStrategy() != LoadBalancerRoutingStrategy`

**验证**: 现有测试通过 + 新增 load_balancer 路由测试

## Step 5: Startup 初始化

**文件**: `proxyd/proxyd.go`

1. `load_balancer` 策略的初始化逻辑:
   - 创建 ConsensusPoller（relaxedMode = true）
   - 如果 `sticky_session_enabled`，创建 ConsistentHash
   - 将 ConsistentHash 赋给 BackendGroup
   - 启动 poller goroutine

2. BackendGroup 构造时传入新字段

**验证**: 启动测试，配置 load_balancer 策略正常初始化

## Step 6: Metrics

**文件**: `proxyd/metrics.go`

新增:
- `proxyd_sticky_session_backend_selected` (CounterVec, labels: backend_name, backend_group)
- `proxyd_load_balancer_healthy_backends` (GaugeVec, labels: backend_group)
- `proxyd_load_balancer_degraded_backends` (GaugeVec, labels: backend_group)

在 `loadBalancedGroup()` 和 ConsensusPoller 更新时记录。

## Step 7: 配置示例更新

**文件**: `proxyd/example.config.toml`

新增 load_balancer 配置示例段落。

## Step 8: 集成测试

**目录**: `proxyd/integration_tests/`

新增测试:
- `load_balancer_test.go` — 基本负载均衡、sticky session、故障转移
- 测试配置文件: `testdata/load_balancer.toml`

测试场景:
1. 请求均匀分布到健康 backends
2. 同一 XFF 的请求始终路由到同一 backend
3. 目标 backend 下线后自动 failover 到下一个
4. 目标 backend 恢复后请求回到原 backend
5. 不做 block tag rewriting（请求参数不被修改）

## Dependencies

```
Step 1 (config) ──→ Step 5 (startup)
Step 2 (hash)  ──→ Step 4 (backend)
Step 3 (poller) ──→ Step 4 (backend)
Step 4 (backend) ──→ Step 5 (startup)
Step 5 (startup) ──→ Step 6 (metrics)
Step 6 (metrics) ──→ Step 7 (example config)
Step 7 (config)  ──→ Step 8 (integration tests)
```

Steps 1, 2, 3 可以并行开发。
