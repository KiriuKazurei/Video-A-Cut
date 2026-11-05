// Shared by the still graph (CPU/GPU) and movie graph. All times are clip-local frames.
export function parameterValue(effects,kind,field,frame,initial=0){
 let value=initial;
 for(const key of effects.parameter_keyframes??[]){
  if(key.kind!==kind)continue;if(frame<key.frame)break;
  const target=key[field]??0;
  if(key.start_value!==undefined)value=key.start_value;
  if(key.duration>0&&frame<key.frame+key.duration)return value+(target-value)*(frame-key.frame)/key.duration;
  value=target;
 }
 return value;
}
export function parameterExpression(effects,kind,field,clock,initial=0){
 let value=initial,expression=String(initial);
 for(const key of effects.parameter_keyframes??[]){
  if(key.kind!==kind)continue;
  const target=key[field]??0;
  const start=key.start_value??value;
  const next=key.duration>0?`if(lt(${clock},${key.frame+key.duration}),${start}+(${target}-${start})*(${clock}-${key.frame})/${key.duration},${target})`:String(target);
  expression=`if(lt(${clock},${key.frame}),${expression},${next})`;value=target;
 }
 return expression;
}
