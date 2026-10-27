// Shared browser scene: preview and export use the same React/HyperFrames pixels.
import React, {useLayoutEffect, useRef, useState} from 'react';
import {createRoot} from 'react-dom/client';
import {Player} from '@remotion/player';
import {Freeze} from 'remotion';
import * as Remotion from 'remotion';
import * as ReactJSX from 'react/jsx-runtime';
import {drawFrameContext} from './draw-time.mjs';

const pending = new Map();
const sources = {};
window.__vacReact=React;window.__vacRemotion=Remotion;window.__vacJSX=ReactJSX;
window.vacInstallSource=id=>{sources[id]=window.VacSourceBuild.default;};
window.vacRemoveSource=id=>{delete sources[id];};
let current;
const wait = (key) => {
  let resolve, reject;
  const promise = new Promise((a, b) => {resolve = a; reject = b;});
  pending.set(key, {resolve, reject});
  return promise;
};
function acknowledge(key, id, error) {
  if (!current || current.key !== key) return;
  if (error) {pending.get(key)?.reject(Error(error)); return;}
  current.ready.add(id);
  if (current.expected.every(id => current.ready.has(id))) pending.get(key)?.resolve();
}
function HtmlLayer({html, frame, requestKey, id, fps}) {
  const element = useRef(null);
  const [loaded, setLoaded] = useState(0);
  useLayoutEffect(() => {
    const listener = event => {
      if (event.source !== element.current?.contentWindow || event.data?.vac !== 'frame-ready') return;
      if (event.data.key === requestKey) acknowledge(requestKey, id, event.data.error);
    };
    window.addEventListener('message', listener);
    if (loaded) element.current.contentWindow.postMessage({vac:'seek', frame, fps, key:requestKey}, '*');
    return () => window.removeEventListener('message', listener);
  }, [frame, requestKey, loaded, id, fps]);
  return <iframe ref={element} sandbox="allow-scripts" srcDoc={html} onLoad={() => setLoaded(n=>n+1)}
    style={{border:0, width:'100%', height:'100%', display:'block', pointerEvents:'none'}}/>;
}
function ReactSource({component:Component,frame,requestKey,id,parameters}) {
  useLayoutEffect(()=>{acknowledge(requestKey,id);},[requestKey,id,frame]);
  const images=new Proxy(parameters.images??{}, {get(target,key,receiver){
    if(typeof key==='symbol'||key==='then'||key==='toJSON'||key in target)return Reflect.get(target,key,receiver);
    throw Error('Image alias '+String(key)+' is not bound to an imported resource');
  }});
  return <Freeze frame={frame}><Component {...parameters} images={images}/></Freeze>;
}
function Scene({document, frame, requestKey, assets, html}) {
  const fps = document.canvas.fps.num / document.canvas.fps.den;
  const times=new Map();
  useLayoutEffect(() => {acknowledge(requestKey, '$react');}, [requestKey]);
  const render = node => {
    const time=drawFrameContext(document,node.id,frame,times);
    if(!time.active)return null;
    let t = node.transform ?? {};
    let opacity=node.opacity??1;
    if(node.keyframes?.length){
      const at=time.parent_frame-node.from,keys=node.keyframes;
      const b=keys.find(k=>k.frame>=at)??keys.at(-1),a=[...keys].reverse().find(k=>k.frame<=at)??keys[0];
      const progress=b.frame===a.frame?0:(at-a.frame)/(b.frame-a.frame);
      t={...t,x:a.x+(b.x-a.x)*progress,y:a.y+(b.y-a.y)*progress};
      if(a.opacity!==undefined&&b.opacity!==undefined)opacity=a.opacity+(b.opacity-a.opacity)*progress;
    }
    const source = document.sources.find(s => s.id === node.source_id);
    const clock=time.fps,local=time.frame;
    const style = {position:'absolute', left:t.x ?? 0, top:t.y ?? 0, width:t.width || document.canvas.width,
      height:t.height || document.canvas.height, opacity, overflow:'hidden',
      transform:`rotate(${t.rotation ?? 0}deg) scale(${t.scale_x ?? 1},${t.scale_y ?? 1})`, transformOrigin:'center',
      clipPath:node.kind==='mask'&&node.shape==='ellipse'?'ellipse(50% 50% at 50% 50%)':node.crop ? `inset(${node.crop.top??0}px ${node.crop.right??0}px ${node.crop.bottom??0}px ${node.crop.left??0}px)` : undefined};
    let child;
    if (node.kind === 'image') child = <img src={assets[node.resource_id]} style={{width:'100%',height:'100%',objectFit:node.fit??'contain'}}/>;
    else if (node.kind === 'text') child = <div style={{color:node.color??'white',fontSize:node.font_size??48,whiteSpace:'pre-wrap',...node.style}}>{node.text}</div>;
    else if (node.kind === 'shape') child=<div style={{width:'100%',height:'100%',background:node.color??'white',borderRadius:node.shape==='ellipse'?'50%':0}}/>;
    else if (node.kind === 'group'||node.kind==='mask') child = document.nodes.filter(n=>n.parent_id===node.id).sort((a,b)=>(a.z??0)-(b.z??0)).map(render);
    else if (source?.engine === 'hyperframes') child = <HtmlLayer html={html[source.id]} frame={local} fps={clock.num/clock.den} id={node.id} requestKey={requestKey}/>;
    else if (source?.engine === 'remotion') {
      const bindings=new Map((source.image_bindings??[]).map(im=>[im.id,{...im,src:assets[im.resource_id]}]));
      for(const im of document.nodes.filter(n=>n.kind==='image')){
        const first=im.keyframes?.[0],last=im.keyframes?.at(-1);
        bindings.set(im.id,{...bindings.get(im.id),...im,...im.transform,
          dx:first&&last?last.x-first.x:0,dy:first&&last?last.y-first.y:0,
          from_frame:first?.frame??0,to_frame:last?.frame??0,mode:im.hidden?'source':'layer',src:assets[im.resource_id]});
      }
      const images=Object.fromEntries(bindings);
      child=<Player component={ReactSource} inputProps={{component:sources[source.id],frame:local,requestKey,id:node.id,parameters:{theme:document.theme??{},seed:document.seed??0,...document.variables,...node.parameters,images}}} compositionWidth={source.canvas?.width??document.canvas.width} compositionHeight={source.canvas?.height??document.canvas.height} fps={clock.num/clock.den} durationInFrames={source.duration_frames??document.duration_frames} controls={false} autoPlay={false} errorFallback={({error})=>{acknowledge(requestKey,node.id,error.message);return null;}} style={{width:source.canvas?.width??document.canvas.width,height:source.canvas?.height??document.canvas.height}}/>;
    }
    if(source){
      const width=source.canvas?.width??document.canvas.width,height=source.canvas?.height??document.canvas.height;
      const boxWidth=t.width||document.canvas.width,boxHeight=t.height||document.canvas.height;
      const fit=node.fit??'contain',ratio=fit==='cover'?Math.max(boxWidth/width,boxHeight/height):Math.min(boxWidth/width,boxHeight/height);
      const sx=fit==='fill'?boxWidth/width:ratio,sy=fit==='fill'?boxHeight/height:ratio;
      child=<div style={{position:'absolute',left:(boxWidth-width*sx)/2,top:(boxHeight-height*sy)/2,width,height,transform:`scale(${sx},${sy})`,transformOrigin:'top left'}}>{child}</div>;
    }
    return <div key={node.id} data-vac-node={node.id} style={style}>{child}</div>;
  };
  return <div style={{width:'100%',height:'100%',position:'relative',overflow:'hidden',background:document.canvas.background??'transparent'}}>
    {document.nodes.filter(n=>!n.parent_id).sort((a,b)=>(a.z??0)-(b.z??0)).map(render)}
  </div>;
}
const root=createRoot(document.getElementById('root'));
let resources={assets:{},html:{}};
const imageBlobs=new Map();
const imageData=new Map();
const dataURI=blob=>new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result);reader.onerror=()=>reject(reader.error);reader.readAsDataURL(blob);});
window.vacSeek=async snapshot=>{
  if(snapshot.resources){
    const assets={};
    for(const [id,url] of Object.entries(snapshot.resources.assets)){
      if(!imageBlobs.has(url)){const response=await fetch(url);if(!response.ok)throw Error('Drawing image failed to load');const blob=await response.blob();imageBlobs.set(url,URL.createObjectURL(blob));imageData.set(url,await dataURI(blob));}
      assets[id]=imageBlobs.get(url);
    }
    // Opaque sandboxed HTML cannot read loopback/blob resources in current
    // Chromium. Resolve its data URIs inside the browser, never over CDP.
    const html=Object.fromEntries(Object.entries(snapshot.resources.html).map(([id,source])=>{for(const url of Object.values(snapshot.resources.assets))source=source.replaceAll(url,imageData.get(url));return [id,source];}));
    resources={assets,html};
  }
  const {document:d,frame,key}=snapshot;
  const times=new Map(),active=n=>drawFrameContext(d,n.id,frame,times).active;
  const expected=d.nodes.filter(n=>active(n)&&n.kind==='source').map(n=>n.id);
  current={key,expected:['$react',...expected],ready:new Set()};
  const ready=wait(key);
  root.render(<Player component={Scene} inputProps={{...snapshot,...resources,requestKey:key}} compositionWidth={d.canvas.width}
    compositionHeight={d.canvas.height} fps={d.canvas.fps.num/d.canvas.fps.den} durationInFrames={d.duration_frames}
    style={{width:d.canvas.width,height:d.canvas.height}} controls={false} autoPlay={false} initiallyShowControls={false}/>);
  let timer;
  try {
    await Promise.race([ready,new Promise((_,reject)=>{timer=setTimeout(()=>reject(Error('Layers did not complete requested frame: '+expected.join(','))),15000);})]);
    await document.fonts.ready;
    await Promise.all([...document.images].map(image=>image.decode()));
    await new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)));
    return [...document.querySelectorAll('[data-vac-node]')].map(element=>{const b=element.getBoundingClientRect();return {id:element.dataset.vacNode,x:b.x,y:b.y,width:b.width,height:b.height};});
  } finally {clearTimeout(timer);pending.delete(key);}
};
window.vacReady=true;
