import { useState } from 'react';
import { Button, Flex, Input, Select, Typography } from 'antd';
import { useSequenceEditor } from '../sequenceEditor';
import { errorText } from './ui';
import { scriptTimelineStarts, type ScriptDocument, type ScriptSegment } from '../scriptWorkbench';
import { fpsValue, frameLabel, parseTimecode } from '../timelineContracts';

function safeName(notes: string, fallback: string) {
  const name = notes.replace(/[{}]/g, '').trim();
  return name || fallback;
}

/** Marker list inside the script dock: jump to a timeline marker, or drop one from a script time. */
export function ScriptTimelineMarkers({ canEdit, document, segments, onInsert }: {
  canEdit: boolean;
  document?: ScriptDocument;
  segments: ScriptSegment[];
  onInsert: (token: string) => void;
}) {
  const e = useSequenceEditor();
  const s = e.view?.sequence;
  const [label, setLabel] = useState('');
  const [time, setTime] = useState('');
  const [paragraph, setParagraph] = useState<number | undefined>();
  const [failure, setFailure] = useState('');
  const points = (s?.markers ?? []).filter(m => m.kind === 'point');
  const starts = document ? scriptTimelineStarts({ segments, document }) : [];
  const rows = document ? document.paragraphs.map((p, index) => ({
    index,
    seconds: starts[index] ?? 0,
    label: (p.instruction || p.video || `第 ${index + 1} 段`).replace(/\s+/g, ' ').slice(0, 24),
  })) : [];
  const add = async (frame: number, notes: string) => {
    if (!s) return;
    const name = safeName(notes, `标记 ${frameLabel(frame, s.fps)}`);
    setFailure('');
    try {
      await e.command([{ type: 'marker.add', marker: { id: '', notes: name, kind: 'point', frame, locked: false } }], '从脚本添加标记');
      if (canEdit) onInsert(`{@${name}}`);
      e.setHead(frame);
    } catch (reason) {
      setFailure(errorText(reason));
    }
  };
  const createFromField = () => {
    if (!s) return;
    const frame = parseTimecode(time, s.fps);
    if (frame === null) { setFailure('时间无法识别，未创建标记'); return; }
    void add(frame, label);
  };
  const chosen = rows.find(row => row.index === paragraph);
  const paragraphFrame = s && chosen ? Math.max(0, Math.round(chosen.seconds * fpsValue(s.fps))) : 0;
  return <div className="script-marker-jump" aria-label="时间线标记">
    <Typography.Text type="secondary">时间线标记。点击脚本里的 {'{@引用}'} 会跳到对应时间；这里也可以从段落时间落标记。</Typography.Text>
    {points.map(m => <Flex key={m.id} className="script-marker-row" gap={6} align="center">
      <Button size="small" onClick={() => e.setHead(m.frame)}>{m.notes || (s ? frameLabel(m.frame, s.fps) : m.frame)}</Button>
      <Typography.Text type="secondary">{s ? frameLabel(m.frame, s.fps) : m.frame}</Typography.Text>
      <Button size="small" disabled={!canEdit} onClick={() => onInsert(`{@${safeName(m.notes, s ? frameLabel(m.frame, s.fps) : '标记')}}`)}>插入引用</Button>
    </Flex>)}
    {!points.length && <Typography.Text type="secondary">尚无标记点。可在下方填写时间，或在时间线按 M。</Typography.Text>}
    <Flex gap={6} wrap>
      <Input size="small" aria-label="新标记名称" placeholder="标记名称（可空）" value={label} onChange={event => setLabel(event.target.value)} style={{ width: 140 }} />
      <Input size="small" aria-label="新标记时间" placeholder="HH:MM:SS:FF 或秒" value={time} onChange={event => setTime(event.target.value)} onPressEnter={createFromField} style={{ width: 150 }} />
      <Button size="small" disabled={!s || e.pending} onClick={createFromField}>在时间线创建标记</Button>
    </Flex>
    {rows.length > 0 && <Flex gap={6} wrap>
      <Select size="small" aria-label="脚本段落时间" placeholder="选择脚本段落" style={{ minWidth: 180, maxWidth: '100%' }} value={paragraph}
        options={rows.map(row => ({ value: row.index, label: `第 ${row.index + 1} 段 · ${row.seconds.toFixed(2)}s · ${row.label}` }))}
        onChange={value => setParagraph(value)} />
      <Button size="small" disabled={!s || paragraph === undefined} onClick={() => s && e.setHead(paragraphFrame)}>跳到该段</Button>
      <Button size="small" disabled={!s || paragraph === undefined || e.pending} onClick={() => chosen && void add(paragraphFrame, chosen.label || `第 ${chosen.index + 1} 段`)}>为该段落标记</Button>
    </Flex>}
    {failure && <Typography.Text type="danger">{failure}</Typography.Text>}
  </div>;
}
