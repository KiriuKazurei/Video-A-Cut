import definition from '../../control-plane/internal/project/script_directives.json' with {type:'json'};
import shotcraft from '../../control-plane/internal/shotcraft/catalog.json' with {type:'json'};
import {fpsValue,frameLabel,parseTimecode,type FrameRate,type TimelineClip} from './timelineContracts.ts';

export type ScriptContext='paragraph'|'code';
export interface DirectiveParameter {name:string;type:string;unit?:string;required?:boolean;min?:number;max?:number}
export interface DirectiveDefinition {name:string;label:string;contexts:string[];mode:string;parameters:DirectiveParameter[]}
export const SCRIPT_DIRECTIVES=definition.directives as DirectiveDefinition[];
export function directiveSpec(name:string){return SCRIPT_DIRECTIVES.find(x=>x.name===name);}

export interface ShotCompletionCard {
 id:string;
 summary:string;
 styles:readonly {id:string;description:string}[];
}
export function shotCardCompletions(query:string,custom:readonly ShotCompletionCard[]=[]){
 const search=query.toLowerCase();
 const matches=[...custom,...shotcraft].flatMap(card=>{
  const options=[{id:card.id,summary:card.summary},...card.styles.filter(style=>card.styles.length>1).map(style=>({id:card.id+'/'+style.id,summary:style.description}))];
  return options.filter(option=>option.id.includes(search)||option.summary.toLowerCase().includes(search));
 });
 return matches.slice(0,12);
}

