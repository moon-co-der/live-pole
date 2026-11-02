import { useEffect, useRef, useState } from 'react';

function shortDate(value) {
  if (value?.includes('T')) {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', hour: '2-digit', timeZone: 'UTC' }).format(date);
  }
  const date = new Date(`${value}T00:00:00Z`);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat('en', { month: 'short', day: 'numeric', timeZone: 'UTC' }).format(date);
}

export default function AnalyticsLineChart({ data = [], height = 280 }) {
  const [activeIndex, setActiveIndex] = useState(-1);
  const [size, setSize] = useState({ width: 760, height });
  const wrapRef = useRef(null);
  const values = data.map((point) => Number(point.votes || 0));
  useEffect(() => {
    const element = wrapRef.current;
    if (!element) return undefined;
    const updateSize = () => {
      const svg = element.querySelector('svg');
      if (!svg) return;
      const rect = svg.getBoundingClientRect();
      if (!rect.width || !rect.height) return;
      setSize((current) => current.width === rect.width && current.height === rect.height ? current : { width: rect.width, height: rect.height });
    };
    const observer = new ResizeObserver(updateSize);
    observer.observe(element);
    updateSize();
    return () => observer.disconnect();
  }, [data.length, height]);
  if (!data.length || values.every((value) => value === 0)) return <div className="chart-empty"><span aria-hidden="true">⌁</span><strong>No votes yet</strong><p>Responses will appear here as people vote.</p></div>;

  const width = size.width;
  const chartHeight = size.height;
  const pad = { top: 18, right: 18, bottom: 38, left: 42 };
  const max = Math.max(3, Math.ceil(Math.max(...values) / 3) * 3);
  const plotWidth = width - pad.left - pad.right;
  const plotHeight = chartHeight - pad.top - pad.bottom;
  const x = (index) => pad.left + (index * plotWidth) / Math.max(1, data.length - 1);
  const y = (value) => pad.top + plotHeight - (value / max) * plotHeight;
  const path = values.map((value, index) => `${index ? 'L' : 'M'} ${x(index)} ${y(value)}`).join(' ');
  const area = `${path} L ${x(values.length - 1)} ${chartHeight - pad.bottom} L ${x(0)} ${chartHeight - pad.bottom} Z`;
  const tickValues = [0, Math.round(max / 3), Math.round(max * 2 / 3), max];
  const point = activeIndex >= 0 ? data[activeIndex] : null;
  const tooltipX = point ? Math.max(pad.left, Math.min(width - 170, x(activeIndex) - 80)) : 0;
  const tooltipY = point ? Math.max(3, y(values[activeIndex]) - 43) : 0;

  return (
    <div ref={wrapRef} className="analytics-chart-wrap">
      <svg className="analytics-line-chart" viewBox={`0 0 ${width} ${chartHeight}`} preserveAspectRatio="none" role="img" aria-label="Votes over time" onPointerLeave={() => setActiveIndex(-1)}>
        <defs><linearGradient id="chart-fill" x1="0" x2="0" y1="0" y2="1"><stop offset="0" stopColor="var(--purple)" stopOpacity=".15"/><stop offset="1" stopColor="var(--purple)" stopOpacity="0"/></linearGradient></defs>
        {tickValues.map((tick, index) => {
          const tickY = y(tick);
          return <g className="chart-gridline" key={`${tick}-${index}`}><line x1={pad.left} x2={width - pad.right} y1={tickY} y2={tickY}/><text x={pad.left - 12} y={tickY + 4} textAnchor="end">{tick}</text></g>;
        })}
        <path d={area} className="chart-area"/>
        <path d={path} className="chart-line"/>
        {values.map((value, index) => <circle className="chart-point" key={data[index].date} cx={x(index)} cy={y(value)} r="5" tabIndex="0" aria-label={`${shortDate(data[index].date)} · ${value} ${value === 1 ? 'vote' : 'votes'}`} onPointerEnter={() => setActiveIndex(index)} onFocus={() => setActiveIndex(index)} onBlur={() => setActiveIndex(-1)}><title>{shortDate(data[index].date)} · {value} {value === 1 ? 'vote' : 'votes'}</title></circle>)}
        {point && <g className="chart-tooltip" pointerEvents="none"><rect x={tooltipX} y={tooltipY} width="152" height="30" rx="6"/><text x={tooltipX + 10} y={tooltipY + 19}>{shortDate(point.date)} · {values[activeIndex]} {values[activeIndex] === 1 ? 'vote' : 'votes'}</text></g>}
        {data.map((item, index) => data.length > 12 && index % 3 !== 0 ? null : <text className={`chart-x-label${index % 2 === 1 ? ' is-alternate' : ''}`} key={`date-${item.date}`} x={x(index)} y={chartHeight - 8} textAnchor="middle">{shortDate(item.date)}</text>)}
      </svg>
    </div>
  );
}
