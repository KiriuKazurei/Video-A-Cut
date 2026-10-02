param(
 [string]$ProfileFile='',
 [ValidateRange(1024,65535)][int]$Port=8806,
 [ValidateRange(0,86400)][int]$RunSeconds=0,
 [string]$IngestRootsFile='',
 [ValidateRange(1,300)][int]$ProviderTimeoutSeconds=30
)
$ErrorActionPreference='Stop'

function Get-WorkerRestartDelay {
 param([datetime[]]$Recent)
 $window=@($Recent | Where-Object { $_ -gt (Get-Date).AddSeconds(-60) })
 if($window.Count -ge 3){ return $null }
 return @(1,2,4)[$window.Count]
}

function Test-PermanentWorkerExit {
 param([int]$ExitCode,[string]$LogText)
 if($ExitCode -eq 3){ return $true }
 if($LogText -match '(?i)(^|\n).*(\bfatal: config:|mcp http 401|mcp http 403|protocol incompatible|schema incompatible)'){ return $true }
 return $false
}

if($env:VAC_SUPERVISOR_LIB_ONLY -eq '1'){ return }

$taskRoot=Split-Path -Parent $PSScriptRoot
if(!$ProfileFile){$ProfileFile=Join-Path $taskRoot 'configs/phase6/profile.builtin.example.json'}
$taskProfile=Get-Content -LiteralPath $ProfileFile -Raw | ConvertFrom-Json
if($taskProfile.revision -ne 1){throw 'A fresh local deployment needs profile revision 1.'}
if(Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue){throw "Port $Port already occupied."}
foreach($taskCommand in 'go','python','node','ffmpeg','ffprobe'){if(!(Get-Command $taskCommand -ErrorAction SilentlyContinue)){throw "Missing local tool: $taskCommand"}}
if(!(Test-Path -LiteralPath (Join-Path $taskRoot 'webui/dist/index.html'))){throw 'Build WebUI with npm --prefix webui run build first.'}
$taskRoles=@('recognizer','narrator','exporter');$taskIngestRoots=$null
if($IngestRootsFile){
 $taskIngestRoots=@(Get-Content -LiteralPath $IngestRootsFile -Raw | ConvertFrom-Json)
 foreach($taskEntry in $taskIngestRoots){if(!$taskEntry.root_id -or !$taskEntry.name -or ![IO.Path]::IsPathRooted([string]$taskEntry.path) -or !(Test-Path -LiteralPath $taskEntry.path -PathType Container)){throw "Ingest root entries need root_id, name and an existing absolute directory path."}}
 $taskRoles+='ingester'
}
$taskRun=Join-Path $taskRoot ('.run-data/local-pipeline/'+[Guid]::NewGuid().ToString('N'))
$taskDelivery=Join-Path $taskRun 'delivery'
New-Item -ItemType Directory -Path $taskDelivery -Force | Out-Null
$taskOwned=@();$taskPrevious=@{}
$taskAgents=@();$taskTokens=@{}
$taskWorkers=@();$taskEnteredRun=$false
function Write-TaskJson($path,$value){$value | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $path -Encoding utf8NoBOM}
function Stop-RecordedProcess($entry){
 if(!$entry.process -or $entry.process.HasExited){ return }
 $taskCurrent=Get-Process -Id $entry.process.Id -ErrorAction SilentlyContinue
 if($taskCurrent -and $taskCurrent.Path -eq $entry.path -and $taskCurrent.StartTime -eq $entry.process.StartTime){ Stop-Process -Id $taskCurrent.Id -Force; $entry.process.WaitForExit(5000) | Out-Null }
}
function Stop-OwnedMediaTrees {
 # The ingester's kernel-owned jobs end descendants when its handle closes.
 # Never signal pid-only records read from disk.
 foreach($taskSlot in $taskWorkers){ if($taskSlot.role -eq 'ingester'){ Stop-RecordedProcess $taskSlot } }
}
function Write-OwnedSnapshot {
 $taskEntries=@(@{process=$taskServer;path=$taskBinary;role='control';generation=1})+@($taskWorkers)
 $taskEntries | ForEach-Object {@{pid=$_.process.Id;path=$_.path;role=$_.role;generation=$_.generation;started_at=$_.process.StartTime.ToUniversalTime().ToString('o')}} |
  ConvertTo-Json | Set-Content -LiteralPath (Join-Path $taskRun 'owned-processes.json') -Encoding utf8NoBOM
}
try {
 foreach($taskRole in $taskRoles){
  $taskEnvName='VAC_LOCAL_'+$taskRole.ToUpper()+'_TOKEN';$taskPrevious[$taskEnvName]=[Environment]::GetEnvironmentVariable($taskEnvName,'Process')
  $taskToken=[Guid]::NewGuid().ToString('N')+[Guid]::NewGuid().ToString('N');[Environment]::SetEnvironmentVariable($taskEnvName,$taskToken,'Process');$taskTokens[$taskRole]=$taskEnvName
  $taskHash=[Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($taskToken))).ToLower()
  $taskAgents+=@{agent_id=$taskRole;role=$taskRole;token_sha256=$taskHash}
 }
 $taskPrevious['VAC_SESSION_STOP']=[Environment]::GetEnvironmentVariable('VAC_SESSION_STOP','Process')
 $env:VAC_SESSION_STOP=Join-Path $taskRun 'session.stop'
 Write-TaskJson (Join-Path $taskRun 'agents.json') @{agents=$taskAgents}
 $taskControl=@{http_addr="127.0.0.1:$Port";delivery_root=$taskDelivery;web_root=(Join-Path $taskRoot 'webui/dist');mcp_agents_file=(Join-Path $taskRun 'agents.json');lease_seconds=60}
 if($taskIngestRoots){$taskControl.ingest_roots=@($taskIngestRoots | ForEach-Object {@{root_id=$_.root_id;name=$_.name;path=$_.path}})}
 Write-TaskJson (Join-Path $taskRun 'control.json') $taskControl
 $taskBinary=Join-Path $taskRun 'control-plane.exe';$taskOldCache=$env:GOCACHE
 try{$env:GOCACHE=Join-Path $taskRoot '.run-data/go-cache';Push-Location (Join-Path $taskRoot 'control-plane');try{& go build -o $taskBinary .;if($LASTEXITCODE){throw 'Go build failed'}}finally{Pop-Location}}finally{$env:GOCACHE=$taskOldCache}
 $taskServer=Start-Process -FilePath $taskBinary -ArgumentList @('-config',('"'+(Join-Path $taskRun 'control.json')+'"'),'-db',('"'+(Join-Path $taskRun 'control.db')+'"')) -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $taskRun 'control.out') -RedirectStandardError (Join-Path $taskRun 'control.err');$taskOwned+=@{process=$taskServer;path=$taskBinary;role='control'}
 $taskURL="http://127.0.0.1:$Port";$taskReady=$false
 for($taskAttempt=0;$taskAttempt -lt 100;$taskAttempt++){if($taskServer.HasExited){throw 'Control plane exited; see control.err'};try{Invoke-RestMethod "$taskURL/api/assets" | Out-Null;$taskReady=$true;break}catch{Start-Sleep -Milliseconds 100}}
 if(!$taskReady){throw 'Control-plane readiness timeout'}
 $taskSaved=Invoke-RestMethod -Method Post -Uri "$taskURL/api/processing-profiles" -ContentType 'application/json' -Body (@{profile=$taskProfile;expected_revision=0;idempotency_key='local-bootstrap'}|ConvertTo-Json -Depth 20)
 $taskResolved=Invoke-RestMethod "$taskURL/api/processing-profiles/$($taskSaved.profile_id)/revisions/1"
 foreach($taskRole in $taskRoles){
  $taskConfig=@{mcp_url="$taskURL/mcp";delivery_root=$taskDelivery;token_env=$taskTokens[$taskRole];profile_sha256=$taskResolved.profile_sha256;processing_profile=$taskSaved;role=$taskRole}
  if($taskRole -eq 'ingester'){
   $taskConfig=@{mcp_url="$taskURL/mcp";delivery_root=$taskDelivery;token_env=$taskTokens[$taskRole];role=$taskRole;ingest_roots=$taskControl.ingest_roots}
   $taskExecutable=(Get-Command python).Source;$taskArgs=@('-m','vac_worker','--config',('"'+(Join-Path $taskRun "$taskRole.json")+'"'));$taskWorking=Join-Path $taskRoot 'workers/python'
  }
  elseif($taskRole -eq 'exporter'){$taskConfig.cli_path=Join-Path $taskRoot 'workers/node/timeline-cli/src/cli.mjs';$taskConfig.adapters_path=Join-Path $taskRoot 'workers/node/timeline-cli/adapters/index.mjs';$taskExecutable=(Get-Command node).Source;$taskArgs=@(('"'+(Join-Path $taskRoot 'workers/node/export-worker/src/main.mjs')+'"'),'--config',('"'+(Join-Path $taskRun "$taskRole.json")+'"'));$taskWorking=$taskRoot}
  else{$taskProvider=if($taskRole -eq 'recognizer'){$taskSaved.vision}else{$taskSaved.narration};$taskConfig.content_provider=$taskProvider.adapter;$taskConfig.tts_auto_approve=$false;$taskConfig.provider_timeout_seconds=$ProviderTimeoutSeconds
   if($taskProvider.adapter -ne 'builtin'){$taskConfig.content_provider_config=@{endpoint=$taskProvider.endpoint;model=$taskProvider.model;token_env=$taskProvider.token_env;allow_external=$true};if($taskProvider.api_format){$taskConfig.content_provider_config.api_format=$taskProvider.api_format}}
   $taskExecutable=(Get-Command python).Source;$taskArgs=@('-m','vac_worker','--config',('"'+(Join-Path $taskRun "$taskRole.json")+'"'));$taskWorking=Join-Path $taskRoot 'workers/python'
  }
  Write-TaskJson (Join-Path $taskRun "$taskRole.json") $taskConfig
  $taskProcess=Start-Process -FilePath $taskExecutable -ArgumentList $taskArgs -WorkingDirectory $taskWorking -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $taskRun "$taskRole.out") -RedirectStandardError (Join-Path $taskRun "$taskRole.err")
  $taskSlot=@{role=$taskRole;process=$taskProcess;path=$taskExecutable;args=$taskArgs;working=$taskWorking;out=(Join-Path $taskRun "$taskRole.out");err=(Join-Path $taskRun "$taskRole.err");recent=@();blocked=$false;nextRestart=$null;generation=1}
  $taskWorkers+=$taskSlot
  $taskOwned+=@{process=$taskProcess;path=$taskExecutable;role=$taskRole}
 }
 Write-OwnedSnapshot
 Write-Host "Local pipeline: $taskURL/"
 Write-Host "Data and logs: $taskRun"
 Write-Host 'Import a controlled package into this delivery root, select the saved profile, and run preflight. Models are not downloaded or launched. Ctrl+C stops only this session.'
 if($taskIngestRoots){Write-Host "Raw recordings: register a file under a configured root in the WebUI, or run scripts/Import-Recording.ps1 -BaseUrl $taskURL -Path <file>."}
 $taskDeadline=if($RunSeconds -gt 0){[DateTime]::UtcNow.AddSeconds($RunSeconds)}else{[DateTime]::MaxValue}
 $taskEnteredRun=$true
 while([DateTime]::UtcNow -lt $taskDeadline){
  if($taskServer.HasExited){ Write-Host 'Control plane exited. Workers stay up and reconnect; this session is not torn down.' }
  foreach($taskSlot in $taskWorkers){
   if($taskSlot.blocked -or !$taskSlot.process.HasExited){ continue }
   $taskLog=''; foreach($taskLogFile in @($taskSlot.err,$taskSlot.out)){ if(Test-Path -LiteralPath $taskLogFile){ $taskLog+=((Get-Content -LiteralPath $taskLogFile -Tail 40 -ErrorAction SilentlyContinue) -join "`n")+"`n" } }
   $taskCode=0; try { $taskCode=[int]$taskSlot.process.ExitCode } catch { $taskCode=1 }
   if(Test-PermanentWorkerExit $taskCode $taskLog){ $taskSlot.blocked=$true; Write-Host "Worker $($taskSlot.role) stopped for a permanent configuration or auth error; not restarting."; continue }
   if($null -eq $taskSlot.nextRestart){
    $taskDelay=Get-WorkerRestartDelay $taskSlot.recent
    if($null -eq $taskDelay){ $taskSlot.blocked=$true; Write-Host "Worker $($taskSlot.role) exceeded 3 restarts in 60s; role blocked, other workers stay up."; continue }
    $taskSlot.nextRestart=(Get-Date).AddSeconds($taskDelay)
    continue
   }
   if((Get-Date) -lt $taskSlot.nextRestart){ continue }
   $taskSlot.recent+=Get-Date
   $taskSlot.generation++
   $taskSlot.process=Start-Process -FilePath $taskSlot.path -ArgumentList $taskSlot.args -WorkingDirectory $taskSlot.working -WindowStyle Hidden -PassThru -RedirectStandardOutput $taskSlot.out -RedirectStandardError $taskSlot.err
   $taskSlot.nextRestart=$null
   Write-OwnedSnapshot
   Write-Host "Restarted $($taskSlot.role) as instance generation $($taskSlot.generation)."
  }
  Start-Sleep -Milliseconds 500
 }
}finally{
 if($taskEnteredRun){
  New-Item -ItemType File -Path $env:VAC_SESSION_STOP -Force | Out-Null
  $taskDrain=[DateTime]::UtcNow.AddSeconds(15)
  while([DateTime]::UtcNow -lt $taskDrain){
   $taskLive=@($taskWorkers | Where-Object { $_.process -and -not $_.process.HasExited })
   if($taskLive.Count -eq 0){ break }
   Start-Sleep -Milliseconds 300
  }
  Stop-OwnedMediaTrees
  foreach($taskSlot in $taskWorkers){ Stop-RecordedProcess $taskSlot }
  Stop-RecordedProcess @{process=$taskServer;path=$taskBinary}
 } else {
  [array]::Reverse($taskOwned)
  foreach($taskEntry in $taskOwned){ Stop-RecordedProcess $taskEntry }
 }
 foreach($taskName in $taskPrevious.Keys){[Environment]::SetEnvironmentVariable($taskName,$taskPrevious[$taskName],'Process')}
 Write-Host 'Owned pipeline processes stopped. Data and logs retained.'
}
