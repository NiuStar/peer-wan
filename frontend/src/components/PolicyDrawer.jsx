import React, { useEffect, useMemo, useState } from 'react';
import { Modal, Form, Row, Col, Input, Select, Checkbox, Button, Card, Typography, Space, message, Tag, List } from 'antd';
import { api } from '../api';
import { MeshMap } from './MeshStatus';
import { useWS } from '../hooks/useWS';

const { Text } = Typography;

const RuleEditor = ({ onAdd, viaOptions, pathSelected }) => {
  const [form] = Form.useForm();
  const add = async () => {
    const v = await form.validateFields();
    onAdd(v);
    form.resetFields();
  };
  return (
    <Card size="small" style={{ marginBottom: 12 }} title="新增规则">
      <Form form={form} layout="inline">
        <Form.Item name="prefix"><Input placeholder="前缀或 geoip:CN" style={{ width: 160 }} /></Form.Item>
        <Form.Item name="domains"><Input placeholder="域名,逗号分隔" style={{ width: 200 }} /></Form.Item>
        <Form.Item name="viaNode" rules={pathSelected ? [] : [{ required: true, message: '经由节点必填' }]}>
          <Select
            allowClear
            showSearch
            placeholder="经由节点"
            style={{ width: 200 }}
            optionFilterProp="label"
            options={[{ label: '本地直出', value: 'local' }, ...viaOptions]}
          />
        </Form.Item>
        <Button type="primary" onClick={add}>添加</Button>
      </Form>
    </Card>
  );
};

const RuleList = ({ rules, onRemove }) => {
  if (!rules?.length) return <Text type="secondary">无策略规则</Text>;
  return (
    <Card size="small" title="已配置规则">
      {rules.map((r, i) => (
        <div key={i} style={{ marginBottom: 8, display: 'flex', justifyContent: 'space-between' }}>
          <span>
            {r.prefix || '(域名)'} {Array.isArray(r.domains) && r.domains.length ? `[${r.domains.join(',')}]` : ''} → {r.viaNode}
            {r.path?.length ? ` 路径: ${r.path.join(' → ')}` : ''}
          </span>
          <Button size="small" onClick={() => onRemove(i)}>删除</Button>
        </div>
      ))}
    </Card>
  );
};

function PathSelectorModal({ open, onClose, mesh, currentNode, onDone }) {
  const [path, setPath] = useState([]);
  useEffect(() => { if (open) setPath([]); }, [open]);
  const nodesWithCurrent = useMemo(() => (mesh?.nodes || []).map(n => ({ ...n, current: n.id === currentNode?.id })), [mesh, currentNode]);
  const links = mesh?.links || [];

  const onNodeClick = (n) => {
    if (n.id === currentNode?.id) return;
    setPath(prev => [...prev, n.id]);
  };
  const downSegments = useMemo(() => {
    const segs = [];
    for (let i = 0; i < path.length; i += 1) {
      const from = i === 0 ? currentNode?.id : path[i - 1];
      const to = path[i];
      if (!from || !to) continue;
      const hit = links.find(l => (l.from === from && l.to === to) || (l.from === to && l.to === from));
      if (hit && !hit.ok) {
        segs.push({ from, to, reason: hit.reason });
      }
    }
    return segs;
  }, [links, path, currentNode]);

  const finish = () => {
    onDone(path);
    onClose();
  };

  return (
    <Modal open={open} onCancel={onClose} onOk={finish} width={980} title="地图连线，确定路径">
      <MeshMap nodes={nodesWithCurrent} links={links} onNodeClick={onNodeClick} highlightPath={[currentNode?.id, ...path].filter(Boolean)} />
      <div style={{ marginTop: 12 }}>
        <Text>已选路径：{[currentNode?.id, ...path].filter(Boolean).join(' → ') || '未选择'}</Text>
      </div>
      <List
        size="small"
        style={{ marginTop: 8 }}
        header={`异常段：${downSegments.length}`}
        dataSource={downSegments}
        locale={{ emptyText: '所选路径暂未发现异常' }}
        renderItem={(item) => (
          <List.Item>
            <Tag color="red">{item.from}</Tag>
            <span style={{ margin: '0 4px' }}>→</span>
            <Tag color="red">{item.to}</Tag>
            <Text type="secondary">{item.reason || '不通'}</Text>
          </List.Item>
        )}
      />
      <Text type="secondary">提示：点击节点按顺序连线，最后一个即为出口节点。</Text>
    </Modal>
  );
}

