# Load Balancer Routing Strategy Design

> Date: 2026-03-10
> Status: Approved
> Branch: alt/proxyd/v4.25.0-flashblock-dev

## Background

proxyd 当前有 `consensus_aware` 路由策略，通过 ConsensusPoller 检查 backend 节点的 block 状态达成共识，并对请求进行 block tag rewriting（把 latest/safe/finalized 替换为共识值）后派发到 backend。

我们需要一个更轻量的 **load balancer** 路由策略：
1. 纯粹做请求转发，**不修改 RPC 方法参数**
2. 支持 **IP 粘连（sticky session）**，让同一 IP 的客户端尽量使用同一个 backend
3. 定期检查 backend 健康状态，剔除不健康节点

## Design

### 新增路由策略

新增 `routing_strategy = "load_balancer"` 作为第四种路由策略，与现有策略并列：

| Strategy | Block Tag Rewriting | Health Check | 选节点方式 |
|----------|-------------------|--------------|-----------|
| `fallback` | No | probe + metrics | 固定顺序 |
| `multicall` | No | probe + metrics | 广播所有节点 |
| `consensus_aware` | **Yes** | ConsensusPoller (strict) | 随机 shuffle |
| `load_balancer` | **No** | ConsensusPoller (relaxed) | **一致性哈希** |

### 健康检查

复用 ConsensusPoller，但工作在 relaxed 模式：
- 检查 block lag（节点落后主网太多则剔除）
- 检查 peer count（peer 过少则剔除）
- **不做** block hash 一致性检查（不要求所有节点在同一个 block）
- **不做** block tag rewriting（不维护共识 block number）

实现方式：ConsensusPoller 增加 `relaxedMode bool` 标志。relaxed 模式下：
- `GetConsensusGroup()` 返回所有 block lag 在阈值内且 peer count 足够的节点
- 不计算 consensus block number / hash
- 不执行 ban 逻辑（或使用更宽松的 ban 策略）

### Sticky Session（一致性哈希）

#### 算法

使用一致性哈希环（consistent hashing with virtual nodes）：
- 每个 backend 在环上有 N 个虚拟节点（默认 150）
- 虚拟节点 key: `"backendName#replicaIndex"`
- 哈希函数: SHA256 前 4 字节 → uint32
- 客户端 key: X-Forwarded-For header（fallback 到 RemoteAddr）

#### 路由流程

```
Client Key (XFF/RemoteAddr)
    ↓
SHA256 Hash → uint32
    ↓
Binary search on sorted hash ring
    ↓
Clockwise traversal → ordered backend list
    ↓
[preferred, fallback1, fallback2, ...]
    ↓
Try in order until success
```

#### 故障转移

当首选 backend 不健康时，一致性哈希返回的排序列表中下一个 backend 自动接管。当原 backend 恢复后，该 IP 的请求自动回到原 backend。

#### 环更新

ConsensusPoller 每次轮询更新 consensus group 后，触发 `ConsistentHash.Update(healthyBackends)`。使用 `hasBackendsChanged()` 检查是否需要重建，避免不必要的重建开销。

### 配置

```toml
[backend_groups.main]
backends = ["geth-1", "geth-2", "geth-3"]
routing_strategy = "load_balancer"

# Sticky session
sticky_session_enabled = true
sticky_session_hash_key = "xff"     # 当前仅支持 "xff"
sticky_virtual_nodes = 150          # 虚拟节点数，默认 150

# 健康检查（复用 consensus poller 配置）
consensus_max_block_lag = 8         # 最大 block 落后数
consensus_min_peer_count = 3        # 最小 peer 数量
consensus_poller_interval = "1s"    # 轮询间隔
consensus_ban_period = "5m"         # 不健康节点封禁时间
```

### 代码改动

#### 新增文件

| 文件 | 内容 |
|------|------|
| `consistent_hash.go` | 一致性哈希环实现 |

#### 修改文件

