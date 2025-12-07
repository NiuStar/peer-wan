import React, { useState } from 'react';
import { Layout, Row, Col, Card, Input, Button, Space, Modal, Typography, message, Menu } from 'antd';
import { api, storage, useAsync } from '../api';
import StatusCard from '../components/StatusCard';
import GeoIPCard from '../components/GeoIPCard';
import NodesTable from '../components/NodesTable';
import PolicyDrawer from '../components/PolicyDrawer';
import EndpointDrawer from '../components/EndpointDrawer';
import MeshStatus from '../components/MeshStatus';
import DiagCard from '../components/DiagCard';
import HealthHistoryModal from '../components/HealthHistoryModal';
import DiagnoseModal from '../components/DiagnoseModal';
import NodeFormModal from '../components/NodeFormModal';

const { Title, Text } = Typography;

export default function Admin() {
  const [policyNode, setPolicyNode] = useState(null);
  const [installModal, setInstallModal] = useState({ open: false, text: '' });
  const [endpointNode, setEndpointNode] = useState(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [menuKey, setMenuKey] = useState('status');
  const [historyNode, setHistoryNode] = useState(null);
  const [diagNode, setDiagNode] = useState(null);
  const [showNodeForm, setShowNodeForm] = useState(false);
  const status = useAsync(() => api('/api/v1/info'), [storage.token]);
  const nodes = useAsync(() => api('/api/v1/nodes'), [storage.token]);
  const mesh = useAsync(() => api('/api/v1/status/mesh'), [storage.token]);

  const reloadAll = async () => {
    await Promise.all([status.run(), nodes.run(), mesh.run()]);
  };

  const showInstall = async (node) => {
    try {
      const body = { id: node.id };
      const res = await api('/api/v1/nodes/prepare', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      const cmd = res.command || '';
      setInstallModal({ open: true, text: cmd });
      navigator.clipboard?.writeText(cmd).catch(() => { });
    } catch (e) { message.error('生成安装命令失败: ' + e); }
  };

  const content = menuKey === 'nodes'
    ? (
      <Card className="card" title="节点" extra={<Button type="primary" onClick={() => setShowNodeForm(true)}>新增节点</Button>}>
        <NodesTable data={nodes.data} onPolicy={setPolicyNode} onInstall={showInstall} onEndpoints={setEndpointNode} onDiagnose={setDiagNode} />
      </Card>
    )
    : <MeshStatus data={mesh.data} onNodeClick={setHistoryNode} />;

  return (
    <Layout style={{ minHeight: '100vh', background: '#0b0f1a' }}>
      <Layout.Sider width={180} theme="dark" style={{ background: '#0a111f' }}>
        <div style={{ color: '#e6e8f0', padding: '12px 16px', fontWeight: 600 }}>peer-wan</div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[menuKey]}
          onClick={({ key }) => setMenuKey(key)}
          items={[
            { key: 'status', label: '状态中心' },
            { key: 'nodes', label: '节点管理' },
          ]}
        />
      </Layout.Sider>
      <Layout.Content style={{ padding: 16 }}>
        <div className="header" style={{ marginBottom: 12 }}>
          <Title level={3} style={{ color: '#e6e8f0', margin: 0, flex: 1 }}>{menuKey === 'nodes' ? '节点管理' : '状态中心'}</Title>
          <Space>
            <Input style={{ width: 200 }} defaultValue={storage.base} onBlur={e => storage.base = e.target.value} placeholder="API 基址" />
            <Input style={{ width: 200 }} defaultValue={storage.token} onBlur={e => storage.token = e.target.value} placeholder="Token" />
            <Button onClick={() => setSettingsOpen(true)}>状态 / 配置</Button>
            <Button onClick={() => { storage.token=''; window.location.reload(); }}>退出</Button>
            <Button type="primary" onClick={reloadAll}>刷新</Button>
          </Space>
        </div>

        {content}

        <Modal
          open={settingsOpen}
          onCancel={() => setSettingsOpen(false)}
          footer={null}
          width={980}
          title="状态与配置"
        >
          <Row gutter={16}>
            <Col span={8}><StatusCard data={status.data} /></Col>
            <Col span={8}><GeoIPCard onChanged={reloadAll} /></Col>
            <Col span={8}><DiagCard onChanged={reloadAll} /></Col>
          </Row>
        </Modal>

        <Modal open={installModal.open} onCancel={() => setInstallModal({ open: false, text: '' })} footer={null} title="安装命令">
          <pre style={{ whiteSpace: 'pre-wrap', background: '#0f1322', padding: 12, borderRadius: 8, color: '#9de1ff' }}>{installModal.text}</pre>
          <Text type="secondary">已自动尝试复制到剪贴板</Text>
        </Modal>

        <PolicyDrawer open={!!policyNode} node={policyNode} onClose={() => setPolicyNode(null)} reloadNodes={nodes.run} nodes={nodes.data} mesh={mesh.data} />
        <EndpointDrawer open={!!endpointNode} node={endpointNode} onClose={() => setEndpointNode(null)} nodes={nodes.data} />
        <HealthHistoryModal open={!!historyNode} node={historyNode} onClose={() => setHistoryNode(null)} />
        <DiagnoseModal open={!!diagNode} node={diagNode} onClose={() => setDiagNode(null)} />
        <NodeFormModal open={showNodeForm} onClose={() => setShowNodeForm(false)} onOk={nodes.run} />
      </Layout.Content>
    </Layout>
  );
}
