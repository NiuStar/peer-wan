import React from 'react';
import { Table, Button, Space } from 'antd';

export default function NodesTable({ data, onPolicy, onInstall, onEndpoints, onDiagnose }) {
  const columns = [
    { title: 'ID', dataIndex: 'id' },
    { title: 'Overlay', dataIndex: 'overlayIp' },
    { title: 'Endpoints', render: (_, r) => (r.endpoints || []).join(', ') },
    { title: 'CIDRs', render: (_, r) => (r.cidrs || []).join(', ') },
    { title: 'Version', dataIndex: 'configVersion' },
    {
      title: '操作', render: (_, r) => (
        <Space>
          <Button size="small" onClick={() => onInstall(r)}>安装命令</Button>
          <Button size="small" onClick={() => onPolicy(r)}>策略</Button>
          <Button size="small" onClick={() => onEndpoints(r)}>Endpoint 配置</Button>
          <Button size="small" onClick={() => onDiagnose?.(r)}>诊断</Button>
        </Space>
      ),
    },
  ];
  return <Table rowKey="id" columns={columns} dataSource={data || []} size="small" pagination={false} />;
}