| 文件 | 改动 |
|------|------|
| `config.go` | 新增 `LoadBalancerRoutingStrategy` 常量；`BackendGroupConfig` 新增 sticky session 字段；`ValidateRoutingStrategy` 增加分支 |
| `backend.go` | `BackendGroup` 新增 `stickySessionEnabled`、`stickyHashKey`、`consistentHash` 字段；`orderedBackendsForRequest()` 改为接收 `ctx context.Context` 参数并增加 load_balancer 分支；新增 `loadBalancedGroup(ctx)` 方法；`Forward()` 中 load_balancer 策略跳过 `OverwriteConsensusResponses` |
| `proxyd.go` | load_balancer 策略启动时创建 ConsensusPoller（relaxed 模式） |
| `consensus_poller.go` | 新增 `relaxedMode` 标志，影响 `UpdateBackend` 和 `UpdateBackendGroupConsensus` 的行为 |
| `metrics.go` | 新增 sticky session 相关 metrics |
| `server.go` | 无改动（XFF 已在 context 中） |

### 请求处理流程

```
Client → Server.HandleRPC()
  ↓
  ctx 已包含 XFF (ContextKeyXForwardedFor)
  ↓
BackendGroup.Forward(ctx, rpcReqs, isBatch)
  ↓
  routing_strategy == "load_balancer"
  → 跳过 OverwriteConsensusResponses（不做 block tag rewriting）
  ↓
  bg.orderedBackendsForRequest(ctx)
  → bg.loadBalancedGroup(ctx)
     1. ConsensusPoller.GetConsensusGroup() → 获取健康 backends
     2. 分层: healthy (not degraded) / degraded
     3. ConsistentHash.GetOrderedBackends(xff, healthy)
     4. append degraded as fallback
  ↓
ForwardRequestToBackendGroup(rpcReqs, orderedBackends, ctx, isBatch)
  ↓
  按顺序尝试，第一个成功的返回
```

### Metrics

| Metric | Type | 描述 |
|--------|------|------|
| `proxyd_sticky_session_backend_selected` | Counter | 按 backend 统计被选中次数 |
| `proxyd_load_balancer_healthy_backends` | Gauge | 当前健康的 backend 数量 |
| `proxyd_load_balancer_degraded_backends` | Gauge | 当前降级的 backend 数量 |

## Future Extensions

以下功能在本次设计中记录但不实现，留待未来按需添加：

### Hash Key 扩展

当前仅支持 `sticky_session_hash_key = "xff"`。未来可扩展：

| Hash Key | 来源 | 适用场景 |
|----------|------|---------|
| `xff` | X-Forwarded-For header / RemoteAddr | 反向代理场景（当前实现） |
| `auth` | Authentication key (URL path) | 按 API key 做 sticky |
| `xff_auth` | XFF + Auth 组合 | 同一 IP 不同 API key 分开 sticky |

实现时只需修改 `loadBalancedGroup()` 中获取 hash key 的逻辑：

```go
func (bg *BackendGroup) getHashKey(ctx context.Context) string {
    switch bg.stickyHashKey {
    case "auth":
        return GetAuthCtx(ctx)
    case "xff_auth":
        return GetXForwardedFor(ctx) + "|" + GetAuthCtx(ctx)
    default: // "xff"
        return GetXForwardedFor(ctx)
    }
}
```

### 方法级别 Sticky Session

允许在同一个 BackendGroup 中为不同方法配置不同的 sticky 策略。但考虑到 batch request 的复杂性（一个 batch 可能包含多个方法），建议通过分 BackendGroup 实现，而非方法级别控制。

### WebSocket Sticky Session

当前 WebSocket 连接在 `ProxyWS()` 中按固定顺序尝试 backend。可以扩展为也使用一致性哈希。

## References

- Stash commit: `04a019a3d06e81783f06da09a680d77f854904da`（前一版设计文档和代码）
- 现有文件：`backend.go`, `consensus_poller.go`, `config.go`, `server.go`
