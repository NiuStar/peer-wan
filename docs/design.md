## SD-WAN 控制面设计（WireGuard + Consul + FRR）

目标：搭一个最小可用的控制面/Agent，后续逐步补全。数据面用 WireGuard，动态路由靠 FRR（BGP/OSPF），状态/协调使用 Consul。

### 角色与组件
- 控制器（Go，中心）：提供 API（gRPC/REST），管理节点/站点信息、密钥分发、WireGuard/FRR 配置模板生成，写入 Consul KV，并驱动 Web UI。
- Agent（Go，边缘）：拉取控制器配置，应用到本地 WireGuard + FRR（通过模板 + `vtysh -b`），上报健康与拓扑探测结果。
- Consul：KV + 服务发现 + 健康检查，用于存储节点元数据、配置版本、拓扑/健康状态。
- Web UI：节点/站点管理、拓扑和路由视图、配置与审计。

### 关键数据模型（草案）
- Node：`id`, `publicKey`, `endpoints[]`, `cidrs[]`, `labels`, `status`, `configVersion`.
- PeerPlan：节点 peer 列表、允许的 CIDR、PresharedKey/allowedIPs、keepalive。
- Routing：站点网段、BGP/OSPF 配置（ASN、neighbors、route-maps/policy）。
- Health：WireGuard 隧道 RTT/丢包、FRR 邻居状态、CPU/Mem。
- Audit：操作人、变更 diff、时间戳、版本号。

### 典型流程
1) 引导/注册  
   - Agent 用 bootstrap token + 节点标识调用 `/nodes/register`。  
   - 控制器生成/校验节点，写 Consul KV，返回 WireGuard/FRR 基础配置与配置版本。
2) 配置下发  
   - 控制器更新配置时写 KV + 版本号。  
   - Agent watch KV（或长轮询），拉取差量，应用到 WireGuard（`wgctrl`) 与 FRR（模板渲染 → 写 `/etc/frr/*.conf` → `vtysh -b`）。
3) 路由分发  
   - 控制器根据站点 CIDR/拓扑策略（全 mesh / hub-spoke / 基于延迟的择优）生成 PeerPlan 和 BGP/OSPF 邻居。  
   - Agent 渲染 FRR 配置，重载，通告站点网段。
4) 健康与自愈  
   - Agent 定期 ping peers，收集 RTT/丢包，读取 FRR 邻居状态，上报健康。  
   - 控制器据此调整 peer 列表或降级策略。
5) 审计/回滚  
   - 配置变更版本化，写 KV + Audit。UI 支持 diff、审批、回滚。

### 最小可用（MVP）范围
- 控制器：节点注册、列表查询，内存存储（可切换 Consul KV，需 build tag）；健康探针；简单“全网状” Peer 计划（会跳过标记为 down 的节点）；节点配置版本号递增（幂等注册不增，`force` 可强制刷新）；审计写入存储。
- Agent：注册后渲染 WireGuard/FRR (BGP) 配置至本地目录（默认 ./out），可选 `--apply` 调用 `wg-quick` 与 `vtysh -b` 应用（需 root 且本机已装 wireguard/frr）；可选健康上报（ping peers + 读取 `vtysh show bgp summary`）定期 POST `/health`。
- API：`/api/v1/nodes/register`、`/api/v1/nodes`、`/api/v1/health`、`/api/v1/audit`、`/healthz`。
- Docs：README + 本设计文档。
- 简易认证：可配置 bootstrap token（`X-Auth-Token`/`Authorization: Bearer`），空值则关闭校验；控制器支持 TLS/mTLS（client CA 可选），Agent 支持自定义 CA/客户端证书或跳过校验（调试）。

### 近期迭代建议
- 存储：完善 Consul KV + session/lock + watch，避免单点；加入配置版本与幂等写；健康与审计落 KV/日志。
- 安全：引导 token 或 mTLS；操作审计；签名配置包。
- 数据面：生成/分发 WireGuard key、preshared-key；模板化接口地址、AllowedIPs、keepalive；多端点 failover。
- 控制平面：拓扑计算（全 mesh / hub-spoke / intent-based）、策略/ACL、QoS（DSCP/TC），多 Region；基于健康（延迟/丢包/FRR 邻居状态）动态调整 peer 计划。
- 观测性：隧道/路由可视化、FRR 邻居状态、流量与延迟面板；健康上报驱动拓扑降级/重算。
- Web UI：节点/路由/拓扑/审计四页 + 配置版本对比/回滚。
