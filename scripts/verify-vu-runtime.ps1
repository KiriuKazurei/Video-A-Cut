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
$token=[guid]::NewGuid().ToString('N')+[guid]::NewGuid().ToString('N')
$digest=[Convert]::ToHexString([Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($token))).ToLowerInvariant()
$agents=Join-Path $dir 'agents.json'
@{agents=@(@{agent_id='runtime-editor';role='narrator';token_sha256=$digest})}|ConvertTo-Json -Depth 4|Set-Content -LiteralPath $agents -Encoding utf8
$config=Join-Path $dir 'control.json'
@{http_addr='127.0.0.1:0';delivery_root=$delivery;mcp_agents_file=$agents;tts_python='python';tts_worker_dir=(Join-Path $repo 'workers/python')}|ConvertTo-Json|Set-Content -LiteralPath $config -Encoding utf8
$start=[Diagnostics.ProcessStartInfo]::new()
$start.FileName=$Binary
$start.WorkingDirectory=$dir
$start.UseShellExecute=$false
$start.CreateNoWindow=$true
$start.RedirectStandardInput=$true
$start.RedirectStandardOutput=$true
$start.RedirectStandardError=$true
foreach($arg in @('-config',$config,'-db',(Join-Path $dir 'vac.db'),'-desktop')){$start.ArgumentList.Add($arg)}
$process=[Diagnostics.Process]::Start($start)
$stderr=$process.StandardError.ReadToEndAsync()
try {
 $line=$process.StandardOutput.ReadLineAsync()
 if(!$line.Wait(15000)){throw 'Runtime readiness timeout'}
 $ready=$line.Result|ConvertFrom-Json
 if($ready.event -ne 'ready'){throw 'Missing runtime readiness'}
 $base=$ready.api_base_url
 $skills=Invoke-RestMethod -Uri "$base/api/skills" -TimeoutSec 5 -NoProxy
 if(@($skills|Where-Object {$_.id -eq 'video-use' -and $_.builtin}).Count -ne 1){throw 'Builtin video-use missing'}
 $project=Invoke-RestMethod -Uri "$base/api/projects" -Method Post -ContentType 'application/json' -Body (@{name='Runtime smoke';flow='visual_heavy';expected_version=0;assets=@();visible=@()}|ConvertTo-Json) -TimeoutSec 5 -NoProxy
 if(!$project.id -or $project.version -ne 1){throw 'Project creation failed'}
 $headers=@{Authorization="Bearer $token";Accept='application/json, text/event-stream';'Mcp-Protocol-Version'='2026-07-28';'Mcp-Method'='tools/call';'Mcp-Name'='skills.get'}
 $body=@{jsonrpc='2.0';id=1;method='tools/call';params=@{name='skills.get';arguments=@{id='video-use'};_meta=@{'io.modelcontextprotocol/protocolVersion'='2026-07-28';'io.modelcontextprotocol/clientCapabilities'=@{}}}}|ConvertTo-Json -Depth 8
 $response=Invoke-RestMethod -Uri "$base/mcp" -Method Post -ContentType 'application/json' -Headers $headers -Body $body -TimeoutSec 5 -NoProxy
 if($response.error -or $response.result.isError -or $response.result.structuredContent.id -ne 'video-use'){throw 'MCP Skill distribution failed'}
 $result=@{runtime='passed';http_project='passed';mcp_skill='passed';builtin_count=@($skills).Count;model_calls=0;human_ui='not_run'}
 $result|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $env:VAC_TEST_OUTPUT 'vu-runtime-result.json') -Encoding utf8
 $result|ConvertTo-Json -Compress
} finally {
 if(!$process.HasExited){
  $process.StandardInput.WriteLine('{"command":"shutdown"}')
  $process.StandardInput.Close()
  if(!$process.WaitForExit(10000)){$process.Kill($true);$process.WaitForExit()}
 }
 $stderr.GetAwaiter().GetResult()|Set-Content -LiteralPath (Join-Path $dir 'runtime.log') -Encoding utf8
 $process.Dispose()
}
