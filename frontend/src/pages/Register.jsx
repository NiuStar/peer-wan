import React, { useState } from 'react';
import { Layout, Card, Form, Input, Button, Space, message } from 'antd';
import { storage } from '../api';

export default function Register({ onSwitch, onAuthed }) {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);

  const submit = async () => {
    const values = await form.validateFields();
    const base = values.base.replace(/\/+$/, '');
    setLoading(true);
    try {
      const res = await fetch(base + '/api/v1/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: values.user, password: values.pass }),
      });
      if (!res.ok) throw new Error('注册失败 ' + res.status);
      const data = await res.json();
      if (!data.token) throw new Error('未返回 token');
      storage.base = base; storage.token = data.token; storage.adminSet = true;
      onAuthed();
    } catch (e) {
      message.error(e.message || '注册失败');
    } finally {
      setLoading(false);
    }
  };

  return (
    <Layout style={{ minHeight: '100vh', background: '#f7f8fa', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <Card style={{ width: 400, borderRadius: 12, boxShadow: '0 10px 30px rgba(0,0,0,0.1)' }}>
        <h2 style={{ textAlign: 'center', marginBottom: 16 }}>注册管理员</h2>
        <Form form={form} layout="vertical" initialValues={{ base: storage.base }}>
          <Form.Item name="base" label="API 基址" rules={[{ required: true }]}>
            <Input placeholder="http://127.0.0.1:8080" />
          </Form.Item>
          <Form.Item name="user" label="用户名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="pass" label="密码" rules={[{ required: true }]}>
            <Input.Password />
          </Form.Item>
          <Space style={{ display: 'flex', justifyContent: 'space-between' }}>
            <Button type="link" onClick={onSwitch}>已有账号? 去登录</Button>
            <Button type="primary" loading={loading} onClick={submit}>注册</Button>
          </Space>
        </Form>
      </Card>
    </Layout>
  );
}
