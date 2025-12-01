## 部署指南（Docker Compose + Consul + mTLS 可选）

### 前置
- Docker / Docker Compose
- 默认为含 Consul 的构建（Dockerfile 使用 `-tags=consul`）。

### 一键启动（开发/演示）
```bash
# 控制器 + Consul
docker compose -f docker-compose.controller.yaml up --build
# 控制器：localhost:8080
# Consul UI：localhost:8500
# Web UI：http://localhost:8080/ui/

# Agent（独立编排，可指向上述控制器）
docker compose -f docker-compose.agent.yaml up --build
```

### 控制器参数（常用）
- `--token`：bootstrap token。
- `--store=consul|memory`：存储后端，镜像默认 consul。
- `--consul-addr`：Consul 地址（默认 `http://consul:8500`）。
- `--tls-cert/--tls-key/--client-ca`：启用 TLS/mTLS。

### Agent 参数（常用）
- `--controller`：控制器地址（可 HTTPS）。
- `--token`：与控制器一致。
- `--overlay-ip/--endpoints/--cidrs/--asn`：隧道/路由信息。
- `--health-interval`、`--plan-interval`：上报健康与拉取计划周期。
- `--ca/--cert/--key`：mTLS 客户端证书。

### Consul 依赖
- Compose 内置 dev Consul，可替换为生产集群，传入 `CONSUL_HTTP_ADDR`。
- 控制器在 Consul 模式下会重算计划并写入 KV（含历史 + 全局版本键），Agent 通过 `waitVersion` 或 Consul watch 感知变更。

### 生产建议
- 将 controller/agent 二进制以 systemd/K8s 运行；Consul 使用多节点 + ACL/mTLS。
- 使用真实 WireGuard/FRR 环境，并在 Agent `--apply` 前确保 root 权限与二进制可用。
