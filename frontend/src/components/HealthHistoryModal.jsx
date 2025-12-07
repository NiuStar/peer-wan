import React, { useEffect, useState } from 'react';
import { Modal, Table, Tag, Typography, message } from 'antd';
import { api } from '../api';

const { Text } = Typography;

export default function HealthHistoryModal({ node, open, onClose }) {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState([]);

  useEffect(() => {
    if (open && node) load();
  }, [open, node]);

  const load = async () => {
    setLoading(true);
    try {
      const res = await api(`/api/v1/health/history?nodeId=${encodeURIComponent(node.id)}&hours=24`);
      setData(res || []);
    } catch (e) {
      message.error('加载历史失败: ' + e);
    } finally {
      setLoading(false);
    }
  };

  const rows = (data || []).flatMap((h) => {
    return Object.keys(h.latencyMs || {}).map(ip => ({
      timestamp: h.timestamp,
      target: ip,
      latency: h.latencyMs[ip],
      loss: (h.packetLoss || {})[ip],
    }));
  });

  return (
    <Modal open={open} onCancel={onClose} footer={null} width={780} title={`互 ping 历史 - ${node?.id || ''}`}>
      <Text type="secondary">近24小时，按最新在前</Text>
      <Table
        size="small"
        loading={loading}
        dataSource={rows.sort((a, b) => new Date(b.timestamp) - new Date(a.timestamp))}
        rowKey={(r, idx) => `${r.timestamp}-${r.target}-${idx}`}
        pagination={{ pageSize: 10 }}
        columns={[
          { title: '时间', dataIndex: 'timestamp', render: (t) => new Date(t).toLocaleString() },
          { title: '目标 IP', dataIndex: 'target' },
          { title: '延迟(ms)', dataIndex: 'latency' },
          { title: '丢包(%)', dataIndex: 'loss', render: (v) => v === undefined ? '-' : v },
        ]}
      />
    </Modal>
  );
}

