// Persistent WebGL compositor. Source engines share its owned browser, but keep
// independent pages/clocks. No project code executes in the desktop WebView.
import puppeteer from 'puppeteer-core';
import {createServer} from 'node:http';
import {drawBrowser} from './draw-runtime.mjs';
import {guardPage,listenDrawServer} from './draw-network.mjs';

export async function openSequenceGPU({browser_path,backend='auto'}={}) {
 const browser=await puppeteer.launch({executablePath:browser_path??drawBrowser('unified'),headless:true,args:['--enable-gpu','--force-color-profile=srgb','--disable-background-timer-throttling','--disable-renderer-backgrounding']});
 const server=createServer((req,res)=>{res.setHeader('Content-Type','text/html');res.end('<!doctype html><html><body style="margin:0"><canvas id="output"></canvas></body></html>');});
 let page;
 try{
  await listenDrawServer(server);const origin='http://127.0.0.1:'+server.address().port;
  page=await guardPage(await browser.newPage(),{origin});await page.goto(origin);
  const capabilities=await page.evaluate(disabled=>{
   const canvas=document.getElementById('output');
   const gl=disabled?null:canvas.getContext('webgl2',{alpha:false,antialias:false,preserveDrawingBuffer:true,premultipliedAlpha:false});
   if(!gl)return {available:false,renderer:'',hardware_accelerated:false,reason:disabled?'CPU backend selected':'WebGL2 unavailable'};
   const debug=gl.getExtension('WEBGL_debug_renderer_info'),renderer=debug?gl.getParameter(debug.UNMASKED_RENDERER_WEBGL):gl.getParameter(gl.RENDERER);
   const vertex=`#version 300 es
    in vec2 position; in vec2 texcoord; out vec2 uv;
    void main(){uv=texcoord;gl_Position=vec4(position,0.,1.);}`;
   const fragment=`#version 300 es
    precision highp float; uniform sampler2D image; uniform float brightness; uniform float fadeColor; uniform float opacity; in vec2 uv; out vec4 color;
    void main(){vec4 c=texture(image,uv);color=vec4(mix(vec3(fadeColor),c.rgb,brightness),mix(fadeColor,c.a,brightness)*opacity);}`;
   const shader=(type,source)=>{const s=gl.createShader(type);gl.shaderSource(s,source);gl.compileShader(s);if(!gl.getShaderParameter(s,gl.COMPILE_STATUS))throw Error(gl.getShaderInfoLog(s));return s;};
   const program=gl.createProgram(),vs=shader(gl.VERTEX_SHADER,vertex),fs=shader(gl.FRAGMENT_SHADER,fragment);
   gl.attachShader(program,vs);gl.attachShader(program,fs);gl.linkProgram(program);gl.deleteShader(vs);gl.deleteShader(fs);
   if(!gl.getProgramParameter(program,gl.LINK_STATUS))throw Error(gl.getProgramInfoLog(program));
   const buffer=gl.createBuffer();gl.useProgram(program);gl.bindBuffer(gl.ARRAY_BUFFER,buffer);
   for(const [name,offset] of [['position',0],['texcoord',8]]){const index=gl.getAttribLocation(program,name);gl.enableVertexAttribArray(index);gl.vertexAttribPointer(index,2,gl.FLOAT,false,16,offset);}
   gl.uniform1i(gl.getUniformLocation(program,'image'),0);
   window.sequenceCompose=async({width,height,layers})=>{
    if(gl.isContextLost())throw Error('WebGL2 context lost');
    if(width>gl.getParameter(gl.MAX_TEXTURE_SIZE)||height>gl.getParameter(gl.MAX_TEXTURE_SIZE))throw Error('GPU texture size exceeded');
    canvas.width=width;canvas.height=height;gl.viewport(0,0,width,height);gl.clearColor(0,0,0,1);gl.clear(gl.COLOR_BUFFER_BIT);
    gl.enable(gl.BLEND);gl.blendFuncSeparate(gl.SRC_ALPHA,gl.ONE_MINUS_SRC_ALPHA,gl.ONE,gl.ONE_MINUS_SRC_ALPHA);
    gl.pixelStorei(gl.UNPACK_PREMULTIPLY_ALPHA_WEBGL,false);
    for(const layer of layers){
     const image=new Image();image.src='data:image/png;base64,'+layer.png;await image.decode();
     const [left,top,right,bottom]=layer.crop,cropWidth=image.width*(1-left-right),cropHeight=image.height*(1-top-bottom);
     const fit=Math.min(layer.width/cropWidth,layer.height/cropHeight),w=cropWidth*fit,h=cropHeight*fit;
     const x=(width-w)/2+layer.x,y=(height-h)/2+layer.y;
     const x0=x/width*2-1,x1=(x+w)/width*2-1,y0=1-y/height*2,y1=1-(y+h)/height*2;
     const texture=gl.createTexture();
     try{
      gl.activeTexture(gl.TEXTURE0);gl.bindTexture(gl.TEXTURE_2D,texture);
      gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MIN_FILTER,gl.LINEAR);gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_MAG_FILTER,gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_WRAP_S,gl.CLAMP_TO_EDGE);gl.texParameteri(gl.TEXTURE_2D,gl.TEXTURE_WRAP_T,gl.CLAMP_TO_EDGE);
      gl.texImage2D(gl.TEXTURE_2D,0,gl.RGBA,gl.RGBA,gl.UNSIGNED_BYTE,image);
      const angle=(layer.rotation||0)*Math.PI/180,cx=x+w/2,cy=y+h/2;
      const point=(px,py)=>{const dx=px-cx,dy=py-cy;return [(cx+dx*Math.cos(angle)-dy*Math.sin(angle))/width*2-1,1-(cy+dx*Math.sin(angle)+dy*Math.cos(angle))/height*2];};
      gl.bufferData(gl.ARRAY_BUFFER,new Float32Array([...point(x,y),left,top,...point(x,y+h),left,1-bottom,...point(x+w,y),1-right,top,...point(x+w,y+h),1-right,1-bottom]),gl.STREAM_DRAW);
      gl.uniform1f(gl.getUniformLocation(program,'brightness'),layer.brightness);gl.uniform1f(gl.getUniformLocation(program,'fadeColor'),layer.fade_color);gl.uniform1f(gl.getUniformLocation(program,'opacity'),layer.opacity);
      gl.drawArrays(gl.TRIANGLE_STRIP,0,4);
     }finally{gl.deleteTexture(texture);}
    }
    if(gl.getError()!==gl.NO_ERROR)throw Error('WebGL2 composition failed');
    return canvas.toDataURL('image/png').split(',')[1];
   };
   return {available:true,renderer,hardware_accelerated:!!debug&&!/swiftshader|llvmpipe|software|basic render|warp/i.test(renderer),reason:''};
  },backend==='cpu');
  return {browser,capabilities,render:async input=>Buffer.from(await page.evaluate(input=>window.sequenceCompose(input),input),'base64'),close:async()=>{try{await browser.close();}finally{await new Promise(done=>server.close(done));}}};
 }catch(error){await browser.close();server.close();throw error;}
}
