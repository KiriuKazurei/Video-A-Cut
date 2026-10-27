import {mkdir,copyFile,lstat} from 'node:fs/promises';
import {join,extname} from 'node:path';
import {runTool} from './media.mjs';

export function imageLayers(c){
 const input=c.images??[];
 if(!Array.isArray(input)||input.length>64)throw Error('At most 64 image instances are supported');
 const seen=new Set(),resources=new Set();
 return input.map(layer=>{
  if(!layer||typeof layer.id!=='string'||!layer.id||layer.id.length>128||/[\/\\\0]/.test(layer.id)||['.','..'].includes(layer.id)||seen.has(layer.id)||typeof layer.resource_id!=='string'||!layer.resource_id||!/^[0-9a-f]{64}$/i.test(layer.resource_sha256??''))throw Error('Invalid image layer identity or resource fingerprint');
  seen.add(layer.id);resources.add(layer.resource_id);
  if(resources.size>32)throw Error('At most 32 original images are supported');
  for(const value of [layer.x,layer.y,layer.width,layer.height,layer.dx,layer.dy])if(!Number.isFinite(value)||Math.abs(value)>100000)throw Error('Invalid image position or displacement');
  if(layer.width<=0||layer.height<=0||layer.width>32768||layer.height>32768||![layer.from_frame,layer.to_frame].every(Number.isInteger)||layer.from_frame<0||layer.to_frame<layer.from_frame||layer.to_frame>18000)throw Error('Invalid image dimensions or animation frames');
  const fit=layer.fit||'contain',mode=layer.mode||'layer',opacity=layer.opacity??1;
  if(!['contain','cover','fill'].includes(fit)||!['layer','source'].includes(mode)||!Number.isFinite(opacity)||opacity<0||opacity>1)throw Error('Invalid image fit, mode or opacity');
  return {...layer,fit,mode,opacity,src:'vac-assets/'+layer.resource_sha256+'.png'};
 });
}
export function isImageReference(value,c){
 if(!value.startsWith('vac-image:'))return false;
 try{return imageLayers(c).some(layer=>layer.id===decodeURIComponent(value.slice(10)));}catch{return false;}
}
export async function checkedImageAssets(input,digest){
 const layers=imageLayers(input.composition),supplied=input.assets??[];
 if(!Array.isArray(supplied)||supplied.length>32)throw Error('Invalid owned image asset manifest');
 const result=new Map();
 for(const layer of layers){
  if(result.has(layer.resource_id))continue;
  const asset=supplied.find(item=>item.resource_id===layer.resource_id);
  try{
   if(!asset||(asset.binding_sha256??asset.sha256)!==layer.resource_sha256||typeof asset.path!=='string'||!(await lstat(asset.path)).isFile()||await digest(asset.path)!==asset.sha256)throw Error('image fingerprint changed');
  }catch{throw Error('Imported image '+layer.resource_id+' is missing or changed; relink and save the composition');}
  result.set(layer.resource_id,asset);
 }
 return [...result.values()];
}
export async function prepareImages(assets,stage,digest){
 const directory=join(stage,'image-cache');await mkdir(directory,{recursive:true});
 const prepared=[];
 for(const asset of assets){
  const filename=(asset.binding_sha256??asset.sha256)+'.png',target=join(directory,filename);
  try{
   // image2 otherwise treats a percent in an imported filename as a sequence.
   // Pin static images to one literal local file; keep the protocol restrictions.
   const literal=['.jpg','.jpeg','.jepg','.png','.webp','.bmp','.tif','.tiff'].includes(extname(asset.path).toLowerCase());
   await runTool('ffmpeg',['-v','error','-nostdin','-y','-format_whitelist','image2,png_pipe,apng,jpeg_pipe,webp_pipe,bmp_pipe,tiff_pipe,gif,ico,mov',...(literal?['-f','image2','-pattern_type','none']:[]),'-i',asset.path,'-frames:v','1','-vf',"scale=w='min(8192,iw)':h='min(8192,ih)':force_original_aspect_ratio=decrease",'-pix_fmt','rgba','-threads','1',target],{timeoutMs:60000});
   if(await digest(asset.path)!==asset.sha256)throw Error('source changed');
  }catch{throw Error('Cannot prepare imported image '+asset.resource_id+'; check its format and source fingerprint');}
  prepared.push({resource_id:asset.resource_id,sha256:asset.sha256,prepared_sha256:await digest(target),filename,path:target});
 }
 return prepared;
}
export async function installImages(prepared,directory){
 await mkdir(join(directory,'vac-assets'),{recursive:true});
 for(const asset of prepared)await copyFile(asset.path,join(directory,'vac-assets',asset.filename));
}
const safeJSON=value=>JSON.stringify(value).replaceAll('<','\\u003c');
const attr=value=>String(value).replaceAll('&','&amp;').replaceAll('"','&quot;').replaceAll('<','&lt;').replaceAll('>','&gt;');
export function hyperframesImageSource(c,gsap){
 const layers=imageLayers(c);
 let source=c.source;
 const guard='window.vacImages=new Proxy(window.vacImages,{get(t,k,r){if(typeof k!=="string"||k==="then"||k==="toJSON"||k in t)return Reflect.get(t,k,r);window.__vacImageError="Image alias "+String(k)+" is not bound to an imported resource";throw Error(window.__vacImageError)}});';
 if(!layers.length)return source.replace(/<head([^>]*)>/i,m=>m+'<script>'+gsap+'</script><script>window.vacImages={};'+guard+'</script>');
 const byID=Object.fromEntries(layers.map(layer=>[layer.id,layer]));
 source=source.replace(/(\b(?:src|href|poster)\s*=\s*["'])vac-image:([^"']+)(["'])/gi,(all,start,id,end)=>{
  const layer=byID[decodeURIComponent(id)];if(!layer)throw Error('Unknown bound image reference');return start+layer.src+end;
 });
 const bootstrap=String.raw`
(function(){
 const settings=__VAC_META__,root=document.querySelector("[data-composition-id]");
 const preloads=[...document.querySelectorAll("[data-vac-preload]")];
 const rootID=root.getAttribute("data-composition-id");
 window.__timelines=window.__timelines||{};
 const timeline=window.__timelines[rootID]||gsap.timeline({paused:true});
 window.__timelines[rootID]=timeline;
 if(getComputedStyle(root).position==="static")root.style.position="relative";
 Object.values(window.vacImages).forEach((image,index)=>{
  const preload=preloads.find(node=>node.getAttribute("data-vac-preload")===image.id);
  if(preload)image.src=preload.currentSrc||preload.src;
  if(image.mode==="source")return;
  const element=document.createElement("img");
  element.src=image.src;element.alt="";element.setAttribute("data-vac-image-id",image.id);
  Object.assign(element.style,{position:"absolute",left:image.x+"px",top:image.y+"px",width:image.width+"px",height:image.height+"px",objectFit:image.fit,opacity:String(image.opacity),pointerEvents:"none",zIndex:String(100+index)});
  root.appendChild(element);image.element=element;
  if(image.to_frame>image.from_frame&&(image.dx||image.dy))
   timeline.fromTo(element,{x:0,y:0},{x:image.dx,y:image.dy,duration:(image.to_frame-image.from_frame)/settings.fps,ease:"none",immediateRender:true},image.from_frame/settings.fps);
 });
 if(timeline.duration()===0)timeline.to({},{duration:settings.duration});
})();
`.replace('__VAC_META__',safeJSON({fps:c.fps.num/c.fps.den,duration:c.duration_frames*c.fps.den/c.fps.num}));
 const preloads=layers.map(layer=>'<img hidden alt="" data-vac-preload="'+attr(layer.id)+'" src="'+attr(layer.src)+'">').join('');
 return source.replace(/<head([^>]*)>/i,m=>m+'<script>'+gsap+'</script><script>window.vacImages='+safeJSON(byID)+';'+guard+'</script>').replace(/<body([^>]*)>/i,m=>m+preloads).replace(/<\/body\s*>/i,'<script>'+bootstrap+'</script></body>');
}
export function remotionImageEntry(c){
 const layers=imageLayers(c),images=Object.fromEntries(layers.map(layer=>[layer.id,{...layer,src:'/'+layer.src}]));
 return String.raw`import React from "react";
import {AbsoluteFill,Composition,Img,registerRoot,useCurrentFrame} from "remotion";
import Scene from "./Composition";
const images=new Proxy(__VAC_IMAGES__,{get(t,k,r){if(typeof k!=="string"||k==="then"||k==="toJSON"||k in t)return Reflect.get(t,k,r);throw Error("Image alias "+String(k)+" is not bound to an imported resource")}}),metadata=__VAC_META__;
function ImageLayers(){
 const frame=useCurrentFrame();
 return React.createElement(AbsoluteFill,{style:{pointerEvents:"none"}},Object.values(images).filter(image=>image.mode!=="source").map((image,index)=>{
  const progress=image.to_frame>image.from_frame?Math.max(0,Math.min(1,(frame-image.from_frame)/(image.to_frame-image.from_frame))):0;
  return React.createElement(Img,{key:image.id,src:image.src,style:{position:"absolute",left:image.x+image.dx*progress,top:image.y+image.dy*progress,width:image.width,height:image.height,objectFit:image.fit,opacity:image.opacity,zIndex:100+index}});
 }));
}
function Composed(){return React.createElement(AbsoluteFill,null,React.createElement(Scene,{images}),React.createElement(ImageLayers));}
registerRoot(()=>React.createElement(Composition,{id:"VAC",component:Composed,...metadata}));
`.replace('__VAC_IMAGES__',safeJSON(images)).replace('__VAC_META__',safeJSON({width:c.width,height:c.height,fps:c.fps.num/c.fps.den,durationInFrames:c.duration_frames}));
}