export default function PolicyDrawer({ open, onClose, node, reloadNodes, nodes, mesh }) {
  const [rules, setRules] = useState([]);
  const [rawPolicy, setRawPolicy] = useState(null);
  const [viewModal, setViewModal] = useState(false);
  const [form] = Form.useForm();
  const [installLogs, setInstallLogs] = useState([]);
  const [logsLoading, setLogsLoading] = useState(false);
  const [diag, setDiag] = useState(null);
  const [diagLoading, setDiagLoading] = useState(false);
  const [logLines, setLogLines] = useState([]);
  const [tasks, setTasks] = useState([]);
  const options = useMemo(() => (nodes || []).map(n => ({ label: n.id, value: n.id })), [nodes]);
  const [pathModal, setPathModal] = useState(false);
  const [currentPath, setCurrentPath] = useState([]);
  useEffect(() => { if (!node || !open) return; load(); }, [node, open]);
  const load = async () => {
    try {
      const res = await api('/api/v1/policy?nodeId=' + encodeURIComponent(node.id));
      form.setFieldsValue({
        egressPeerId: res.egressPeerId,
        defaultRoute: !!res.defaultRoute,
        bypassCidrs: (res.bypassCidrs || []).join(','),
        defaultRouteNextHop: res.defaultRouteNextHop || undefined,
      });
      setRules(res.policyRules || []);
    } catch (e) { message.error('加载策略失败'); }
  };
  const loadLogs = async () => {
    if (!node) return;
    setLogsLoading(true);
    try {
      const res = await api('/api/v1/policy/status?nodeId=' + encodeURIComponent(node.id) + '&limit=40');
      setInstallLogs(res.items || []);
    } catch (e) {
      // silent to avoid打扰
    } finally {
      setLogsLoading(false);
    }
  };
  useEffect(() => {
    if (!open || !node) return () => {};
    loadLogs();
    loadTasks();
    const t = setInterval(loadLogs, 4000);
    const t2 = setInterval(loadTasks, 4000);
    return () => { clearInterval(t); clearInterval(t2); };
  }, [open, node]);
  const loadTasks = async () => {
    if (!node) return;
    try {
      const res = await api('/api/v1/tasks?nodeId=' + encodeURIComponent(node.id));
      setTasks(res.items || []);
    } catch {
      // ignore
    }
  };
  const loadDiag = async () => {
    if (!node) return;
    // trigger agent实时诊断
    try {
      await api('/api/v1/policy/command', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ nodeId: node.id, action: 'diag' }),
      });
    } catch (e) {
      message.error('下发诊断指令失败: ' + e);
    }
    setDiagLoading(true);
    try {
      const res = await api('/api/v1/policy/diag?nodeId=' + encodeURIComponent(node.id) + '&limit=1');
      setDiag(res.items?.length ? res.items[res.items.length - 1] : null);
    } catch (e) {
      message.error('获取诊断失败: ' + e);
    } finally {
      setDiagLoading(false);
    }
  };
  useWS(node ? `/api/v1/ws/logs?nodeId=${encodeURIComponent(node.id)}` : null, (data) => {
    if (!data?.lines) return;
    setLogLines(prev => [...data.lines.map(l => `[${new Date().toLocaleTimeString()}] ${l}`), ...prev].slice(0, 200));
  });
  const addRule = (values) => {
    const viaNode = currentPath.length ? currentPath[currentPath.length - 1] : values.viaNode;
    const path = currentPath.length ? [...currentPath] : undefined;
    const domains = Array.isArray(values.domains)
      ? values.domains
      : (values.domains || '')
        .split(',')
        .map(s => s.trim())
        .filter(Boolean);
    setRules(prev => [...prev, { ...values, domains, viaNode, path }]);
    setCurrentPath([]);
  };
  const removeRule = (idx) => setRules(rules.filter((_, i) => i !== idx));
  const viewPolicy = async () => {
    if (!node) return;
    try {
      const res = await api('/api/v1/policy?nodeId=' + encodeURIComponent(node.id));
      setRawPolicy(res);
      setViewModal(true);
    } catch (e) { message.error('查看策略失败: ' + e); }
  };
  const submit = async () => {
    const v = await form.validateFields();
    const payload = {
      nodeId: node.id,
      egressPeerId: v.egressPeerId || '',
      defaultRoute: !!v.defaultRoute,
      bypassCidrs: (v.bypassCidrs || '').split(',').map(s => s.trim()).filter(Boolean),
      defaultRouteNextHop: v.defaultRouteNextHop || '',
      policyRules: rules,
    };
    try {
      await api('/api/v1/policy', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      message.success('策略已提交');
      reloadNodes?.();
    } catch (e) { message.error('提交失败: ' + e); }
  };
  const statusColor = (s) => {
    switch (s) {
      case 'success': return 'green';
      case 'failed': return 'red';
      case 'applying': return 'blue';
      case 'checking': return 'orange';
      default: return 'default';
    }
  };
  const latest = installLogs.length ? installLogs[installLogs.length - 1] : null;
  return (
    <Modal title={`策略配置 - ${node?.id || ''}`} open={open} onCancel={onClose} onOk={submit} width={720}>
      <Form layout="vertical" form={form}>
        <Row gutter={12}>
          <Col span={8}>
            <Form.Item name="egressPeerId" label="出口 PeerID">
              <Select allowClear showSearch options={options} placeholder="选择出口节点" optionFilterProp="label" />
            </Form.Item>
          </Col>
          <Col span={8}>
            <Form.Item name="defaultRouteNextHop" label="兜底下一跳节点">
              <Select allowClear showSearch options={options} placeholder="不填则用出口 Peer" optionFilterProp="label" />
            </Form.Item>
          </Col>
          <Col span={8}><Form.Item name="bypassCidrs" label="旁路 CIDR(逗号)"><Input placeholder="172.19.3.0/24" /></Form.Item></Col>
        </Row>
        <Form.Item name="defaultRoute" valuePropName="checked"><Checkbox>启用兜底默认路由（自动策略路由）</Checkbox></Form.Item>
      </Form>
      <div style={{ marginTop: 12, marginBottom: 8 }}>
        <Space>
          <Text>当前路径：</Text>
          <Text type="secondary">{currentPath.length ? [node?.id, ...currentPath].join(' → ') : '未选择，默认直达出口节点'}</Text>
          <Button size="small" onClick={() => setPathModal(true)}>打开地图连线</Button>
          <Button size="small" onClick={viewPolicy}>查看接口配置</Button>
          {mesh?.links && <Text type="secondary">异常段 {mesh.links.filter(l => !l.ok).length}</Text>}
        </Space>
      </div>
      <RuleEditor onAdd={addRule} viaOptions={options} pathSelected={currentPath.length > 0} />
      <RuleList rules={rules} onRemove={removeRule} />
      <PathSelectorModal open={pathModal} onClose={() => setPathModal(false)} mesh={mesh} currentNode={node} onDone={setCurrentPath} />
      <Card size="small" title="安装进度" style={{ marginTop: 12 }} extra={<Button size="small" loading={logsLoading} onClick={loadLogs}>手动刷新</Button>}>
        {latest ? (
          <div style={{ marginBottom: 8 }}>
            <Space>
              <Text>当前状态：</Text>
              <Tag color={statusColor(latest.status)}>{latest.status}</Tag>
              <Text type="secondary">{latest.message}</Text>
            </Space>
          </div>
        ) : <Text type="secondary">暂无状态</Text>}
        <List
          size="small"
          dataSource={[...installLogs].reverse()}
          locale={{ emptyText: '暂无安装日志' }}
          renderItem={(item) => (
            <List.Item>
              <Space direction="vertical" size={2}>
                <Space>
                  <Tag color={statusColor(item.status)}>{item.status}</Tag>
                  <Text>{item.message || '-'}</Text>
                </Space>
                <Text type="secondary" style={{ fontSize: 12 }}>{new Date(item.timestamp).toLocaleString()} {item.version ? `(版本 ${item.version})` : ''}</Text>
                {item.logs?.length ? <Text type="secondary" style={{ fontSize: 12 }}>{item.logs.join(' | ')}</Text> : null}
              </Space>
            </List.Item>
          )}
        />
      </Card>
      <Card size="small" title="策略任务时间线" style={{ marginTop: 12 }}>
        <List
          size="small"
          dataSource={tasks}
          locale={{ emptyText: '暂无任务' }}
          renderItem={(task) => (
            <List.Item>
              <Space direction="vertical" size={4} style={{ width: '100%' }}>
                <Space>
                  <Tag color="blue">{task.type}</Tag>
                  <Text strong>{task.id?.slice(0, 8)}</Text>
                  <Text type="secondary">{task.createdAt ? new Date(task.createdAt).toLocaleString() : ''}</Text>
                </Space>
                {(task.steps || []).map((s, idx) => (
                  <Space key={idx} size={8}>
                    <Tag color={statusColor(s.status)}>{s.name}</Tag>
                    <Text type="secondary">{s.message}</Text>
                    <Text type="secondary" style={{ fontSize: 12 }}>{s.timestamp ? new Date(s.timestamp).toLocaleTimeString() : ''}</Text>
                  </Space>
                ))}
              </Space>
            </List.Item>
          )}
        />
      </Card>
      <Card size="small" title="Agent 日志(WS)" style={{ marginTop: 12, maxHeight: 240, overflow: 'auto' }}>
        <pre style={{ whiteSpace: 'pre-wrap', margin: 0, fontSize: 12 }}>
          {logLines.length ? logLines.join('\n') : '等待日志...'}
        </pre>
      </Card>
      <Card size="small" title="策略诊断" style={{ marginTop: 12 }} extra={<Button size="small" loading={diagLoading} onClick={loadDiag}>诊断</Button>}>
        {diag ? (
          <>
            <Space style={{ marginBottom: 8 }}>
              <Tag color={statusColor(diag.summary.includes('错误') ? 'failed' : diag.summary.includes('警告') ? 'warn' : 'success')}>
                {diag.summary}
              </Tag>
              <Text type="secondary">{new Date(diag.timestamp).toLocaleString()}</Text>
            </Space>
            <List
              size="small"
              dataSource={diag.checks || []}
              renderItem={(item) => (
                <List.Item>
                  <Space>
                    <Tag color={statusColor(item.status)}>{item.status}</Tag>
                    <Text strong>{item.name}</Text>
                    <Text type="secondary">{item.detail}</Text>
                  </Space>
                </List.Item>
              )}
            />
          </>
        ) : <Text type="secondary">尚无诊断结果，点击诊断按钮获取最新检查。</Text>}
      </Card>
      <Modal open={viewModal} onCancel={() => setViewModal(false)} footer={null} title="接口返回">
        <pre style={{ background: '#0f1322', color: '#9de1ff', padding: 12, borderRadius: 8, maxHeight: 400, overflow: 'auto' }}>
          {rawPolicy ? JSON.stringify(rawPolicy, null, 2) : '无数据'}
        </pre>
      </Modal>
    </Modal>
  );
}
