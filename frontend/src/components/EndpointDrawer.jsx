import React, { useEffect, useState } from 'react';
import { Modal, Form, Input, message, Select } from 'antd';
import { api } from '../api';

export default function EndpointDrawer({ open, onClose, node, nodes }) {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!node) return;
    load();
  }, [node]);

  const load = async () => {
    try {
      const res = await api('/api/v1/nodes');
      const target = res.find(n => n.id === node.id) || {};
      form.setFieldsValue({ peerEndpoints: target.peerEndpoints || {} });
    } catch (e) { message.error('加载节点失败'); }
  };

  const submit = async () => {
    const v = await form.validateFields();
    setLoading(true);
    try {
      await api('/api/v1/nodes/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id: node.id,
          peerEndpoints: v.peerEndpoints,
          force: true,
        }),
      });
      message.success('Endpoint 已更新');
      onClose();
    } catch (e) {
      message.error('保存失败: ' + e);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal open={open} title={`Endpoint 配置 - ${node?.id || ''}`} onCancel={onClose} onOk={submit} confirmLoading={loading}>
      <Form form={form} layout="vertical">
        {(nodes || []).filter(n => n.id !== node?.id).map(n => {
          const opts = (n.endpoints || []).map(ep => ({ label: ep, value: ep }));
          return (
            <Form.Item key={n.id} label={`连接到 ${n.id}`}>
              <Form.Item name={['peerEndpoints', n.id]} noStyle>
                <Select
                  allowClear
                  showSearch
                  placeholder="选择对端 endpoint"
                  options={opts}
                  style={{ width: '100%' }}
                  optionFilterProp="label"
                />
              </Form.Item>
            </Form.Item>
          );
        })}
      </Form>
    </Modal>
  );
}
