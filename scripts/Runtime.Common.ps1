# Shared path and process identity checks. This file does not start services.
function Assert-VacPlainPath([string]$Path) {
    if (![IO.Path]::IsPathFullyQualified($Path) -or $Path.StartsWith('\\')) { throw 'An absolute path on a local drive is required.' }
    $full = [IO.Path]::GetFullPath($Path)
    for ($cursor = $full; $cursor; $cursor = Split-Path -Parent $cursor) {
        $entry = Get-Item -LiteralPath $cursor -Force -ErrorAction SilentlyContinue
        if ($entry -and ($entry.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
            throw "Reparse points are not supported in runtime paths: $cursor"
        }
    }
    return $full
}

function Resolve-VacSessionPath([string]$DataDirectory, [string]$SessionID) {
    if ($SessionID -notmatch '^[a-f0-9]{32}$') { throw 'Invalid runtime session identity.' }
    return Assert-VacPlainPath (Join-Path $DataDirectory "sessions/$SessionID")
}

function Test-VacProcessIdentity($Entry) {
    if (!$Entry -or $Entry.pid -lt 1 -or !$Entry.path -or !$Entry.started_at) { throw 'Incomplete process identity; automatic runtime changes are blocked.' }
    $current = Get-Process -Id $Entry.pid -ErrorAction SilentlyContinue
    if (!$current) { return $false }
    try {
        return $current.Path -eq $Entry.path -and
            $current.StartTime.ToUniversalTime().ToString('o') -eq $Entry.started_at
    } catch { throw 'Cannot verify process identity; automatic runtime changes are blocked.' }
}

function Write-VacJson([string]$Path, $Value) {
    $Path = Assert-VacPlainPath $Path
    # Replace atomically so the stop entry never consumes a partially written identity.
    $temporary = $Path + '.' + [Guid]::NewGuid().ToString('N') + '.tmp'
    try {
        $Value | ConvertTo-Json -Depth 20 | Set-Content -LiteralPath $temporary -Encoding utf8NoBOM
        [IO.File]::Move($temporary, $Path, $true)
    } finally {
        if (Test-Path -LiteralPath $temporary) { Remove-Item -LiteralPath $temporary }
    }
}

function Invoke-VacToolProbe([string]$Executable, [string[]]$Arguments) {
    $info = [Diagnostics.ProcessStartInfo]::new()
    $info.FileName = $Executable
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    foreach ($argument in $Arguments) { $info.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::new()
    $process.StartInfo = $info
    $buffers = @([char[]]::new(4096), [char[]]::new(4096))
    $streams = @($null, $null)
    $pending = @($null, $null)
    $finished = @($false, $false)
    $output = @([Text.StringBuilder]::new(), [Text.StringBuilder]::new())
    $started = $false
    try {
        if (!$process.Start()) { throw 'Tool could not be started.' }
        $started = $true
        $streams = @($process.StandardOutput, $process.StandardError)
        $deadline = [DateTime]::UtcNow.AddSeconds(5)
        while (!$process.HasExited -or !$finished[0] -or !$finished[1]) {
            if ([DateTime]::UtcNow -ge $deadline) { throw 'Local tool probe timed out.' }
            for ($index = 0; $index -lt 2; $index++) {
                if ($finished[$index]) { continue }
                if (!$pending[$index]) { $pending[$index] = $streams[$index].ReadAsync($buffers[$index], 0, 4096) }
                if (!$pending[$index].IsCompleted) { continue }
                $count = $pending[$index].GetAwaiter().GetResult()
                $pending[$index] = $null
                if (!$count) { $finished[$index] = $true; continue }
                if ($output[$index].Length + $count -gt 262144) { throw 'Local tool probe exceeded its output limit.' }
                $null = $output[$index].Append($buffers[$index], 0, $count)
            }
            Start-Sleep -Milliseconds 10
        }
        if ($process.ExitCode -ne 0) { throw 'Local tool probe failed.' }
        return ($output[0].ToString() + "`n" + $output[1].ToString())
    } finally {
        if ($started -and !$process.HasExited) { $process.Kill($true); $null = $process.WaitForExit(2000) }
        $process.Dispose()
    }
}
