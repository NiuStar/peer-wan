import React from 'react';
import { Card } from 'antd';

export default function StatusCard({ data }) {
  return (
    <Card className="card" title="状态">
      <p>Store: {data?.store} ({data?.storeStatus || '-'})</p>
      <p>Consul: {data?.consulAddr || '-'}</p>
      <p>MySQL: {data?.mysql || '-'}</p>
      <p>PublicAddr: {data?.publicAddr || '-'}</p>
      <p>PlanVersion: {data?.planVersion || 0}</p>
      <p>Build: {data?.buildVersion || 'dev'}</p>
    </Card>
  );
}
