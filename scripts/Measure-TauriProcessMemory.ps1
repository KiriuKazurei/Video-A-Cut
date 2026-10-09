param([Parameter(Mandatory)][ValidatePattern('^[0-9]+(,[0-9]+)*$')][string]$RootPid)
$ErrorActionPreference='Stop'
$all=@(Get-CimInstance Win32_Process)
$ids=[Collections.Generic.HashSet[int]]::new()
foreach($root in $RootPid.Split(',')){[void]$ids.Add([int]$root)}
do {$added=$false;foreach($process in $all){if($ids.Contains([int]$process.ParentProcessId) -and $ids.Add([int]$process.ProcessId)){$added=$true}}}while($added)
$details=@(foreach($process in $all){if($ids.Contains([int]$process.ProcessId)){@{pid=[int]$process.ProcessId;name=$process.Name;working_set=[long]$process.WorkingSetSize;private_bytes=[long]$process.PrivatePageCount}}})
@{working_set_bytes=[long](($details.working_set|Measure-Object -Sum).Sum);private_bytes=[long](($details.private_bytes|Measure-Object -Sum).Sum);processes=$details}|ConvertTo-Json -Depth 4 -Compress
