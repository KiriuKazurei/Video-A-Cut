#Requires -Version 7.2
param([switch]$Debug,[switch]$Installer,[ValidatePattern('^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$')][string]$Version)
$ErrorActionPreference='Stop'
$desktopRepo=Split-Path -Parent $PSScriptRoot
$desktopRoot=Join-Path $desktopRepo 'desktop'
$buildRoot=Join-Path $desktopRepo 'build/desktop'
$oldCache=$env:GOCACHE;$oldProxy=$env:GOPROXY;$oldTarget=$env:CARGO_TARGET_DIR
$oldTauriConfig=$env:TAURI_CONFIG;$oldToolchain=$env:RUSTUP_TOOLCHAIN;$oldPrivateKey=$env:TAURI_SIGNING_PRIVATE_KEY
$oldTemp=$env:TEMP;$oldTmp=$env:TMP
if($Installer -and $Debug){throw 'Debug binaries cannot be published as signed release installers'}
try {
  $env:GOCACHE=Join-Path $desktopRepo 'build/cache/go';$env:GOPROXY='off';$env:CARGO_TARGET_DIR=$buildRoot
  $env:RUSTUP_TOOLCHAIN='1.98.1'
  $sourceConfig=Get-Content -LiteralPath (Join-Path $desktopRoot 'tauri.conf.json') -Raw|ConvertFrom-Json
  if(!$Version){$Version=$sourceConfig.version}
  $overlay=@{version=$Version;bundle=@{active=[bool]$Installer;createUpdaterArtifacts=[bool]$Installer;useLocalToolsDir=$true}}
  $env:TAURI_CONFIG=$overlay|ConvertTo-Json -Depth 5 -Compress
  & npm --prefix (Join-Path $desktopRepo 'webui') run build
  if($LASTEXITCODE){throw 'WebUI build failed'}
  New-Item -ItemType Directory -Force (Join-Path $desktopRoot 'binaries')|Out-Null
  $binary=Join-Path $desktopRoot 'binaries/control-plane-x86_64-pc-windows-msvc.exe'
  & go -C (Join-Path $desktopRepo 'control-plane') build -o $binary .
  if($LASTEXITCODE){throw 'Go build failed'}
  $migrations=@(Get-ChildItem -LiteralPath (Join-Path $desktopRepo 'control-plane/internal/store/migrations') -File -Filter '*.sql'|Sort-Object Name)
  $manifest=@{schema_version=1;schema_versions=@($migrations|ForEach-Object {[int]$_.Name.Substring(0,4)});binary_sha256=(Get-FileHash -LiteralPath $binary -Algorithm SHA256).Hash.ToLowerInvariant()}
  $manifest|ConvertTo-Json -Depth 5|Set-Content -LiteralPath ($binary+'.runtime.json') -Encoding utf8NoBOM
  $arguments=@('+1.98.1','build','--offline','--locked','--manifest-path',(Join-Path $desktopRoot 'Cargo.toml'))
  if(!$Debug){$arguments+='--release'}
  & cargo @arguments
  if($LASTEXITCODE){throw 'Rust build failed; missing build dependencies must be installed explicitly'}
  if($Installer){
    if(!$env:TAURI_SIGNING_PRIVATE_KEY){
      $env:TAURI_SIGNING_PRIVATE_KEY=Join-Path $desktopRepo 'build/desktop-signing/update.key'
      if(!(Test-Path -LiteralPath $env:TAURI_SIGNING_PRIVATE_KEY)){throw 'A local updater signing key or TAURI_SIGNING_PRIVATE_KEY is required'}
    }
    # Keep extraction and cached bundler tools on one drive. Windows rename()
    # cannot move extracted NSIS tools across different filesystem volumes.
    $bundlerTemp=Join-Path $buildRoot 'bundler-temp'
    New-Item -ItemType Directory -Force $bundlerTemp|Out-Null
    $env:TEMP=$bundlerTemp;$env:TMP=$bundlerTemp
    $bundleOverlay=Join-Path $buildRoot 'bundle-config.json'
    $env:TAURI_CONFIG|Set-Content -LiteralPath $bundleOverlay -Encoding utf8NoBOM
    Push-Location $desktopRoot
    try {
      & ./node_modules/.bin/tauri.cmd bundle --config $bundleOverlay --bundles nsis --ci
      if($LASTEXITCODE){throw 'Signed NSIS installer build failed'}
    } finally {Pop-Location}
    $installerFile=Get-ChildItem -LiteralPath (Join-Path $buildRoot 'release/bundle/nsis') -File -Filter "*_$($Version)_x64-setup.exe"|Select-Object -First 1
    if(!$installerFile -or !(Test-Path -LiteralPath ($installerFile.FullName+'.sig'))){throw 'Signed updater artifacts are missing'}
    $releaseDirectory=Join-Path $buildRoot "updates/$Version"
    New-Item -ItemType Directory -Force $releaseDirectory|Out-Null
    Copy-Item -LiteralPath $installerFile.FullName,($installerFile.FullName+'.sig') -Destination $releaseDirectory -Force
    & (Join-Path $PSScriptRoot 'New-TauriUpdateManifest.ps1') -Version $Version -Installer (Join-Path $releaseDirectory $installerFile.Name) -OutputPath (Join-Path $releaseDirectory 'latest.json')
    Write-Output "Signed update artifacts: $releaseDirectory"
  }
  $compiledOutput=Join-Path $buildRoot $(if($Debug){'debug'}else{'release'})
  $output=Join-Path $buildRoot $(if($Debug){'app-debug'}else{'app'})
  New-Item -ItemType Directory -Force $output|Out-Null
  Copy-Item -LiteralPath (Join-Path $compiledOutput 'video-auto-cut-desktop.exe') -Destination (Join-Path $output 'video-auto-cut-desktop.exe') -Force
  Copy-Item -LiteralPath $binary -Destination (Join-Path $output 'control-plane.exe') -Force
  Copy-Item -LiteralPath ($binary+'.runtime.json') -Destination (Join-Path $output 'control-plane.exe.runtime.json') -Force
  $runtime=Join-Path $output 'runtime'
  New-Item -ItemType Directory -Force $runtime|Out-Null
  foreach($directory in @('workers','contracts')) {
    New-Item -ItemType Directory -Force (Join-Path $runtime $directory)|Out-Null
    # Copy runtime sources/dependencies, excluding tests, caches and development output.
    & robocopy (Join-Path $desktopRepo $directory) (Join-Path $runtime $directory) /E /XD tests test scripts fixtures __pycache__ .venv .run-data /XF '*.pyc' /NFL /NDL /NJH /NJS /NP
    if($LASTEXITCODE -gt 7){throw 'Runtime resource copy failed'}
  }
  $scriptSkillTarget=Join-Path $runtime 'skills/vac-script-syntax'
  New-Item -ItemType Directory -Force $scriptSkillTarget|Out-Null
  & robocopy (Join-Path $desktopRepo 'skills/vac-script-syntax') $scriptSkillTarget /E /NFL /NDL /NJH /NJS /NP
  if($LASTEXITCODE -gt 7){throw 'Script skill resource copy failed'}
  New-Item -ItemType Directory -Force (Join-Path $runtime 'scripts')|Out-Null
  Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'runtime_state.py') -Destination (Join-Path $runtime 'scripts/runtime_state.py') -Force
  Copy-Item -LiteralPath (Join-Path $desktopRepo 'configs/phase6/profile.builtin.example.json') -Destination (Join-Path $runtime 'default-profile.json') -Force
  foreach($document in @('README.md','ACCEPTANCE.md','UPDATER.md','desktop.example.json')) {
    Copy-Item -LiteralPath (Join-Path $desktopRoot $document) -Destination (Join-Path $output $document) -Force
  }
  Write-Host "Desktop executable: $(Join-Path $output 'video-auto-cut-desktop.exe')"
  # robocopy uses 1..7 for successful copies; callers expect a normal success code.
  $global:LASTEXITCODE=0
} finally {
  $env:GOCACHE=$oldCache;$env:GOPROXY=$oldProxy;$env:CARGO_TARGET_DIR=$oldTarget
  $env:TAURI_CONFIG=$oldTauriConfig;$env:RUSTUP_TOOLCHAIN=$oldToolchain;$env:TAURI_SIGNING_PRIVATE_KEY=$oldPrivateKey
  $env:TEMP=$oldTemp;$env:TMP=$oldTmp
}
