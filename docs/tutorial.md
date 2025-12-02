## 端到端使用教程（控制器部署、节点加入、出站策略）

本教程从零搭建控制器和节点，完成健康驱动的动态计划，并指导如何让 A 节点流量从 B/C 节点出口（策略示例）。

### 1. 控制器部署（含 Consul）
#### 1.1 快速启动（Docker Compose）
```bash
# 控制器 + Consul（持久化 data 卷）
docker compose -f docker-compose.controller.yaml up --build -d
# 控制器：http://<host>:8080
# UI：http://<host>:8080/ui/
# Consul UI：http://<host>:8500
```

#### 1.2 手动运行二进制
```bash
./controller --store=consul --consul-addr=http://127.0.0.1:8500 --token=changeme \
  --tls-cert=server.crt --tls-key=server.key --client-ca=ca.crt   # 可选 mTLS
# 或内存存储：--store=memory --token=changeme
```

### 2. 节点/Agent 部署（仅输入节点名，一键脚本）
#### 2.1 在控制台点击“添加节点”
- 只输入节点 ID，控制器自动生成：WireGuard 公私钥、Overlay IP、ProvisionToken、一键脚本。
- UI 会弹出脚本并自动复制到剪贴板，例如：
```bash
curl -fsSL https://raw.githubusercontent.com/NiuStar/peer-wan/refs/heads/main/scripts/agent-install.sh -o /tmp/agent-install.sh \
  && chmod +x /tmp/agent-install.sh \
  && sudo /tmp/agent-install.sh --controller=https://peerwan.199028.xyz --node-id=edge-1 --provision-token=pt-xxx --auto-endpoint=true
```
- 参数含义与来源：
  - `NODE_ID`：节点名称（UI 输入）。
  - `PROVISION_TOKEN`：控制器生成的一次性节点令牌，用于免登录拉取配置、上报健康、获取计划。
  - `CONTROLLER_ADDR`：控制器外部可访问的地址（在启动控制器时通过 `--public-addr` 指定）。

#### 2.2 脚本执行结果
- 自动下载 agent 二进制到 `/usr/local/bin/agent`，生成包装 `/usr/local/bin/peer-wan-agent`。
- 包装默认启用：
  - `--provision-token=${PROVISION_TOKEN}`：无需手填 pub/priv/overlay。
  - `--auto-endpoint=true`：自动探测出口 IP:Port。
  - `--plan-interval=30s --health-interval=30s --apply=true`：持续拉取计划/健康上报并应用配置。

#### 2.3 手动运行（可选）
```bash
./agent \
  --id=edge-1 \
  --provision-token=pt-xxx \            # 来自 UI 的令牌
  --controller="https://peerwan.199028.xyz" \
  --auto-endpoint=true \                # 自动发现出口
  --plan-interval=30s --health-interval=30s \
  --apply=true
# 如需手工覆盖：--endpoints="203.0.113.1:51820" --cidrs="10.10.1.0/24" --overlay-ip="10.10.1.1/32" --asn=65000
```

附：字段解释
- `Overlay IP`：控制器分配的隧道地址（/32），如 10.10.x.1/32。
- `Endpoints`：agent 自动探测的出口 `IP:Port`（可覆盖）。
- `CIDRs`：站点网段，若需出口策略，可在 UI 的“出口/策略”设置中配置。
- `ProvisionToken`：节点级凭据，计划拉取与健康上报都可使用（通过请求头 `X-Provision-Token`）。
### 3. 控制器 API/UI 操作
- 注册：`POST /api/v1/nodes/register`
- 计划：`GET /api/v1/plan?nodeId=&waitVersion=`（长轮询）
- 历史：`GET /api/v1/plan/history?nodeId=...`
- 回滚：`POST /api/v1/plan/rollback` body: `{"nodeId":"","version":123}`
- 全局版本：`GET /api/v1/version`
- UI：`/ui/` 查看节点/健康/审计/计划历史/回滚，拓扑按健康延迟着色。

### 4. 计划/签名/回滚机制
- 计划保存含 SHA256 签名（节点 ID + configVersion + peers/AllowedIPs），回滚校验。
- 全局计划版本键：Consul `peer-wan/plan/version`；注册/健康/重算/回滚更新版本。
- 历史：Consul `peer-wan/plan/<node>/<version>`；UI 可查看/回滚。
- Agent：先取 `/api/v1/version`，再带 `waitVersion` 请求 `/api/v1/plan`，或 Consul watch 版本键。

### 5. 让 A 节点流量从 B/C 节点出口（内置策略）
- 计划/模型：Plan 与 Node 支持 `egressPeerId`、`policyRules`（Prefix -> ViaNode）。
- UI：节点卡片下的“出口/策略配置”可设置出口 PeerID、添加/删除前缀策略并提交；历史时间轴显示出口与签名状态。
- 控制器：`POST /api/v1/policy` 写入策略并递增计划版本，保存历史；Agent 通过 waitVersion 或 Consul watch 拉取新计划。
- Agent 渲染：默认路由指向出口 peer 的 overlay IP，策略前缀生成静态路由指向指定 peer 的 overlay。
- 多出口切换：基于健康（延迟/丢包/FRR 状态）可调整出口标记，重算计划后 Agent 自动更新路由。

### 6. 验证 SD-WAN 网络
1) 启动至少两个节点，CIDR 不同，Overlay IP 唯一。
2) Agent 渲染/应用配置后，在任意节点：
   ```bash
   ping <对端 overlay IP>
   ping <对端站点网段 IP>
   vtysh -c "show bgp summary"   # 邻居应为 Established
   ```
3) 在 UI 查看健康拓扑、计划历史，确认版本变化与出口策略生效。

### 7. 常见问题
- UI 默认基址：已自动使用当前 origin，若不对请手动填写 API 基址与 token。
- Consul 数据丢失：compose 已挂载 `consul-data` 卷；生产请使用正式 Consul 集群。
- Release 构建/上传：`RELEASE_TAG=vX.Y.Z scripts/build-release.sh`，必要时设置 `GH_REPO`/`GITHUB_TOKEN`。***
