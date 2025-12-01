## peer-wan (minimal SD-WAN control plane stub)

This repo sketches a lightweight SD-WAN control plane built around WireGuard (data plane), Consul (coordination/state), and FRR (dynamic routing). It reuses ideas from Nebula-style onboarding/panel flows: one-click agent bootstrap, config versioning, and auditability.

### What’s here
- Minimal Go scaffolding:
  - `cmd/controller`: HTTP API stub for node registration/listing/health/audit.
  - `cmd/agent`: Agent registers to controller，渲染 WireGuard + FRR 配置到磁盘，支持可选自动应用与健康上报。
  - `pkg/api`: Shared types and HTTP handlers.
  - `pkg/store`: Storage interface + in-memory impl（带版本号递增，可切换 Consul KV，通过 build tag）。
  - `pkg/topology`: Simple full-mesh peer plan builder.
  - `pkg/model`: Shared data models（节点、peer、版本）。
  - `pkg/wireguard`: WireGuard config renderer。
  - `pkg/frr`: FRR/BGP config renderer。
  - `pkg/agent`: 渲染与落盘 WireGuard/FRR 配置、健康上报的辅助方法。
  - `pkg/consul`: Consul 存储实现（需 build tag `consul` + 依赖）。
- Docs:
  - `docs/design.md`: High-level architecture and control/agent responsibilities.

### Quick start (dev)
Prereq: Go 1.25+（Homebrew 安装示例 GOROOT `/usr/local/Cellar/go/1.25.1/libexec`，请确保 `go env GOROOT` 指向 1.25 以避免工具链混用）。

```bash
# run controller
go run ./cmd/controller --token="changeme" \
  --tls-cert=server.crt --tls-key=server.key \  # 可选
  --store=memory

# in another shell, run the agent (uses localhost controller by default), writing configs to ./out
NODE_ID=edge-1 go run ./cmd/agent \
  --pub="<wg-public-key>" \
  --priv="<wg-private-key>" \
  --overlay-ip="10.10.1.1/32" \
  --endpoints="203.0.113.1:51820" \
  --cidrs="10.10.1.0/24" \
  --asn=65000 \
  --token="changeme" \
  --health-interval=30s \
  --ca=ca.crt --cert=agent.crt --key=agent.key \ # 可选 mTLS
  --apply=false # 设为 true 将尝试 wg-quick/vtysh 应用，需 root 且本机已安装 wireguard/frr
```

Endpoints (dev):
- `POST /api/v1/nodes/register` — headers: `X-Auth-Token: changeme` — body: `{"id":"edge-1","publicKey":"<wg pub>","endpoints":["203.0.113.1:51820"],"cidrs":["10.10.1.0/24"],"overlayIp":"10.10.1.1/32","listenPort":51820,"asn":65000,"force":false}`
- `GET /api/v1/nodes` — list registered nodes
- `POST /api/v1/health` — node health report (headers include token)
- `GET /api/v1/health` — list latest health reports
- `GET /api/v1/audit` — recent audit entries
- `GET /api/v1/plan?nodeId=` — 动态 Peer 计划（基于健康/延迟），Agent 可轮询
- `GET /healthz` — liveness probe
- `GET /ui/` — 简易 Web UI（需浏览器，token/地址在页面输入）
  - 展示节点、健康、审计；输入 Node ID 可查看动态计划、健康详情（延迟/丢包/FRR 邻居）与基于健康的拓扑表

TLS/Consul（可选）:
- 控制器支持 `--tls-cert/--tls-key` 启用 HTTPS。
- Agent 支持 `--ca` 自定义 CA、`--cert/--key` 客户端证书（mTLS）、`--insecure` 跳过校验（调试用）。
- 存储默认 memory；`--store=consul` 需使用 build tag `consul` 并提供 Consul 依赖（KV 已接入，watch/lock 仍占位）。
### Next steps
- Replace the in-memory store with Consul KV/service discovery.
- Wire up WireGuard key generation/distribution and FRR config rendering/push.
- Add authN/Z (mTLS or bootstrap tokens) and audit logs.
- Expand API: topology plans, policies/ACL, QoS, config versions/rollbacks.
- Build the Web UI over the API (nodes, routes, topology, audit).
