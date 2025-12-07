import React, { useEffect, useState } from 'react';
import { Card, Form, Input, Button, Row, Col, Space, message } from 'antd';
import { api } from '../api';

export default function GeoIPCard({ onChanged }) {
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const load = async () => {
    try {
      const cfg = await api('/api/v1/settings/geoip');
      form.setFieldsValue(cfg);
    } catch (e) {
      message.warning('加载 GeoIP 设置失败');
    }
  };
  useEffect(() => { load(); }, []);
  const save = async () => {
    const values = await form.validateFields();
    setLoading(true);
    try {
      await api('/api/v1/settings/geoip', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(values),
      });
      message.success('已保存 GeoIP 设置');
      onChanged?.();
    } catch (e) {
      message.error('保存失败: ' + e);
    }
    setLoading(false);
  };
  return (
    <Card className="card" title="GeoIP 设置">
      <Form form={form} layout="vertical">
        <Form.Item name="sourceV4" label="IPv4 源 URL 模板" rules={[{ required: true }]}>
          <Input placeholder="https://.../%s.cidr" />
        </Form.Item>
        <Form.Item name="sourceV6" label="IPv6 源 URL 模板" rules={[{ required: true }]}>
          <Input placeholder="https://.../%s.cidr" />
        </Form.Item>
        <Row gutter={12}>
          <Col span={12}><Form.Item name="cacheDir" label="缓存目录" rules={[{ required: true }]}><Input /></Form.Item></Col>
          <Col span={12}><Form.Item name="cacheTtl" label="缓存TTL" rules={[{ required: true }]}><Input placeholder="24h" /></Form.Item></Col>
        </Row>
        <Space>
          <Button onClick={load}>重载</Button>
          <Button type="primary" loading={loading} onClick={save}>保存</Button>
        </Space>
      </Form>
    </Card>
  );
}