export function directiveFields(body:string){
 const fields:{text:string;start:number;end:number}[]=[];let start=0,quoted=false,escaped=false;
 for(let i=0;i<body.length;i++){const c=body[i];if(quoted){if(escaped)escaped=false;else if(c==='\\')escaped=true;else if(c==='"')quoted=false;}else if(c==='"')quoted=true;else if(c===','||c==='，'){fields.push({text:body.slice(start,i),start,end:i});start=i+1;}}
 fields.push({text:body.slice(start),start,end:body.length});if(fields.length>1&&!fields.at(-1)!.text.trim())fields.pop();return fields;
}
export function readDirective(body:string,context:ScriptContext='code'){
 const fields=directiveFields(body),head=fields[0].text.trim(),args:Record<string,string>={},legacy=head.startsWith('"');
 if(body.trim().startsWith('@')){const reference=body.trim().slice(1).trim();args['引用']=reference;return {name:'@',args,error:reference&&new TextEncoder().encode(reference).length<=512&&!/[\r\n]/.test(reference)?'':'引用名称不能为空或跨行'};}
 if(body.trim().startsWith('#')){
  const reference=body.trim().slice(1).trim();
  const [id,style,...extra]=reference.split('/');
  const valid=/^user-[a-z0-9]+(?:-[a-z0-9]+)*(?:\/default)?$/.test(reference)&&id.length<=64||!extra.length&&shotcraft.some(card=>card.id===id&&(style===undefined||card.styles.some(variant=>variant.id===style)));
  return {name:'#',args:{镜头卡:reference},error:valid?'':`未知镜头卡或变体 ${reference}`};
 }
 const name=legacy?'字幕':head,spec=directiveSpec(name);
 if(!spec)return {name,args,error:`未知指令${name}`};
 if(!spec.contexts.includes(context))return {name,args,error:`${name}仅适用于代码条工作台`};
 if(legacy)args['文本']=head;
 for(const field of fields.slice(1)){const colon=field.text.indexOf(':'),key=field.text.slice(0,colon).trim(),value=field.text.slice(colon+1).trim();if(colon<0||!key||!value)return {name,args,error:'参数需要名称:值'};if(key in args)return {name,args,error:`参数${key}重复`};args[key]=value;}
 for(const key of Object.keys(args))if(!spec.parameters.some(p=>p.name===key))return {name,args,error:`${name}不支持参数${key}`};
 for(const p of spec.parameters){const value=args[p.name];if(value===undefined){if(p.required)return {name,args,error:`缺少参数${p.name}`};continue;}
  if(p.type==='string'){try{const text=JSON.parse(value);if(typeof text!=='string'||!text.trim()||(p.name==='文本'?[...text].length>2000:[...text].length>100||/[\r\n\0]/.test(text)))throw Error();}catch{return {name,args,error:`${p.name}需要双引号包围的有效文字`};}}
  if(p.type==='number'){const raw=p.unit?value.slice(0,-p.unit.length):value,number=Number(raw);if(p.unit&&!value.endsWith(p.unit)||!/^[-+]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][-+]?\d+)?$/.test(raw)||!Number.isFinite(number)||p.min!==undefined&&number<p.min||p.max!==undefined&&number>p.max)return {name,args,error:`${name}.${p.name}需要${p.min??''}～${p.max??''}${p.unit??''}`};}
 }
 return {name,args,error:''};
}
export function durationFrames(text:string,rate:FrameRate){
 const match=/^(\d+(?:\.\d+)?)(ms|s|f)$/.exec(text);if(!match)return null;const value=Number(match[1]);if(!Number.isFinite(value)||value>1e7||match[2]==='f'&&!Number.isInteger(value))return null;
 return match[2]==='f'?value:Math.round(value*(match[2]==='ms'?.001:1)*fpsValue(rate));
}
export function scriptTimeFrame(text:string|undefined,clip:Pick<TimelineClip,'at'>,rate:FrameRate){
 if(!text)return 0;const local=text.startsWith('条内'),value=text.replace(/^(条内|序列)/,''),frames=value.includes(':')?parseTimecode(value,rate):durationFrames(value,rate);
 return frames===null?null:frames-(local?0:clip.at);
}
export function rewriteDirectiveTime(token:string,delta:number,rate:FrameRate){
 if(delta===0)return token;
 const body=token.slice(1,-1),field=directiveFields(body).find(f=>f.text.slice(0,f.text.indexOf(':')).trim()==='时间');if(!field)return token;
 const colon=field.text.indexOf(':'),raw=field.text.slice(colon+1),value=raw.trim();if(value.startsWith('条内'))return token;
 const frame=scriptTimeFrame(value,{at:0},rate);if(frame===null)return token;
 const prefix=value.startsWith('序列')?'序列':'',start=1+field.start+colon+1+raw.indexOf(value);
 return token.slice(0,start)+prefix+frameLabel(frame+delta,rate)+token.slice(start+value.length);
}
export const parameterExample=(name:string)=>({'文本':'"字幕文字"','时间':'条内0f','持续':'1s','字号':'24px','字体':'"Microsoft YaHei"','比例':'100%','从':'0%','到':'100%','增益':'0dB','起始增益':'0dB','角度':'0','x':'0px','y':'0px'}[name]??'');
export function directiveTemplate(name:string){const spec=directiveSpec(name);if(!spec)return `{${name}}`;const args=spec.parameters.filter(p=>p.required||p.name==='时间').map(p=>`${p.name}:${parameterExample(p.name)}`);return `{${name}${args.length?', '+args.join(', '):''}}`;}
export function activeDirective(source:string,caret:number){
 let start=-1,quoted=false,escaped=false;
 for(let i=0;i<caret;i++){const c=source[i];if(quoted){if(escaped)escaped=false;else if(c==='\\')escaped=true;else if(c==='"')quoted=false;continue;}if(c==='\\'&&'{}\\'.includes(source[i+1]??'')){i++;continue;}if(c==='"'&&start>=0)quoted=true;else if(c==='{')start=i;else if(c==='}')start=-1;}
 return {start,quoted};
}
export function scriptBlockEnd(source:string,start:number){let quoted=false,escaped=false;for(let i=start+1;i<source.length;i++){const c=source[i];if(quoted){if(escaped)escaped=false;else if(c==='\\')escaped=true;else if(c==='"')quoted=false;}else if(c==='"')quoted=true;else if(c==='}')return i;}return -1;}
export function directiveCompletions(source:string,caret:number,context:ScriptContext='code',custom:readonly ShotCompletionCard[]=[]){
 // Only the current unfinished directive is completed. Strings are scanned
 // forward so a brace in subtitle text never changes the active directive.
 const {start,quoted}=activeDirective(source,caret);
 if(start<0||quoted)return [];
 const prefix=source.slice(start+1,caret),fields=directiveFields(prefix),end=scriptBlockEnd(source,start),stop=end<0?caret:end+1;
 if(prefix.startsWith('#'))return shotCardCompletions(prefix.slice(1).trim(),custom).map(card=>({label:card.id+' · '+card.summary,start,end:stop,text:`{#${card.id}}`}));
 if(fields.length===1){const query=fields[0].text.trim();return SCRIPT_DIRECTIVES.filter(x=>x.name!=='@'&&x.name!=='#'&&x.contexts.includes(context)&&x.name.startsWith(query)).map(x=>({label:x.name+' · '+x.label,start,end:stop,text:directiveTemplate(x.name)}));}
 const name=fields[0].text.trim(),spec=directiveSpec(name);if(!spec)return [];
 const last=fields.at(-1)!,query=last.text.trim(),used=new Set(fields.slice(1,-1).map(f=>f.text.split(':')[0].trim()));
 if(query.includes(':'))return [];
 return spec.parameters.filter(p=>!used.has(p.name)&&p.name.startsWith(query)).map(p=>({label:p.name+(p.unit?` (${p.unit})`:''),start:start+1+last.start,end:caret,text:' '+p.name+':'+parameterExample(p.name)}));
}
export interface ScriptIssue {start:number;end:number;line:number;column:number;message:string;frame?:number}
export function inspectCodeScript(source:string,clip:Pick<TimelineClip,'at'|'duration'>,rate:FrameRate){
 const issues:ScriptIssue[]=[],instructions:{start:number;end:number;label:string;frame:number;mode:string}[]=[];let quoted=false,escaped=false,start=-1;
 const report=(position:number,end:number,message:string)=>{const before=source.slice(0,position);issues.push({start:position,end,line:before.split('\n').length,column:[...before.slice(before.lastIndexOf('\n')+1)].length+1,message});};
 const ranges=new Map<string,{start:number;end:number}[]>();
 for(let i=0;i<source.length;i++){const c=source[i];if(start<0){if(c==='\\'&&'{}\\'.includes(source[i+1]??'')){i++;continue;}if(c==='{')start=i;else if(c==='}')report(i,i+1,'多余右花括号');continue;}
  if(quoted){if(escaped)escaped=false;else if(c==='\\')escaped=true;else if(c==='"')quoted=false;continue;}if(c==='"'){quoted=true;continue;}if(c==='{'){report(start,i+1,'语法块不能嵌套');continue;}if(c!=='}')continue;
  const block=readDirective(source.slice(start+1,i),'code');if(block.error)report(start,i+1,block.error);else{
   const frame=scriptTimeFrame(block.args['时间'],clip,rate),duration=block.args['持续']===undefined?0:durationFrames(block.args['持续'],rate),timed=!!directiveSpec(block.name)?.parameters.some(p=>p.type==='time');
   if(timed&&(frame===null||duration===null||frame<0||frame>=clip.duration||duration>clip.duration-frame||block.name==='字幕'&&block.args['持续']!==undefined&&duration<1))report(start,i+1,'时间或持续超出当前代码条');
   else if(timed&&block.name!=='字幕'&&(ranges.get(block.name)??[]).some(r=>frame!<r.end&&Math.max(frame!+1,frame!+duration!)>r.start))report(start,i+1,`${block.name}指令重复或动画范围重叠`);
   else if(block.name==='#'&&instructions.some(item=>item.label.startsWith('#')))report(start,i+1,'每个代码条只能引用一张镜头卡');
   else{if(timed&&block.name!=='字幕'){const list=ranges.get(block.name)??[];list.push({start:frame!,end:Math.max(frame!+1,frame!+duration!)});ranges.set(block.name,list);}instructions.push({start,end:i+1,label:block.name==='#'?'#'+block.args['镜头卡']:block.name,frame:frame??0,mode:directiveSpec(block.name)?.mode??'direct'});}
  }start=-1;
 }
 if(start>=0)report(start,source.length,'语法块缺少右花括号或引号未闭合');
 return {issues,instructions};
}
