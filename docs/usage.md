## 使用说明（计划历史/回滚 + UI）

### API 摘要
- `GET /api/v1/version`：全局计划版本。
- `POST /api/v1/nodes/prepare`：仅需节点 ID，返回 ProvisionToken + 安装命令（自动分配 overlay/key）。
- `POST /api/v1/nodes/register`
- `GET /api/v1/nodes`
- `POST /api/v1/health` / `GET /api/v1/health`
- `GET /api/v1/plan?nodeId=&waitVersion=`：动态计划（支持等待版本）。
- `GET /api/v1/plan/history?nodeId=&limit=`：计划历史。
- `POST /api/v1/plan/rollback`：`{"nodeId":"","version":123}`。
- `GET /api/v1/audit`
- `GET /ui/`：Web UI（节点/健康/审计/计划历史/回滚/拓扑）。

### Web UI 操作
- 打开 `http://controller:8080/ui/` 输入 API 地址与 token，首次注册的用户即管理员，之后禁用注册。
- 节点页：查看节点列表，点击“添加节点”仅输入 Node ID，即可生成一键安装命令（含 ProvisionToken/overlay/key）。
- 查看/操作：输入 Node ID，点击“查看计划”“加载历史”“回滚所选版本”（单选历史版本）。
- 历史：展示版本/时间/peers/签名存在与否；回滚成功会提示全局版本更新（Agent 需稍后拉取新计划）。
- 健康/拓扑：查看延迟/丢包/FRR 邻居及基于健康的拓扑表/简图。
- 审计：查看近期操作。

### Agent 长轮询/Consul watch
- 默认使用 `/api/v1/version` + `/api/v1/plan?waitVersion` 长轮询。
- 编译含 `-tags=consul` 时，可通过 Consul watch 全局版本键触发（镜像已带此标签）。

### 回滚流程（示例）
1) 控制器/Agent 正常运行，计划写入 KV（含历史与签名）。
2) 在 UI 选择 Node ID -> “加载历史” -> 选择版本 -> “回滚所选版本”。
3) 控制器校验签名并写回最新计划，更新全局版本键并写审计。
4) Agent 长轮询/Consul watch 感知版本变化，拉取并应用新计划（如开启 `--apply`）。

### 安全/签名
- 计划保存时附带 SHA256 签名（节点 ID + configVersion + peers/AllowedIPs），回滚时校验。
- Agent 可以使用 `Authorization: Bearer <JWT>` 或节点级 `X-Provision-Token` 访问 `/plan`、`/health`。
- mTLS：控制器 `--client-ca`、Agent `--cert/--key/--ca` 可启用双向 TLS；token 作为额外引导校验。
