import React, { useEffect, useState } from 'react';
import { Modal, List, Tag, Typography, message, Space } from 'antd';
import { api } from '../api';

const { Text } = Typography;

const colorMap = {
  ok: 'green',
  warn: 'orange',
  fail: 'red',
  info: 'blue',
};

export default function DiagnoseModal({ node, open, onClose }) {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState(null);

  useEffect(() => {
    if (open && node) load();
  }, [open, node]);

  const load = async () => {
    setLoading(true);
    try {
      const res = await api(`/api/v1/diagnose?nodeId=${encodeURIComponent(node.id)}`);
      setData(res);
    } catch (e) {
      message.error('诊断失败: ' + e);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal open={open} onCancel={onClose} footer={null} title={`诊断 - ${node?.id || ''}`} width={700}>
      <Text type="secondary">{data?.summary || '诊断结果'}</Text>
      <List
        loading={loading}
        dataSource={data?.results || []}
        renderItem={(item) => (
          <List.Item>
            <Space size="small">
              <Tag color={colorMap[item.severity] || 'default'}>{item.check}</Tag>
              <Text>{item.detail}</Text>
            </Space>
          </List.Item>
        )}
      />
    </Modal>
  );
}
