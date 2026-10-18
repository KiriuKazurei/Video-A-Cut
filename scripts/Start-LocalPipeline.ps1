#Requires -Version 7.2
param(
 [string]$ProfileFile='',
 [ValidateRange(1024,65535)][int]$Port=8806,
 [ValidateRange(0,86400)][int]$RunSeconds=0,
 [string]$IngestRootsFile='',
 [ValidateRange(1,300)][int]$ProviderTimeoutSeconds=30,
 [string]$DataDirectory='',
 [string]$ControlBinary=''
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
. (Join-Path $PSScriptRoot 'Runtime.Common.ps1')
if(!$ProfileFile){$ProfileFile=Join-Path $taskRoot 'configs/phase6/profile.builtin.example.json'}
$taskProfile=Get-Content -LiteralPath $ProfileFile -Raw | ConvertFrom-Json
if(!$taskProfile.profile_id -or $taskProfile.revision -lt 1){throw 'Select a profile with a positive saved revision.'}
if(Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue){throw "Port $Port already occupied."}
$taskCommands=@('python','node','ffmpeg','ffprobe');if(!$ControlBinary){$taskCommands+='go'}
foreach($taskCommand in $taskCommands){if(!(Get-Command $taskCommand -CommandType Application -ErrorAction SilentlyContinue)){throw "Missing local tool: $taskCommand"}}
if($ControlBinary){$ControlBinary=Assert-VacPlainPath $ControlBinary;if(!(Test-Path -LiteralPath $ControlBinary -PathType Leaf)){throw 'Prebuilt control-plane executable is missing.'}}
if(!(Test-Path -LiteralPath (Join-Path $taskRoot 'webui/dist/index.html'))){throw 'Build WebUI with npm --prefix webui run build first.'}
$taskRoles=@('recognizer','narrator','exporter');$taskIngestRoots=$null
if($IngestRootsFile){
 $taskIngestRoots=@(Get-Content -LiteralPath $IngestRootsFile -Raw | ConvertFrom-Json)
 foreach($taskEntry in $taskIngestRoots){if(!$taskEntry.root_id -or !$taskEntry.name -or ![IO.Path]::IsPathRooted([string]$taskEntry.path) -or !(Test-Path -LiteralPath $taskEntry.path -PathType Container)){throw "Ingest root entries need root_id, name and an existing absolute directory path."}}
 $taskRoles+='ingester'
}
$taskSessionID=[Guid]::NewGuid().ToString('N')
$taskPersistent=[bool]$DataDirectory
if(!$DataDirectory){$taskSessionRoot=if($env:VAC_TEST_ROOT){$env:VAC_TEST_ROOT}else{Join-Path $taskRoot '.run-data/local-pipeline'};$DataDirectory=Join-Path $taskSessionRoot $taskSessionID}
$DataDirectory=Assert-VacPlainPath $DataDirectory
New-Item -ItemType Directory -Path $DataDirectory -Force | Out-Null
$taskLock=$null
$taskRun=if($taskPersistent){Resolve-VacSessionPath $DataDirectory $taskSessionID}else{$DataDirectory}
$taskDelivery=Assert-VacPlainPath (Join-Path $DataDirectory 'delivery')
$taskDatabase=Assert-VacPlainPath (Join-Path $DataDirectory 'control.db')
$taskActivePath=Assert-VacPlainPath (Join-Path $DataDirectory 'active-session.json')
$taskStopPath=Assert-VacPlainPath (Join-Path $taskRun 'session.stop')
$taskOwned=@();$taskPrevious=@{}
$taskAgents=@();$taskTokens=@{}
$taskWorkers=@();$taskEnteredRun=$false;$taskActive=$null
function Write-TaskJson($path,$value){$value | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $path -Encoding utf8NoBOM}
function Stop-RecordedProcess($entry){
 if(!$entry.process -or $entry.process.HasExited){ return }
 $taskCurrent=Get-Process -Id $entry.process.Id -ErrorAction SilentlyContinue
 if($taskCurrent){
  if($taskCurrent.Path -ne $entry.identity_path -or $taskCurrent.StartTime.ToUniversalTime().ToString('o') -ne $entry.started_at){throw 'Process identity changed; automatic stop refused.'}
  Stop-Process -Id $taskCurrent.Id -Force
  if(!$entry.process.WaitForExit(5000)){throw 'Owned process did not exit within the stop deadline.'}
 }
}
function New-RecordedProcess($process,$role){
 # Start-Process can return before the executable path becomes readable.
 $taskIdentity=$null
 for($taskIdentityAttempt=0;$taskIdentityAttempt -lt 40;$taskIdentityAttempt++){
  $taskIdentity=Get-Process -Id $process.Id -ErrorAction SilentlyContinue
  if($taskIdentity -and $taskIdentity.Path){break}
  if($process.HasExited){throw 'Newly launched process exited before identity could be recorded.'}
  Start-Sleep -Milliseconds 50
 }
 if(!$taskIdentity -or !$taskIdentity.Path){throw 'Newly launched process identity could not be verified.'}
 return @{process=$process;identity_path=$taskIdentity.Path;started_at=$taskIdentity.StartTime.ToUniversalTime().ToString('o');role=$role}
}
function Write-OwnedSnapshot {
 Write-VacJson (Join-Path $taskRun 'owned-processes.json') @($taskOwned | ForEach-Object {@{pid=$_.process.Id;path=$_.identity_path;role=$_.role;started_at=$_.started_at}})
}
try {
 try{$taskLock=[IO.File]::Open((Assert-VacPlainPath (Join-Path $DataDirectory 'runtime.lock')),[IO.FileMode]::OpenOrCreate,[IO.FileAccess]::ReadWrite,[IO.FileShare]::None)}catch{throw 'Data directory is locked or not writable; stop its owning session first.'}
 if($taskPersistent -and (Test-Path -LiteralPath $taskActivePath)){
  $taskPrior=Get-Content -LiteralPath $taskActivePath -Raw | ConvertFrom-Json
  $taskPriorRun=Resolve-VacSessionPath $DataDirectory $taskPrior.session_id
  if($taskPrior.status -eq 'running' -and (Test-VacProcessIdentity $taskPrior.supervisor)){throw 'Previous supervisor is still alive; startup refused.'}
  $taskPriorOwned=Assert-VacPlainPath (Join-Path $taskPriorRun 'owned-processes.json')
  if(Test-Path -LiteralPath $taskPriorOwned){
   foreach($taskPriorProcess in @(Get-Content -LiteralPath $taskPriorOwned -Raw | ConvertFrom-Json)){
    if(Test-VacProcessIdentity $taskPriorProcess){throw 'Previous session has a live process; review its identities before manual recovery.'}
   }
  }
 }
 New-Item -ItemType Directory -Path $taskDelivery,$taskRun -Force | Out-Null
 $taskSupervisor=Get-Process -Id $PID
 $taskActive=@{session_id=$taskSessionID;status='running';supervisor=@{pid=$PID;path=$taskSupervisor.Path;started_at=$taskSupervisor.StartTime.ToUniversalTime().ToString('o')}}
 if($taskPersistent){Write-VacJson $taskActivePath $taskActive}
 if($taskPersistent){
  $taskSchemas=@()
  if($ControlBinary){
   $taskManifest=Get-Content -LiteralPath (Assert-VacPlainPath ($ControlBinary+'.runtime.json')) -Raw | ConvertFrom-Json
   if($taskManifest.binary_sha256 -ne (Get-FileHash -LiteralPath $ControlBinary -Algorithm SHA256).Hash.ToLowerInvariant()){throw 'Prebuilt executable does not match its runtime manifest.'}
   if($taskManifest.schema_version -ne 1){throw 'Unsupported runtime manifest format.'}
   $taskSchemas=@($taskManifest.schema_versions | ForEach-Object {[int]$_})
  }else{
   $taskSchemas=@(Get-ChildItem -LiteralPath (Join-Path $taskRoot 'control-plane/internal/store/migrations') -Filter '*.sql' | ForEach-Object {[int]($_.Name.Substring(0,4))})
  }
  if(!$taskSchemas.Count -or @($taskSchemas | Where-Object {$_ -lt 1}).Count){throw 'Runtime migration inventory is missing or invalid.'}
  $taskBackup=Assert-VacPlainPath (Join-Path $taskRun 'pre-start.db')
  & python (Join-Path $PSScriptRoot 'runtime_state.py') --database $taskDatabase --supported-schemas ($taskSchemas -join ',') --backup $taskBackup | Set-Content -LiteralPath (Join-Path $taskRun 'database-preflight.json') -Encoding utf8NoBOM
  if($LASTEXITCODE){throw 'Database compatibility/backup check blocked startup; see database-preflight.json.'}
 }
 foreach($taskRole in $taskRoles){
  $taskEnvName='VAC_LOCAL_'+$taskRole.ToUpper()+'_TOKEN';$taskPrevious[$taskEnvName]=[Environment]::GetEnvironmentVariable($taskEnvName,'Process')
  $taskToken=[Guid]::NewGuid().ToString('N')+[Guid]::NewGuid().ToString('N');[Environment]::SetEnvironmentVariable($taskEnvName,$taskToken,'Process');$taskTokens[$taskRole]=$taskEnvName
  $taskHash=[Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($taskToken))).ToLower()
  $taskAgents+=@{agent_id=$taskRole;role=$taskRole;token_sha256=$taskHash}
 }
 $taskPrevious['VAC_SESSION_STOP']=[Environment]::GetEnvironmentVariable('VAC_SESSION_STOP','Process')
 $env:VAC_SESSION_STOP=$taskStopPath
 Write-TaskJson (Join-Path $taskRun 'agents.json') @{agents=$taskAgents}
 $taskControl=@{http_addr="127.0.0.1:$Port";delivery_root=$taskDelivery;web_root=(Join-Path $taskRoot 'webui/dist');mcp_agents_file=(Join-Path $taskRun 'agents.json');lease_seconds=60;tts_python=(Get-Command python).Source;tts_worker_dir=(Join-Path $taskRoot 'workers/python')}
 if($taskIngestRoots){$taskControl.ingest_roots=@($taskIngestRoots | ForEach-Object {@{root_id=$_.root_id;name=$_.name;path=$_.path}})}
 Write-TaskJson (Join-Path $taskRun 'control.json') $taskControl
 $taskBinary=$ControlBinary
 if(!$taskBinary){
  $taskBinary=Join-Path $taskRun 'control-plane.exe';$taskOldCache=$env:GOCACHE;$taskOldProxy=$env:GOPROXY;$taskOldToolchain=$env:GOTOOLCHAIN
  try{$env:GOCACHE=if($taskOldCache){$taskOldCache}else{Join-Path $taskRoot 'build/cache/go'};$env:GOPROXY='off';$env:GOTOOLCHAIN='local';Push-Location (Join-Path $taskRoot 'control-plane');try{& go build -o $taskBinary .;if($LASTEXITCODE){throw 'Go build failed; local dependencies and a compatible Go toolchain are required.'}}finally{Pop-Location}}finally{$env:GOCACHE=$taskOldCache;$env:GOPROXY=$taskOldProxy;$env:GOTOOLCHAIN=$taskOldToolchain}
 }
 $taskServer=Start-Process -FilePath $taskBinary -ArgumentList @('-config',('"'+(Join-Path $taskRun 'control.json')+'"'),'-db',('"'+$taskDatabase+'"')) -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $taskRun 'control.out') -RedirectStandardError (Join-Path $taskRun 'control.err')
 $taskServerEntry=New-RecordedProcess $taskServer 'control';$taskOwned+=$taskServerEntry;Write-OwnedSnapshot
 $taskURL="http://127.0.0.1:$Port";$taskReady=$false
 for($taskAttempt=0;$taskAttempt -lt 100;$taskAttempt++){if($taskServer.HasExited){throw 'Control plane exited; see control.err'};try{Invoke-RestMethod "$taskURL/api/assets" -TimeoutSec 1 | Out-Null;$taskReady=$true;break}catch{Start-Sleep -Milliseconds 100}}
 if(!$taskReady){throw 'Control-plane readiness timeout'}
 $taskProfileID=[Uri]::EscapeDataString($taskProfile.profile_id)
 $taskProfileURI="$taskURL/api/processing-profiles/$taskProfileID/revisions/$($taskProfile.revision)"
 try{$taskResolved=Invoke-RestMethod $taskProfileURI -TimeoutSec 5}catch{
  if([int]$_.Exception.Response.StatusCode -ne 404){throw}
  if($taskProfile.revision -ne 1){throw 'The selected profile revision is not saved. Only revision 1 can be bootstrapped.'}
  $null=Invoke-RestMethod -Method Post -Uri "$taskURL/api/processing-profiles" -TimeoutSec 5 -ContentType 'application/json' -Body (@{profile=$taskProfile;expected_revision=0;idempotency_key=('local-bootstrap-'+$taskProfile.profile_id)}|ConvertTo-Json -Depth 20)
  $taskResolved=Invoke-RestMethod $taskProfileURI -TimeoutSec 5
 }
 $taskSaved=$taskResolved.profile
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
  $taskWorkerEntry=New-RecordedProcess $taskProcess $taskRole
  $taskSlot.identity_path=$taskWorkerEntry.identity_path;$taskSlot.started_at=$taskWorkerEntry.started_at
  $taskOwned+=$taskWorkerEntry;Write-OwnedSnapshot
 }
 Write-OwnedSnapshot
 Write-Host "Local pipeline: $taskURL/"
 Write-Host "Business data: $DataDirectory"
 Write-Host "Session logs: $taskRun"
 Write-Host "Worker profile: $($taskSaved.profile_id) revision $($taskSaved.revision). Existing saved revisions are reused; edit profiles through the application."
 Write-Host 'Import a controlled package into this delivery root, select the saved profile, and run preflight. Models are not downloaded or launched. Ctrl+C stops only this session.'
 if($taskIngestRoots){Write-Host "Raw recordings: register a file under a configured root in the WebUI, or run scripts/Import-Recording.ps1 -BaseUrl $taskURL -Path <file>."}
 $taskDeadline=if($RunSeconds -gt 0){[DateTime]::UtcNow.AddSeconds($RunSeconds)}else{[DateTime]::MaxValue}
 $taskEnteredRun=$true
 while([DateTime]::UtcNow -lt $taskDeadline -and !(Test-Path -LiteralPath $env:VAC_SESSION_STOP)){
  if($taskServer.HasExited){throw 'Control plane exited; stopping this session. Review control.err and restart with the same data directory.'}
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
   $taskWorkerEntry=New-RecordedProcess $taskSlot.process $taskSlot.role
   $taskSlot.identity_path=$taskWorkerEntry.identity_path;$taskSlot.started_at=$taskWorkerEntry.started_at
   $taskOwned+=$taskWorkerEntry
   $taskSlot.nextRestart=$null
   Write-OwnedSnapshot
   Write-Host "Restarted $($taskSlot.role) as instance generation $($taskSlot.generation)."
  }
  Start-Sleep -Milliseconds 500
 }
}finally{
 $taskStopErrors=@()
 try{
 if($taskActive){
  try{New-Item -ItemType File -Path (Assert-VacPlainPath $taskStopPath) -Force | Out-Null}catch{$taskStopErrors+='Cannot write session stop marker.'}
 }
 if($taskEnteredRun){
  $taskDrain=[DateTime]::UtcNow.AddSeconds(15)
  while([DateTime]::UtcNow -lt $taskDrain){
   $taskLive=@($taskWorkers | Where-Object { $_.process -and -not $_.process.HasExited })
   if($taskLive.Count -eq 0){ break }
   Start-Sleep -Milliseconds 300
  }
 }
 [array]::Reverse($taskOwned)
 foreach($taskEntry in $taskOwned){try{Stop-RecordedProcess $taskEntry}catch{$taskStopErrors+='An owned process could not be stopped; review owned-processes.json.'}}
 if($taskPersistent -and $taskActive){$taskActive.status=if($taskStopErrors.Count){'stop_failed'}else{'stopped'};Write-VacJson $taskActivePath $taskActive}
 }finally{
  foreach($taskName in $taskPrevious.Keys){[Environment]::SetEnvironmentVariable($taskName,$taskPrevious[$taskName],'Process')}
  if($taskLock){$taskLock.Dispose()}
 }
 if($taskStopErrors.Count){throw ($taskStopErrors -join ' ')}
 Write-Host 'Recorded pipeline processes stopped. Data and logs retained; forced worker exits may require task recovery on restart.'
}
