#Requires -Version 7.2
[CmdletBinding()]
param([string]$Binary = (Join-Path $env:VAC_TEST_OUTPUT 'vac-control-plane.exe'))
$ErrorActionPreference='Stop'
if (!$env:VAC_TEST_OUTPUT) { throw 'Run through TestWorkspace.ps1' }
$repo=[IO.Path]::GetFullPath((Split-Path -Parent $PSScriptRoot))
$dir=Join-Path $env:VAC_TEST_OUTPUT ('vu-runtime-'+[guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($dir)|Out-Null
$delivery=Join-Path $dir 'delivery'
[IO.Directory]::CreateDirectory($delivery)|Out-Null
$fixtureRoot=Join-Path $repo 'workers/node/timeline-cli/fixtures/media'
$token=[guid]::NewGuid().ToString('N')+[guid]::NewGuid().ToString('N')
$digest=[Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($token))).ToLowerInvariant()
$agents=Join-Path $dir 'agents.json'
@{agents=@(@{agent_id='runtime-editor';role='narrator';token_sha256=$digest})}|ConvertTo-Json -Depth 4|Set-Content -LiteralPath $agents -Encoding utf8
$config=Join-Path $dir 'control.json'
@{http_addr='127.0.0.1:0';delivery_root=$delivery;mcp_agents_file=$agents;mcp_local_access=$true;tts_python='python';tts_worker_dir=(Join-Path $repo 'workers/python');ingest_roots=@(@{root_id='runtime-fixtures';name='Shared fixed media';path=$fixtureRoot})}|ConvertTo-Json -Depth 5|Set-Content -LiteralPath $config -Encoding utf8
$start=[Diagnostics.ProcessStartInfo]::new()
$start.FileName=$Binary
$start.WorkingDirectory=$dir
$start.UseShellExecute=$false
$start.CreateNoWindow=$true
$start.RedirectStandardInput=$true
$start.RedirectStandardOutput=$true
$start.RedirectStandardError=$true
$humanToken=[guid]::NewGuid().ToString('N')+[guid]::NewGuid().ToString('N')
$start.Environment['VAC_HUMAN_ACCESS_TOKEN']=$humanToken
foreach($arg in @('-config',$config,'-db',(Join-Path $dir 'vac.db'),'-desktop')){$start.ArgumentList.Add($arg)}
$process=[Diagnostics.Process]::Start($start)
$stderr=$process.StandardError.ReadToEndAsync()
$checks=[Collections.Generic.List[string]]::new()
$films=[Collections.Generic.List[object]]::new()
function Invoke-Human([string]$Path,[string]$Method='GET',$Document=$null) {
 $parameters=@{Uri="$base$Path";Method=$Method;Headers=@{'X-VAC-Human-Token'=$humanToken};TimeoutSec=120;NoProxy=$true}
 if($null -ne $Document){$parameters.ContentType='application/json';$parameters.Body=$Document|ConvertTo-Json -Depth 40 -Compress}
 Invoke-RestMethod @parameters
}
function Invoke-Agent([string]$Name,$Document) {
 $headers=$localModern.Clone()
 $headers['Mcp-Method']='tools/call';$headers['Mcp-Name']=$Name
 $body=@{jsonrpc='2.0';id=30;method='tools/call';params=@{name=$Name;arguments=$Document;_meta=$localMeta}}|ConvertTo-Json -Depth 50 -Compress
 $response=Invoke-RestMethod -Uri "$base/mcp" -Method Post -ContentType 'application/json' -Headers $headers -Body $body -TimeoutSec 120 -NoProxy
 if($response.error){throw "MCP RPC error for $Name"}
 return $response.result
}
function Invoke-FlowTool([string]$Name,$ToolArguments=@{}) {
 $script:flowStep++
 $reply=Invoke-Agent 'skill_runs.tool' @{run_id=$script:flowRun.id;request=@{name=$Name;arguments=$ToolArguments;expected_version=$script:flowRun.version;idempotency_key="flow-$script:flowStep"}}
 if($reply.isError){throw "Flow tool $Name failed: $($reply.content|ConvertTo-Json -Depth 5 -Compress)"}
 $script:flowRun=$reply.structuredContent.run
 return $reply
}
function Test-FlowDelivery([double]$Duration,[string]$Label) {
 $null=Invoke-FlowTool 'renders.create'
 $movie=@($script:flowRun.artifacts|Where-Object {$_.kind -eq 'mp4' -and $_.plan_version -eq $script:flowRun.plan.version})[-1]
 $package=@($script:flowRun.artifacts|Where-Object {$_.kind -eq 'premiere_package' -and $_.plan_version -eq $script:flowRun.plan.version})[-1]
 if(!$movie.id -or !$package.id){throw 'Missing same-plan MP4/Premiere package'}
 $filmPath=Join-Path $dir "$Label.mp4"
 Invoke-WebRequest -Uri "$base/api/skill-runs/$($script:flowRun.id)/artifacts/$($movie.id)" -Headers @{'X-VAC-Human-Token'=$humanToken} -OutFile $filmPath -NoProxy -TimeoutSec 30
 if((Get-FileHash -LiteralPath $filmPath -Algorithm SHA256).Hash.ToLowerInvariant() -ne $movie.sha256){throw 'Downloaded film hash mismatch'}
 $mediaInfo=(& ffprobe -v error -show_entries 'format=duration:stream=codec_type' -of json $filmPath)|ConvertFrom-Json
 if($LASTEXITCODE -ne 0 -or [Math]::Abs([double]$mediaInfo.format.duration-$Duration) -gt .02 -or 'video' -notin @($mediaInfo.streams.codec_type)){throw 'Invalid rendered film duration/stream'}
 & ffmpeg -hide_banner -loglevel error -i $filmPath -f null -
 if($LASTEXITCODE -ne 0){throw 'Film cannot be decoded end to end'}
 $zipPath=Join-Path $dir "$Label.zip"
 Invoke-WebRequest -Uri "$base/api/skill-runs/$($script:flowRun.id)/artifacts/$($package.id)" -Headers @{'X-VAC-Human-Token'=$humanToken} -OutFile $zipPath -NoProxy -TimeoutSec 30
 $archive=[IO.Compression.ZipFile]::OpenRead($zipPath)
 try {if('edl.json' -notin @($archive.Entries.FullName) -or $archive.Entries.Count -lt 3){throw 'Incomplete Premiere package'}} finally {$archive.Dispose()}
 $workbench=Invoke-Human "/api/skill-runs/$($script:flowRun.id)/workbench"
 $preview=@($workbench.previews|Where-Object {$_.artifact.id -eq $movie.id -and $_.available})[0]
 if(!$preview.preview_url -or $preview.historical){throw 'Current film unavailable in VAC workbench'}
 $range=Invoke-WebRequest -Uri "$base$($preview.preview_url)" -Headers @{Range='bytes=0-127'} -NoProxy -TimeoutSec 10
 if([int]$range.StatusCode -ne 206 -or $range.RawContentLength -ne 128){throw 'Player preview ticket/Range failed'}
 $null=Invoke-FlowTool 'renders.inspect'
 $frameIDs=@($script:flowRun.inspection.frame_ids)
 foreach($frameID in $frameIDs) {
  $frame=Invoke-FlowTool 'artifacts.get' @{id=$frameID}
  $image=@($frame.content|Where-Object {$_.type -eq 'image'})
  if($image.Count -ne 1 -or [Convert]::FromBase64String($image[0].data).Length -lt 100){throw 'Rendered inspection frame missing native image'}
 }
 $null=Invoke-FlowTool 'renders.review' @{plan_version=$script:flowRun.plan.version;frame_ids=$frameIDs;verdict='passed';issues=@()}
 $null=Invoke-FlowTool 'run.finish'
 if($script:flowRun.state -ne 'succeeded'){throw 'Film workflow did not finish'}
 $films.Add(@{label=$Label;duration=[double]$mediaInfo.format.duration;sha256=$movie.sha256;plan_version=$movie.plan_version;decoded=$true;preview_range=206;premiere='verified'})
 return $movie
}
try {
 $line=$process.StandardOutput.ReadLineAsync()
 if(!$line.Wait(15000)){throw 'Runtime readiness timeout'}
 $ready=$line.Result|ConvertFrom-Json
 if($ready.event -ne 'ready'){throw 'Missing runtime readiness'}
 $base=$ready.api_base_url
 $mcpAuth=@{Authorization="Bearer $token";Accept='application/json, text/event-stream'}
 $discoverHeaders=$mcpAuth.Clone()
 $discoverHeaders['Mcp-Protocol-Version']='2026-07-28'
 $discoverHeaders['Mcp-Method']='server/discover'
 $discoverBody=@{jsonrpc='2.0';id=10;method='server/discover';params=@{_meta=@{'io.modelcontextprotocol/protocolVersion'='2026-07-28';'io.modelcontextprotocol/clientCapabilities'=@{}}}}|ConvertTo-Json -Depth 8
 $discovery=Invoke-RestMethod -Uri "$base/mcp" -Method Post -ContentType 'application/json' -Headers $discoverHeaders -Body $discoverBody -TimeoutSec 5 -NoProxy
 if($discovery.error -or !$discovery.result.capabilities.tools){throw 'MCP server discovery failed'}
 # Exercise a legacy client's actual initialize request instead of inferring
 # compatibility from a modern hand-crafted tools/call.
 $localHeaders=@{Accept='application/json, text/event-stream';'X-VAC-Agent-Source'='desktop-runtime-smoke'}
 $legacy=@()
 foreach($version in @('2025-03-26','2025-06-18','2025-11-25')) {
  $body=@{jsonrpc='2.0';id=11;method='initialize';params=@{protocolVersion=$version;capabilities=@{};clientInfo=@{name='vac-legacy-probe';version='1'}}}|ConvertTo-Json -Depth 8
  $probe=Invoke-WebRequest -Uri "$base/mcp" -Method Post -ContentType 'application/json' -Headers $localHeaders -Body $body -SkipHttpErrorCheck -TimeoutSec 5 -NoProxy
  $payload=$null
  try {$payload=$probe.Content|ConvertFrom-Json} catch { }
  $negotiated=$payload.result.protocolVersion
  $listStatus=$null
  $listError=$null
  $listed=$false
  if($negotiated) {
   $listHeaders=$localHeaders.Clone()
   $listHeaders['Mcp-Session-Id']=[string]$probe.Headers['Mcp-Session-Id'][0]
   $listHeaders['Mcp-Protocol-Version']=$negotiated
   $listBody=@{jsonrpc='2.0';id=12;method='tools/list';params=@{}}|ConvertTo-Json -Depth 8
   $listProbe=Invoke-WebRequest -Uri "$base/mcp" -Method Post -ContentType 'application/json' -Headers $listHeaders -Body $listBody -SkipHttpErrorCheck -TimeoutSec 5 -NoProxy
   $listStatus=[int]$listProbe.StatusCode
   $listPayload=$null
   try {$listPayload=$listProbe.Content|ConvertFrom-Json} catch { }
   $listed=($listStatus -eq 200 -and @($listPayload.result.tools).Count -gt 0)
   $listError=$listPayload.error.message
  }
  $legacy+=@{requested=$version;http_status=[int]$probe.StatusCode;negotiated=$negotiated;tools_status=$listStatus;tools_listed=$listed;compatible=([int]$probe.StatusCode -eq 200 -and $negotiated -eq $version -and $listed);error=$payload.error.message;tools_error=$listError;tools_detail=if(!$listed -and $negotiated){$listProbe.Content.Substring(0,[Math]::Min(300,$listProbe.Content.Length))}else{$null}}
  if(!$legacy[-1].compatible){throw "Legacy MCP regression: $($legacy[-1]|ConvertTo-Json -Compress)"}
 }
 $localModern=$localHeaders.Clone()
 $localModern['Mcp-Protocol-Version']='2026-07-28'
 $localModern['Mcp-Method']='server/discover'
 $localMeta=@{'io.modelcontextprotocol/protocolVersion'='2026-07-28';'io.modelcontextprotocol/clientCapabilities'=@{};'io.modelcontextprotocol/clientInfo'=@{name='vac-desktop-probe';version='1'}}
 $localDiscovery=Invoke-WebRequest -Uri "$base/mcp" -Method Post -ContentType 'application/json' -Headers $localModern -Body (@{jsonrpc='2.0';id=20;method='server/discover';params=@{_meta=$localMeta}}|ConvertTo-Json -Depth 8) -TimeoutSec 5 -NoProxy
 $localModern['Mcp-Session-Id']=[string]$localDiscovery.Headers['Mcp-Session-Id'][0]
 $localModern['Mcp-Method']='tools/call'
 $localModern['Mcp-Name']='skills.get'
 $localSkill=Invoke-RestMethod -Uri "$base/mcp" -Method Post -ContentType 'application/json' -Headers $localModern -Body (@{jsonrpc='2.0';id=21;method='tools/call';params=@{name='skills.get';arguments=@{id='video-use'};_meta=$localMeta}}|ConvertTo-Json -Depth 8) -TimeoutSec 5 -NoProxy
 if($localSkill.error -or $localSkill.result.isError -or !$localSkill.result.structuredContent.resources.'vac/tool-contracts.md'){throw 'Desktop MCP Skill contract distribution failed'}
 $localForbidden=Invoke-WebRequest -Uri "$base/api/agent-operations" -Headers $localModern -SkipHttpErrorCheck -TimeoutSec 5 -NoProxy
 if([int]$localForbidden.StatusCode -ne 403){throw 'MCP session gained human journal access'}
 $skills=Invoke-RestMethod -Uri "$base/api/skills" -TimeoutSec 5 -NoProxy
 if(@($skills|Where-Object {$_.id -eq 'video-use' -and $_.builtin}).Count -ne 1){throw 'Builtin video-use missing'}
 $project=Invoke-RestMethod -Uri "$base/api/projects" -Method Post -Headers @{'X-VAC-Human-Token'=$humanToken} -ContentType 'application/json' -Body (@{name='Runtime smoke';flow='visual_heavy';expected_version=0;assets=@();visible=@()}|ConvertTo-Json) -TimeoutSec 5 -NoProxy
 if(!$project.id -or $project.version -ne 1){throw 'Project creation failed'}
 $headers=@{Authorization="Bearer $token";Accept='application/json, text/event-stream';'Mcp-Protocol-Version'='2026-07-28';'Mcp-Method'='tools/call';'Mcp-Name'='skills.get'}
 $body=@{jsonrpc='2.0';id=1;method='tools/call';params=@{name='skills.get';arguments=@{id='video-use'};_meta=@{'io.modelcontextprotocol/protocolVersion'='2026-07-28';'io.modelcontextprotocol/clientCapabilities'=@{}}}}|ConvertTo-Json -Depth 8
 $response=Invoke-RestMethod -Uri "$base/mcp" -Method Post -ContentType 'application/json' -Headers $headers -Body $body -TimeoutSec 5 -NoProxy
 if($response.error -or $response.result.isError -or $response.result.structuredContent.id -ne 'video-use'){throw 'MCP Skill distribution failed'}
 # Exercise the actual human HTTP and Agent MCP boundaries continuously. No
 # store seeding or service-method shortcut is used for these operations.
 $project=Invoke-Human '/api/projects' 'POST' @{name='Full runtime flow';flow='lightweight';expected_version=0;assets=@();visible=@();accept_external=$true}
 $project=Invoke-Human "/api/projects/$($project.id)/media" 'POST' @{root_id='runtime-fixtures';relative_path='clip_01.mp4';expected_version=$project.version;idempotency_key='flow-import'}
 $asset=$project.assets[0]
 if(!$asset){throw 'Fixed media import did not register an asset'}
 $null=Invoke-Human "/api/assets/$asset" 'PATCH' @{agent_visible=$true;allowed_agents=@('narrator')}
 $project=Invoke-Human "/api/projects/$($project.id)" 'PUT' @{name=$project.name;flow='lightweight';expected_version=$project.version;assets=@($asset);visible=@($asset);accept_external=$true}
 $view=Invoke-Human "/api/projects/$($project.id)/visible-set"
 $view=Invoke-Human "/api/projects/$($project.id)/visible-set/confirm" 'POST' @{visible_set_sha=$view.sha256;expected_version=$project.version}
 $checks.Add('HTTP project/import/asset-role/scope confirmation')
 $created=Invoke-Agent 'skill_runs.create' @{project_id=$project.id;skill_id='video-use';execution_mode='external_mcp';visible_set_sha=$view.sha256;goal='Two authorized moments';idempotency_key='flow-run'}
 if($created.isError){throw "Cannot create external run: $($created.content|ConvertTo-Json -Compress)"}
 $script:flowRun=$created.structuredContent;$script:flowStep=0
 $null=Invoke-FlowTool 'media.list'
 $null=Invoke-FlowTool 'media.get_frames'
 $frameID=$script:flowRun.frames[0].id
 $frame=Invoke-FlowTool 'artifacts.get' @{id=$frameID}
 if(@($frame.content|Where-Object {$_.type -eq 'image'}).Count -ne 1){throw 'Source frame not delivered as native MCP image'}
 $sourceID=$view.media[0].id
 $null=Invoke-FlowTool 'summaries.submit' @{model='deterministic-runtime-client';narrative='Shared fixed media evidence';beats=@('Opening','Ending');timeline=@(@{media_id=$sourceID;start=0;end=.5;description='Fixed source';confidence='low';frame_ids=@($frameID)})}
 $plan=@{body="Opening`n`nEnding";subtitles=$false;segments=@(@{media_id=$sourceID;start=0;end=.5;narration='Opening'},@{media_id=$sourceID;start=.5;end=1;narration='Ending'})}
 $null=Invoke-FlowTool 'scripts.propose' $plan
 if($script:flowRun.state -ne 'waiting_human' -or $script:flowRun.plan.finalized_by){throw 'Proposal bypassed human approval'}
 $denied=Invoke-WebRequest -Uri "$base/api/skill-runs/$($script:flowRun.id)/finalize" -Method Post -Headers $localModern -ContentType 'application/json' -Body (@{expected_version=$script:flowRun.version}|ConvertTo-Json) -SkipHttpErrorCheck -NoProxy -TimeoutSec 5
 if([int]$denied.StatusCode -ne 403){throw 'Agent connection gained human approval permission'}
 $script:flowRun=Invoke-Human "/api/skill-runs/$($script:flowRun.id)/finalize" 'POST' @{expected_version=$script:flowRun.version}
 $first=Test-FlowDelivery 1 'initial-film'
 $checks.Add('MCP evidence/summary/plan + HTTP approval + decoded MP4/Premiere/preview')
 $script:flowRun=Invoke-Human "/api/skill-runs/$($script:flowRun.id)/edits" 'POST' @{expected_version=$script:flowRun.version;plan_version=$script:flowRun.plan.version;prompt='Shorten only opening';segment_index=0;artifact_id=$first.id;artifact_sha256=$first.sha256;idempotency_key='flow-segment-edit'}
 $context=Invoke-Agent 'skill_runs.get' @{run_id=$script:flowRun.id}
 $window=$context.structuredContent.run.edit_request.windows[0]
 if($window.timeline_start -ne 0 -or $window.timeline_end -ne .5 -or $window.source_end -ne .5){throw 'Segment edit lost rendered/source time windows'}
 $plan.segments[0].end=.3
 $null=Invoke-FlowTool 'scripts.propose' $plan
 $oldView=Invoke-Human "/api/skill-runs/$($script:flowRun.id)/workbench"
 $oldPreview=@($oldView.previews|Where-Object {$_.artifact.id -eq $first.id -and $_.historical -and $_.available})[0]
 if(!$oldPreview.preview_url){throw 'Prior film cannot be previewed while editing'}
 $null=Invoke-WebRequest -Uri "$base$($oldPreview.preview_url)" -Headers @{Range='bytes=0-127'} -NoProxy -TimeoutSec 10
 $spent=$script:flowRun.steps;$frameSpent=$script:flowRun.frames_spent
 $script:flowRun=Invoke-Human "/api/skill-runs/$($script:flowRun.id)/undo" 'POST' @{expected_version=$script:flowRun.version}
 if($script:flowRun.plan.segments[0].end -ne .5 -or $script:flowRun.plan.finalized_by -or $script:flowRun.steps -ne $spent){throw 'Undo failed or refunded budget/restored approval'}
 $script:flowRun=Invoke-Human "/api/skill-runs/$($script:flowRun.id)/redo" 'POST' @{expected_version=$script:flowRun.version}
 if($script:flowRun.plan.segments[0].end -ne .3 -or $script:flowRun.plan.segments[1].end -ne 1 -or $script:flowRun.plan.finalized_by -or $script:flowRun.frames_spent -ne $frameSpent){throw 'Redo changed unrelated content/approval/budget'}
 $script:flowRun=Invoke-Human "/api/skill-runs/$($script:flowRun.id)/finalize" 'POST' @{expected_version=$script:flowRun.version}
 $second=Test-FlowDelivery .8 'segment-edited-film'
 if($second.sha256 -eq $first.sha256){throw 'Segment edit did not change video'}
 $checks.Add('Segment prompt/source windows + historical preview + HTTP undo/redo + new approval/render')
 $script:flowRun=Invoke-Human "/api/skill-runs/$($script:flowRun.id)/edits" 'POST' @{expected_version=$script:flowRun.version;plan_version=$script:flowRun.plan.version;prompt='Shorten ending at film time 0.6';at_seconds=.6;artifact_id=$second.id;artifact_sha256=$second.sha256;idempotency_key='flow-time-edit'}
 $context=Invoke-Agent 'skill_runs.get' @{run_id=$script:flowRun.id}
 $window=$context.structuredContent.run.edit_request.windows[0]
 if($window.segment_index -ne 1 -or $window.timeline_start -ne .3 -or $window.source_start -ne .5){throw 'Point edit did not map film time to source/segment'}
 $plan.segments[1].end=.9
 $null=Invoke-FlowTool 'scripts.propose' $plan
 $script:flowRun=Invoke-Human "/api/skill-runs/$($script:flowRun.id)/finalize" 'POST' @{expected_version=$script:flowRun.version}
 $third=Test-FlowDelivery .7 'time-edited-film'
 $checks.Add('Film time-point to source mapping + third completed delivery')
 $cancel=Invoke-Agent 'skill_runs.create' @{project_id=$project.id;skill_id='video-use';execution_mode='external_mcp';visible_set_sha=$view.sha256;goal='Cancellation boundary';idempotency_key='flow-cancel-run'}
 if($cancel.isError){throw 'Cannot create cancellation fixture run'}
 $cancel=Invoke-Agent 'skill_runs.cancel' @{run_id=$cancel.structuredContent.id;expected_version=$cancel.structuredContent.version}
 if($cancel.isError -or $cancel.structuredContent.state -ne 'cancelled'){throw 'MCP cancellation failed'}
 $afterCancel=Invoke-Agent 'skill_runs.tool' @{run_id=$cancel.structuredContent.id;request=@{name='media.list';arguments=@{};expected_version=$cancel.structuredContent.version;idempotency_key='cancelled-denial'}}
 if(!$afterCancel.isError){throw 'Cancelled run accepted more tools'}
 $checks.Add('MCP cancel is terminal')
 $forbidden=Invoke-WebRequest -Uri "$base/api/agent-operations" -Headers $mcpAuth -SkipHttpErrorCheck -TimeoutSec 5 -NoProxy
 if([int]$forbidden.StatusCode -ne 403){throw 'Journal is not human-only'}
 $journal=Invoke-RestMethod -Uri "$base/api/agent-operations" -Headers @{'X-VAC-Human-Token'=$humanToken} -TimeoutSec 5 -NoProxy
 $chain=Invoke-RestMethod -Uri "$base/api/agent-operations/verify" -Headers @{'X-VAC-Human-Token'=$humanToken} -TimeoutSec 5 -NoProxy
 if(@($journal).Count -lt 1 -or $chain.verified_through -lt 1){throw 'Agent operation journal missing'}
 $reported=@($journal | Where-Object {$_.body.detail.agent.name -eq 'vac-desktop-probe' -and $_.body.detail.agent.source -eq 'desktop-runtime-smoke' -and $_.body.detail.agent.identity_kind -eq 'client_reported'})
 if($reported.Count -lt 2){throw 'Reported Agent name/source missing from journal'}
 $result=@{runtime='passed';http_project='passed';mcp_discovery='passed';mcp_skill='passed';desktop_mcp_no_credential='passed';agent_name_source_journal='passed';legacy_protocols=$legacy;full_flow_checks=@($checks.ToArray());films=@($films.ToArray());final_run_state=$script:flowRun.state;workspace=@{directory=$dir;delivery=$delivery;database=(Join-Path $dir 'vac.db');project_id=$project.id;run_id=$script:flowRun.id};human_journal='passed';unauthorized_journal='denied';builtin_count=@($skills).Count;model_calls=0;agent_decisions='deterministic_client_fixture';human_ui='not_run'}
 $result|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $env:VAC_TEST_OUTPUT 'vu-runtime-result.json') -Encoding utf8
 $result|ConvertTo-Json -Compress
} catch {
 @{runtime='failed';error=$_.Exception.Message;completed_checks=@($checks.ToArray());films=@($films.ToArray());model_calls=0}|ConvertTo-Json -Depth 10|Set-Content -LiteralPath (Join-Path $env:VAC_TEST_OUTPUT 'vu-runtime-failure.json') -Encoding utf8
 throw
} finally {
 if(!$process.HasExited){
  $process.StandardInput.WriteLine('{"command":"shutdown"}')
  $process.StandardInput.Close()
  if(!$process.WaitForExit(10000)){$process.Kill($true);$process.WaitForExit()}
 }
 $stderr.GetAwaiter().GetResult()|Set-Content -LiteralPath (Join-Path $dir 'runtime.log') -Encoding utf8
 $process.Dispose()
}
