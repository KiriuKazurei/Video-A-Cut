// Browser-only code runs in a fresh profile. Only the renderer's own origin is reachable.
const policy="default-src 'self' data: blob:; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; frame-src 'self'; worker-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; sandbox allow-scripts allow-same-origin";
const local = url => ['127.0.0.1','localhost','[::1]'].includes(new URL(url).hostname);
export async function guardPage(page, {remotion=false,origin}={}) {
 const client=remotion?page._client():await page.createCDPSession();
 let allowed=origin;
 const goto=page.goto.bind(page);
 page.goto=async (...args)=>{
  const url=remotion?args[0].url:args[0];
  if(!allowed && /^https?:/.test(url)){if(!local(url))throw Error('Draw renderer requires an owned loopback origin');allowed=new URL(url).origin;}
  return goto(...args);
 };
 await client.send('Network.enable');
 await client.send('Network.setBypassServiceWorker',{bypass:true});
 client.on('Fetch.requestPaused',async event=>{
  const id=event.requestId, url=event.request.url;
  const send=(name,value)=>client.send(name,{requestId:id,...value}).catch(()=>{});
  try {
   if(!url.startsWith('data:')&&!url.startsWith('blob:')&&(!allowed || new URL(url).origin!==allowed))
    return await send('Fetch.failRequest',{errorReason:'BlockedByClient'});
   if(event.responseStatusCode===undefined)return await send('Fetch.continueRequest',{});
   if(event.responseStatusCode>=300&&event.responseStatusCode<400)return await send('Fetch.continueRequest',{});
   const result=await client.send('Fetch.getResponseBody',{requestId:id}), body=result.value??result;
   const headers=(event.responseHeaders??[]).filter(h=>!['content-security-policy','content-length','content-encoding'].includes(h.name.toLowerCase()));
   headers.push({name:'Content-Security-Policy',value:policy},{name:'Cache-Control',value:'no-store'});
   await send('Fetch.fulfillRequest',{responseCode:event.responseStatusCode,responseHeaders:headers,body:body.base64Encoded?body.body:Buffer.from(body.body).toString('base64')});
  } catch {await send('Fetch.failRequest',{errorReason:'BlockedByClient'});}
 });
 await client.send('Fetch.enable',{patterns:[{urlPattern:'*',requestStage:'Request'},{urlPattern:'*',resourceType:'Document',requestStage:'Response'}]});
 return page;
}
export function guardRemotion(browser,origin) {
 const create=browser.newPage.bind(browser);
 browser.newPage=async options=>guardPage(await create(options),{remotion:true,origin});
 return browser;
}
if(process.env.VAC_DRAW_NETWORK_POLICY==='1'){
 const {default:puppeteer}=await import('puppeteer-core');
 const launch=puppeteer.launch.bind(puppeteer);
 puppeteer.launch=async options=>{
  const browser=await launch(options),create=browser.newPage.bind(browser);
  browser.newPage=async (...args)=>guardPage(await create(...args));
  const context=browser.createBrowserContext.bind(browser);
  browser.createBrowserContext=async (...args)=>{
   const value=await context(...args),newPage=value.newPage.bind(value);
   value.newPage=async (...params)=>guardPage(await newPage(...params));
   return value;
  };
  return browser;
 };
}
