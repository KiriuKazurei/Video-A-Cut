$ErrorActionPreference='Stop'
$guardBase=$env:VAC_TEST_ROOT
if(!$guardBase){$guardBase=& (Join-Path $PSScriptRoot 'TestWorkspace.ps1') -Action Prepare -Suite guard}
$guardRoot=Join-Path $guardBase ('runtime-state-guard/'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Force $guardRoot|Out-Null
$guardSelf=Get-Process -Id $PID
$guardSession=[guid]::NewGuid().ToString('N')
@{session_id=$guardSession;status='running';supervisor=@{pid=$PID;path=$guardSelf.Path;started_at=$guardSelf.StartTime.ToUniversalTime().ToString('o')}}|ConvertTo-Json -Depth 5|Set-Content (Join-Path $guardRoot 'active-session.json') -Encoding utf8NoBOM
$env:PYTHONDONTWRITEBYTECODE='1'
$guardOutput=& python (Join-Path $PSScriptRoot 'runtime_state.py') --data-directory $guardRoot --database (Join-Path $guardRoot 'control.db') --supported-schemas 1 --backup (Join-Path $guardRoot 'backup.db')
if($LASTEXITCODE -ne 1 -or (($guardOutput|ConvertFrom-Json).message -ne 'Previous supervisor is still alive; stop its session first.')){throw 'Live legacy supervisor was not rejected'}
@{session_id=$guardSession;status='stopped'}|ConvertTo-Json|Set-Content (Join-Path $guardRoot 'active-session.json') -Encoding utf8NoBOM
$guardOutput=& python (Join-Path $PSScriptRoot 'runtime_state.py') --data-directory $guardRoot --database (Join-Path $guardRoot 'control.db') --supported-schemas 1 --backup (Join-Path $guardRoot 'backup.db')
if($LASTEXITCODE -ne 0 -or (($guardOutput|ConvertFrom-Json).status -ne 'new_database')){throw 'Stopped legacy session did not release data'}
@{passed=$true;scope='live legacy process identity rejected; stopped session accepted';directory=$guardRoot}|ConvertTo-Json|Set-Content (Join-Path $guardRoot 'result.json') -Encoding utf8NoBOM
Write-Output 'PASS: legacy process identity preflight'
