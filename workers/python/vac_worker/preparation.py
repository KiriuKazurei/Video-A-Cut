"""Local, bounded capabilities and fixed processing-profile adaptation."""
import json
import os
import subprocess
from dataclasses import replace
from .sampling import SamplingLimits
from .stages import StageFailure

def configured_task(cfg, task_input):
    p = task_input.get('processing_profile')
    if not p:
        return cfg
    if not cfg.profile_sha256 or cfg.profile_sha256 != task_input.get('profile_sha256'):
        raise StageFailure('Worker configuration fingerprint does not match task profile')
    spec = p['vision'] if cfg.role == 'recognizer' else p['narration']
    provider_config = {} if spec['adapter'] == 'builtin' else {k:spec[k] for k in ('endpoint','model','token_env')}
    if spec.get('api_format'):
        provider_config['api_format'] = spec['api_format']
    if provider_config:
        # Control-plane input is returned only while the exact profile's
        # external authorization is valid; local endpoints require no grant.
        provider_config['allow_external'] = True
        # Transport deadline comes from the operator's worker configuration,
        # independently of the profile's frame-sampling deadline.
        provider_config['timeout_seconds'] = cfg.provider_timeout_seconds
    budget = p['sampling']
    return replace(cfg, content_provider=spec['adapter'], content_provider_config=provider_config,
                   sampling_limits=SamplingLimits(max_total_frames=budget['max_frames'],
                   max_total_bytes=budget['max_bytes'],max_total_time=budget['timeout_seconds']),
                   tts_voice=p['tts_voice'],tts_auto_approve=False)

def local_capability(cfg):
    if not cfg.profile_sha256:
        return None
    tools=True
    for binary in (cfg.ffmpeg,cfg.ffprobe):
        try:
            r=subprocess.run([binary,'-version'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL,timeout=3,
                             creationflags=getattr(subprocess,'CREATE_NO_WINDOW',0))
            tools = tools and r.returncode == 0
        except (OSError,subprocess.TimeoutExpired):
            tools=False
    voices=[]
    if cfg.role == 'narrator':
        try:
            script="$ErrorActionPreference='Stop';[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false);$s=New-Object -ComObject SAPI.SpVoice;@($s.GetVoices() | ForEach-Object {$_.GetAttribute('Name')}) | ConvertTo-Json -Compress"
            r=subprocess.run(['powershell','-NoProfile','-NonInteractive','-Command',script],capture_output=True,timeout=5,
                             creationflags=getattr(subprocess,'CREATE_NO_WINDOW',0))
            if r.returncode == 0:
                result=json.loads(r.stdout.decode('utf-8-sig'));voices=result if isinstance(result,list) else [result]
        except (OSError,ValueError,subprocess.TimeoutExpired):
            pass
    spec=(cfg.processing_profile or {}).get('vision' if cfg.role=='recognizer' else 'narration',{})
    credentials=bool(cfg.token) and (spec.get('adapter')=='builtin' or bool(os.environ.get(spec.get('token_env',''))))
    return {'profile_sha256':cfg.profile_sha256,'tools_ready':tools,'credentials_ready':credentials,'tts_voices':voices}
