import React, { useEffect } from 'react';
import { Card, Form, Input, Button, message } from 'antd';
import { api } from '../api';

export default function DiagCard({ onChanged }) {
  const [form] = Form.useForm();
  const load = async () => {
    try {
      const res = await api('/api/v1/settings/diag');
      form.setFieldsValue(res);
    } catch (e) {
      message.warning('加载诊断配置失败');
    }
  };
  useEffect(() => { load(); }, []);

  const save = async () => {
    const v = await form.validateFields();
    try {
      await api('/api/v1/settings/diag', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(v),
      });
      message.success('诊断配置已保存');
      onChanged?.();
    } catch (e) {
      message.error('保存失败: ' + e);
    }
  };

  return (
    <Card className="card" title="诊断设置（Agent 互 ping）">
      <Form layout="vertical" form={form}>
        <Form.Item
          name="pingInterval"
          label="互 ping 周期"
          rules={[{ required: true, message: '请输入如 3s/5s/10s' }]}
        >
          <Input placeholder="3s" />
        </Form.Item>
        <Button type="primary" onClick={save}>保存</Button>
      </Form>
    </Card>
  );
}
