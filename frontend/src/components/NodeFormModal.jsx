import React, { useState } from 'react';
import { Modal, Form, Input, Button, message } from 'antd';
import { api } from '../api';

export default function NodeFormModal({ open, onClose, onOk }) {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);

  const submit = async () => {
    const v = await form.validateFields();
    const payload = {
      id: v.id,
      listenPort: Number(v.listenPort || 8082),
    };
    setLoading(true);
    try {
      await api('/api/v1/nodes/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      message.success('节点已创建');
      onOk?.();
      onClose();
    } catch (e) {
      message.error('创建失败: ' + e);
    } finally {
      setLoading(false);
    }
  };

  return (
    <Modal open={open} onCancel={onClose} onOk={submit} title="新增节点" footer={null}>
      <Form form={form} layout="vertical">
        <Form.Item name="id" label="节点 ID" rules={[{ required: true, message: '请输入节点ID' }]}>
          <Input placeholder="如 node-1" />
        </Form.Item>
        <Form.Item name="listenPort" label="监听端口" rules={[{ required: true, message: '请输入端口' }]}>
          <Input placeholder="默认 8082" defaultValue="8082" />
        </Form.Item>
        <Button type="primary" onClick={submit} loading={loading}>提交</Button>
      </Form>
    </Modal>
  );
}
