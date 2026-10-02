param(
 [Parameter(Mandatory=$true)][string]$Path,
 [Parameter(Mandatory=$true)][string]$IngestRootsFile,
 [string]$BaseUrl='http://127.0.0.1:8806',
 [string]$AssetId='',
 [switch]$AllowIngester,
 [switch]$Start
)
# Registers a recording by root id + relative path. The control plane never
# receives the absolute path, and this script never opens the file.
$ErrorActionPreference='Stop'
if($Start -and !$AllowIngester){Write-Host 'Note: -Start needs the asset to allow the ingester; pass -AllowIngester or grant it in the WebUI first.'}
$importFull=[IO.Path]::GetFullPath($Path)
if(!(Test-Path -LiteralPath $importFull -PathType Leaf)){throw "Recording not found: $importFull"}
$importRoot=$null;$importRel=$null
foreach($importEntry in @(Get-Content -LiteralPath $IngestRootsFile -Raw | ConvertFrom-Json)){
 $importBase=[IO.Path]::GetFullPath([string]$importEntry.path).TrimEnd('\','/')+[IO.Path]::DirectorySeparatorChar
 if($importFull.StartsWith($importBase,[StringComparison]::OrdinalIgnoreCase)){$importRoot=$importEntry.root_id;$importRel=$importFull.Substring($importBase.Length).Replace('\','/');break}
}
if(!$importRoot){throw 'The recording is not under any configured ingest root.'}
if(!$AssetId){$AssetId='rec_'+[DateTime]::UtcNow.ToString('yyyyMMddHHmmss')}
$importBody=@{asset_id=$AssetId;root_id=$importRoot;relative_path=$importRel;idempotency_key=[Guid]::NewGuid().ToString('N')}|ConvertTo-Json
$importOut=Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/recordings" -ContentType 'application/json; charset=utf-8' -Body ([Text.Encoding]::UTF8.GetBytes($importBody))
Write-Host "Registered $($importOut.source.source_id) on asset $AssetId (root $importRoot)."
if($AllowIngester){
 $importAgents=@($importOut.asset.allowed_agents)+'ingester' | Where-Object {$_} | Select-Object -Unique
 $importPatch=@{agent_visible=$true;locked=$false;allowed_agents=@($importAgents)}|ConvertTo-Json
 Invoke-RestMethod -Method Patch -Uri "$BaseUrl/api/assets/$AssetId" -ContentType 'application/json' -Body $importPatch | Out-Null
 Write-Host 'Asset now allows the local ingester.'
}
if($Start){
 $importStart=@{source_id=$importOut.source.source_id;expected_source_version=$importOut.source_version;idempotency_key=[Guid]::NewGuid().ToString('N')}|ConvertTo-Json
 $importRun=Invoke-RestMethod -Method Post -Uri "$BaseUrl/api/assets/$AssetId/ingest-runs" -ContentType 'application/json' -Body $importStart
 Write-Host "Ingest run $($importRun.run.run_id) queued. Review candidates in the WebUI."
}
