import { useEffect, useId, useRef, useState, type RefObject } from 'react';
import type { TextAreaRef } from 'antd/es/input/TextArea';
import type { ReferenceCandidate, ScriptReference } from '../timelineContracts';
import { candidateLabel, insertReference, rebaseReferences } from '../scriptReferences';

interface Choice {key:string;label:string;text?:string;candidate?:ReferenceCandidate}
export function ScriptCompletion({editor,body,references,directory,onEdit}:{editor:RefObject<TextAreaRef|null>;body:string;references:ScriptReference[];directory:ReferenceCandidate[];onEdit:(body:string,references:ScriptReference[])=>void}) {
 const [range,setRange]=useState<{start:number;end:number;query:string;reference:boolean}|null>(null),[active,setActive]=useState(0),composing=useRef(false);
 const id=useId(),popup=useRef<HTMLDivElement>(null),[position,setPosition]=useState({left:0,top:0,width:0,maxHeight:180});
 const matching=range?.reference?directory.filter(c=>candidateLabel(c).toLocaleLowerCase().includes(range.query.toLocaleLowerCase())):[];
 const choices:Choice[]=range?(range.reference?matching.slice(0,80).map((c,i)=>({key:`${c.target_id}:${c.clip_id ?? ''}:${i}`,label:candidateLabel(c),candidate:c})):[{key:'reference',label:'@ 素材或标记引用',text:'{@'},{key:'subtitle',label:'字幕 · 指定文字与字号',text:'{"字幕文字", 字号:32px}'},{key:'black',label:'黑场淡变',text:'{黑屏}'},{key:'white',label:'白场淡变',text:'{白屏}'},{key:'dissolve',label:'交叉溶解',text:'{交叉溶解}'}]):[];
 const place=()=>{const el=editor.current?.resizableTextArea?.textArea;if(!el)return;const r=el.getBoundingClientRect(),below=window.innerHeight-r.bottom-12,above=r.top-12,h=Math.max(0,Math.min(220,Math.max(below,above)));setPosition({left:Math.max(8,r.left),top:below>=Math.min(220,above)?r.bottom+4:Math.max(8,r.top-h-4),width:Math.max(0,Math.min(r.width,window.innerWidth-r.left-8)),maxHeight:h});};
 const refresh=()=>{if(composing.current)return;const el=editor.current?.resizableTextArea?.textArea;if(!el||document.activeElement!==el)return;const caret=el.selectionStart,prefix=el.value.slice(0,caret),start=prefix.lastIndexOf('{');if(start<0||prefix.slice(start).includes('}')||(start>0&&prefix[start-1]==='\\')){setRange(null);return;}const token=prefix.slice(start+1),tail=el.value.slice(caret),closing=tail.indexOf('}'),end=closing>=0&&!/[{\n]/.test(tail.slice(0,closing))?caret+closing+1:caret;place();if(token.startsWith('@')){setRange({start,end,query:token.slice(1),reference:true});setActive(0);}else if(!token){setRange({start,end:caret,query:'',reference:false});setActive(0);}else setRange(null);};
 const choose=(choice:Choice)=>{if(!range)return;let nextBody:string,nextReferences:ScriptReference[],caret:number;
  if(choice.candidate){const result=insertReference(body,references,range.start,range.end,choice.candidate);nextBody=result.body;nextReferences=result.references;caret=result.caret;}
  else{const text=choice.text!;nextBody=body.slice(0,range.start)+text+body.slice(range.end);nextReferences=rebaseReferences(body,nextBody,references);caret=range.start+text.length;}
  onEdit(nextBody,nextReferences);setRange(null);requestAnimationFrame(()=>{const el=editor.current?.resizableTextArea?.textArea;el?.focus();el?.setSelectionRange(caret,caret);if(choice.key==='reference')refresh();});
 };
 useEffect(()=>{const el=editor.current?.resizableTextArea?.textArea;if(!el)return;const key=(event:KeyboardEvent)=>{if(!range||event.isComposing||composing.current)return;if(event.key==='Escape'){event.preventDefault();event.stopPropagation();setRange(null);}else if(event.key==='ArrowDown'||event.key==='ArrowUp'){event.preventDefault();event.stopPropagation();setActive(value=>(value+(event.key==='ArrowDown'?1:Math.max(1,choices.length)-1))%Math.max(1,choices.length));}else if((event.key==='Enter'||event.key==='Tab')&&choices.length){event.preventDefault();event.stopPropagation();choose(choices[active % choices.length]);}};
  const begin=()=>{composing.current=true;setRange(null);},end=()=>{composing.current=false;requestAnimationFrame(refresh);};
  const up=(event:KeyboardEvent)=>{if(!['ArrowDown','ArrowUp','Escape','Enter','Tab'].includes(event.key))refresh();};
  const blur=(event:FocusEvent)=>{if(!(event.relatedTarget instanceof Node)||!popup.current?.contains(event.relatedTarget))setRange(null);};
  el.setAttribute('aria-autocomplete','list');el.setAttribute('aria-controls',id);el.setAttribute('aria-expanded',String(!!range));if(range&&choices.length)el.setAttribute('aria-activedescendant',`${id}-${active%choices.length}`);else el.removeAttribute('aria-activedescendant');
  el.addEventListener('keydown',key);el.addEventListener('keyup',up);el.addEventListener('click',refresh);el.addEventListener('blur',blur);el.addEventListener('compositionstart',begin);el.addEventListener('compositionend',end);window.addEventListener('resize',place);window.addEventListener('scroll',place,true);
  return()=>{el.removeEventListener('keydown',key);el.removeEventListener('keyup',up);el.removeEventListener('click',refresh);el.removeEventListener('blur',blur);el.removeEventListener('compositionstart',begin);el.removeEventListener('compositionend',end);window.removeEventListener('resize',place);window.removeEventListener('scroll',place,true);};
 });
 useEffect(()=>{refresh();},[body]);
 useEffect(()=>{popup.current?.querySelector('[aria-selected=true]')?.scrollIntoView({block:'nearest'});},[active]);
 useEffect(()=>()=>{const el=editor.current?.resizableTextArea?.textArea;['aria-autocomplete','aria-controls','aria-expanded','aria-activedescendant'].forEach(attr=>el?.removeAttribute(attr));},[]);
 if(!range)return null;
 return <div ref={popup} id={id} className="script-completion" style={position} role="listbox" aria-label="脚本语法补全">{choices.length?choices.map((choice,index)=><button id={`${id}-${index}`} key={choice.key} type="button" tabIndex={-1} role="option" aria-selected={active%choices.length===index} className={active%choices.length===index?'is-active':''} onMouseDown={event=>event.preventDefault()} onClick={()=>choose(choice)}>{choice.label}</button>):<span>没有匹配对象，可搜索名称、类型、素材或序列。</span>}{matching.length>80&&<span>显示前 80 项；继续输入所属素材、序列或时码可缩小范围。</span>}</div>;
}
