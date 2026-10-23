import { useEffect, useId, useRef, useState, type RefObject } from 'react';
import type { TextAreaRef } from 'antd/es/input/TextArea';
import type { ReferenceCandidate, ScriptReference } from '../timelineContracts';
import { candidateLabel, insertReference, rebaseReferences } from '../scriptReferences';
import { completionContext, filterFonts, filterParams, filterSnippets, type CompletionQuery } from '../scriptAuthoring.ts';

interface Choice {
  key: string;
  label: string;
  text?: string;
  candidate?: ReferenceCandidate;
  selectStart?: number;
  selectEnd?: number;
  openReference?: boolean;
  font?: string;
}

export function ScriptCompletion({ editor, body, references, directory, fonts = [], onEdit }: {
  editor: RefObject<TextAreaRef | null>;
  body: string;
  references: ScriptReference[];
  directory: ReferenceCandidate[];
  fonts?: string[];
  onEdit: (body: string, references: ScriptReference[]) => void;
}) {
  const [range, setRange] = useState<CompletionQuery | null>(null), [active, setActive] = useState(0), composing = useRef(false);
  const id = useId(), popup = useRef<HTMLDivElement>(null), [position, setPosition] = useState({ left: 0, top: 0, width: 0, maxHeight: 180 });
  const matching = range?.kind === 'reference' ? directory.filter(c => candidateLabel(c).toLocaleLowerCase().includes(range.query.toLocaleLowerCase())) : [];
  const choices: Choice[] = !range ? [] : range.kind === 'reference'
    ? matching.slice(0, 80).map((c, i) => ({ key: `${c.target_id}:${c.clip_id ?? ''}:${i}`, label: candidateLabel(c), candidate: c }))
    : range.kind === 'snippet'
      ? filterSnippets(range.query).map(snippet => ({ key: snippet.key, label: snippet.label, text: snippet.text, selectStart: snippet.selectStart, selectEnd: snippet.selectEnd, openReference: snippet.openReference }))
      : range.kind === 'font'
        ? filterFonts(fonts, range.query).map(font => ({ key: font, label: font, font }))
        : range.pendingQuote ? []
          : filterParams(range.query, range.omit).map(param => ({ key: param.key, label: param.label, text: param.text }));
  const place = () => {
    const el = editor.current?.resizableTextArea?.textArea;
    if (!el) return;
    const r = el.getBoundingClientRect(), below = window.innerHeight - r.bottom - 12, above = r.top - 12, h = Math.max(0, Math.min(220, Math.max(below, above)));
    setPosition({ left: Math.max(8, r.left), top: below >= Math.min(220, above) ? r.bottom + 4 : Math.max(8, r.top - h - 4), width: Math.max(0, Math.min(r.width, window.innerWidth - r.left - 8)), maxHeight: h });
  };
  const refresh = (force = false) => {
    if (composing.current) return;
    const el = editor.current?.resizableTextArea?.textArea;
    if (!el || document.activeElement !== el) return;
    const next = completionContext(el.value, el.selectionStart, force);
    if (!next) { setRange(null); return; }
    place();
    setRange(next);
    setActive(0);
  };
  const choose = (choice: Choice) => {
    if (!range) return;
    let nextBody: string, nextReferences: ScriptReference[], selStart: number, selEnd: number, reopen = false;
    if (choice.candidate) {
      const result = insertReference(body, references, range.start, range.end, choice.candidate);
      nextBody = result.body; nextReferences = result.references; selStart = selEnd = result.caret;
    } else if (choice.font) {
      const closing = body[range.end] === '"' ? '' : '"';
      const text = choice.font.replace(/\\/g, '\\\\').replace(/"/g, '\\"') + closing;
      nextBody = body.slice(0, range.start) + text + body.slice(range.end);
      nextReferences = rebaseReferences(body, nextBody, references);
      selStart = selEnd = range.start + text.length;
    } else {
      const raw = choice.text!;
      const text = range.kind === 'param' && range.needsComma ? `, ${raw}` : raw;
      nextBody = body.slice(0, range.start) + text + body.slice(range.end);
      nextReferences = rebaseReferences(body, nextBody, references);
      const pad = text.length - raw.length;
      selStart = range.start + (choice.selectStart ?? raw.length) + pad;
      selEnd = range.start + (choice.selectEnd ?? raw.length) + pad;
      reopen = !!choice.openReference;
    }
    onEdit(nextBody, nextReferences);
    setRange(null);
    requestAnimationFrame(() => {
      const el = editor.current?.resizableTextArea?.textArea;
      el?.focus();
      el?.setSelectionRange(selStart, selEnd);
      if (reopen) refresh();
    });
  };
  useEffect(() => {
    const el = editor.current?.resizableTextArea?.textArea;
    if (!el) return;
    const key = (event: KeyboardEvent) => {
      if (event.isComposing || composing.current) return;
      if ((event.ctrlKey || event.metaKey) && event.key === ' ' && !event.altKey) {
        event.preventDefault();
        event.stopPropagation();
        refresh(true);
        return;
      }
      if (!range) return;
      if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); setRange(null); }
      else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') { event.preventDefault(); event.stopPropagation(); setActive(value => (value + (event.key === 'ArrowDown' ? 1 : Math.max(1, choices.length) - 1)) % Math.max(1, choices.length)); }
      else if ((event.key === 'Enter' || event.key === 'Tab') && choices.length) { event.preventDefault(); event.stopPropagation(); choose(choices[active % choices.length]); }
    };
    const begin = () => { composing.current = true; setRange(null); }, end = () => { composing.current = false; requestAnimationFrame(() => refresh()); };
    const up = (event: KeyboardEvent) => {
      if (['ArrowDown', 'ArrowUp', 'Escape', 'Enter', 'Tab'].includes(event.key)) return;
      if ((event.ctrlKey || event.metaKey) && event.key === ' ') return;
      refresh();
    };
    const click = () => refresh();
    const blur = (event: FocusEvent) => { if (!(event.relatedTarget instanceof Node) || !popup.current?.contains(event.relatedTarget)) setRange(null); };
    el.setAttribute('aria-autocomplete', 'list');
    el.setAttribute('aria-controls', id);
    el.setAttribute('aria-expanded', String(!!range));
    if (range && choices.length) el.setAttribute('aria-activedescendant', `${id}-${active % choices.length}`);
    else el.removeAttribute('aria-activedescendant');
    el.addEventListener('keydown', key);
    el.addEventListener('keyup', up);
    el.addEventListener('click', click);
    el.addEventListener('blur', blur);
    el.addEventListener('compositionstart', begin);
    el.addEventListener('compositionend', end);
    window.addEventListener('resize', place);
    window.addEventListener('scroll', place, true);
    return () => {
      el.removeEventListener('keydown', key);
      el.removeEventListener('keyup', up);
      el.removeEventListener('click', click);
      el.removeEventListener('blur', blur);
      el.removeEventListener('compositionstart', begin);
      el.removeEventListener('compositionend', end);
      window.removeEventListener('resize', place);
      window.removeEventListener('scroll', place, true);
    };
  });
  useEffect(() => { refresh(); }, [body]);
  useEffect(() => { popup.current?.querySelector('[aria-selected=true]')?.scrollIntoView({ block: 'nearest' }); }, [active]);
  useEffect(() => () => { const el = editor.current?.resizableTextArea?.textArea; ['aria-autocomplete', 'aria-controls', 'aria-expanded', 'aria-activedescendant'].forEach(attr => el?.removeAttribute(attr)); }, []);
  if (!range) return null;
  const empty = range.kind === 'reference'
    ? '没有匹配对象，可搜索名称、类型、素材或序列。'
    : range.kind === 'font'
      ? '没有匹配的本机字体，可继续手写名称。'
      : range.pendingQuote
        ? '先闭合字幕引号，再补字号或字体。'
        : range.kind === 'param'
          ? '没有匹配的字幕参数。可用字号或字体。'
          : '没有匹配的语法。可继续输入黑屏、白屏、交叉溶解、字幕或 @。';
  return <div ref={popup} id={id} className="script-completion" style={position} role="listbox" aria-label="脚本语法补全">{choices.length ? choices.map((choice, index) => <button id={`${id}-${index}`} key={choice.key} type="button" tabIndex={-1} role="option" aria-selected={active % choices.length === index} className={active % choices.length === index ? 'is-active' : ''} onMouseDown={event => event.preventDefault()} onClick={() => choose(choice)}>{choice.label}</button>) : <span>{empty}</span>}{matching.length > 80 && <span>显示前 80 项；继续输入所属素材、序列或时码可缩小范围。</span>}</div>;
}
