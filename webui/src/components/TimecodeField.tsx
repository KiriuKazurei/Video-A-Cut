import { useRef, useState } from 'react';
import { Input } from 'antd';
import { frameLabel, parseTimecode, type FrameRate } from '../timelineContracts';

/** Click the readout, type HH:MM:SS:FF or seconds, Enter seeks. Invalid text reverts. */
export function TimecodeField({ label, frame, rate, onSeek }: { label: string; frame: number; rate: FrameRate; onSeek: (frame: number) => void }) {
  const [editing, setEditing] = useState(false);
  const [text, setText] = useState('');
  const cancel = useRef(false);
  const finishing = useRef(false);
  const open = () => { cancel.current = false; finishing.current = false; setText(frameLabel(frame, rate)); setEditing(true); };
  const commit = () => {
    if (finishing.current) return;
    finishing.current = true;
    if (cancel.current) { cancel.current = false; setEditing(false); return; }
    const next = parseTimecode(text, rate);
    setEditing(false);
    if (next === null || next === frame) return;
    onSeek(next);
  };
  if (!editing) {
    return <button type="button" className="timecode-readout" aria-label={label} title="点击编辑时间码，Enter 跳转" onClick={open}>{frameLabel(frame, rate)}</button>;
  }
  return <Input className="timecode-input" size="small" autoFocus aria-label={label} value={text}
    onChange={event => setText(event.target.value)}
    onBlur={commit}
    onKeyDown={event => {
      event.stopPropagation();
      if (event.key === 'Enter') { event.preventDefault(); commit(); }
      else if (event.key === 'Escape') { event.preventDefault(); cancel.current = true; setEditing(false); }
    }} />;
}
