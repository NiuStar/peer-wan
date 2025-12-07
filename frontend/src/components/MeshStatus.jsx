import React, { useMemo } from 'react';
import { Card, List, Tag } from 'antd';

const WIDTH = 900;
const HEIGHT = 420;

// MeshMap renders nodes/links on a simple equirectangular plane.
export function MeshMap({ nodes = [], links = [], onNodeClick, highlightPath = [] }) {
  const positions = useMemo(() => {
    if (!nodes?.length) return {};
    let unknownIndex = 0;
    return nodes.reduce((acc, n, idx) => {
      const loc = n.location || {};
      let x; let y;
      if (typeof loc.lat === 'number' && typeof loc.lng === 'number') {
        x = ((loc.lng + 180) / 360) * WIDTH;
        y = ((90 - loc.lat) / 180) * HEIGHT;
      } else {
        // fallback: spread unknown nodes on a circle
        const angle = (unknownIndex / Math.max(nodes.length, 1)) * Math.PI * 2;
        x = WIDTH / 2 + Math.cos(angle) * 180;
        y = HEIGHT / 2 + Math.sin(angle) * 120;
        unknownIndex += 1;
      }
      acc[n.id] = { x, y };
      return acc;
    }, {});
  }, [nodes]);

  const pathSet = useMemo(() => {
    const s = new Set();
    for (let i = 0; i < highlightPath.length - 1; i += 1) {
      s.add(`${highlightPath[i]}->${highlightPath[i + 1]}`);
      s.add(`${highlightPath[i + 1]}->${highlightPath[i]}`);
    }
    return s;
  }, [highlightPath]);

  return (
    <div style={{ position: 'relative', height: HEIGHT, width: '100%', background: 'radial-gradient(circle at 20% 20%, #0f1b2b, #050910)', borderRadius: 12, overflow: 'hidden' }}>
      <svg width="100%" height={HEIGHT} style={{ position: 'absolute', left: 0, top: 0 }}>
        {links.map((l, idx) => {
          const a = positions[l.from];
          const b = positions[l.to];
          if (!a || !b) return null;
          const key = `${l.from}-${l.to}-${idx}`;
          const color = l.ok ? '#38f39d' : '#ff4d4f';
          const strokeWidth = pathSet.has(`${l.from}->${l.to}`) ? 3 : 1.6;
          return <line key={key} x1={a.x} y1={a.y} x2={b.x} y2={b.y} stroke={color} strokeWidth={strokeWidth} strokeDasharray={l.ok ? '4 0' : '6 4'} opacity={0.85} />;
        })}
      </svg>
      {nodes.map((n) => {
        const pos = positions[n.id];
        if (!pos) return null;
        const critical = links.some(l => (!l.ok && (l.from === n.id || l.to === n.id)));
        const size = n.current ? 16 : 12;
        return (
          <div
            key={n.id}
            onClick={() => onNodeClick?.(n)}
            style={{
              position: 'absolute',
              left: pos.x - size / 2,
              top: pos.y - size / 2,
              width: size,
              height: size,
              borderRadius: '50%',
              background: critical ? '#ff4d4f' : '#9de1ff',
              boxShadow: critical ? '0 0 12px rgba(255,77,79,0.7), 0 0 20px rgba(255,77,79,0.4)' : '0 0 10px rgba(157,225,255,0.8)',
              cursor: onNodeClick ? 'pointer' : 'default',
              transition: 'transform 0.2s',
            }}
            title={n.id}
          >
            <span style={{ position: 'absolute', top: size + 4, left: -4, whiteSpace: 'nowrap', color: '#e6e8f0', fontSize: 12 }}>{n.id}</span>
          </div>
        );
      })}
    </div>
  );
}

export default function MeshStatus({ data, onNodeClick }) {
  const downLinks = (data?.links || []).filter(l => !l.ok);
  return (
    <Card className="card" title="状态中心（全网连通）" style={{ marginTop: 16 }}>
      <MeshMap nodes={data?.nodes || []} links={data?.links || []} onNodeClick={onNodeClick} />
      <div style={{ marginTop: 12 }}>
        <List
          size="small"
          header={`异常段：${downLinks.length}`}
          dataSource={downLinks}
          locale={{ emptyText: '全部已连通' }}
          renderItem={(item) => (
            <List.Item>
              <Tag color="red">{item.from}</Tag>
              <span style={{ margin: '0 6px' }}>→</span>
              <Tag color="red">{item.to}</Tag>
              <span style={{ color: '#999' }}>{item.reason || '不通'}</span>
              {typeof item.latencyMs === 'number' && <Tag color="blue">lat {item.latencyMs}ms</Tag>}
              {typeof item.packetLoss === 'number' && <Tag color="orange">loss {item.packetLoss}%</Tag>}
            </List.Item>
          )}
        />
      </div>
    </Card>
  );
}
