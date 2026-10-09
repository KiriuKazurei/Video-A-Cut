param([Parameter(Mandatory)][int]$DesktopPid)
$ErrorActionPreference='Stop'
if((Get-Process -Id $DesktopPid).ProcessName -ne 'video-auto-cut-desktop'){throw 'Owned desktop required'}
Add-Type -AssemblyName UIAutomationClient,UIAutomationTypes
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class OwnedTrayClick {
  [StructLayout(LayoutKind.Sequential)] public struct Point {public int X;public int Y;}
  [DllImport("user32.dll")] public static extern bool GetCursorPos(out Point p);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x,int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f,uint x,uint y,uint d,UIntPtr extra);
}
'@
function Invoke-TrayElement($element) {
  try {$point=$element.GetClickablePoint()} catch {
    $rect=$element.Current.BoundingRectangle
    if($element.Current.IsOffscreen -or !$element.Current.IsEnabled -or $rect.IsEmpty){throw}
    $point=[pscustomobject]@{X=$rect.X+$rect.Width/2;Y=$rect.Y+$rect.Height/2}
  }
  $oldPoint=New-Object OwnedTrayClick+Point
  [void][OwnedTrayClick]::GetCursorPos([ref]$oldPoint)
  try {
    [void][OwnedTrayClick]::SetCursorPos([int]$point.X,[int]$point.Y)
    [OwnedTrayClick]::mouse_event(2,0,0,0,[UIntPtr]::Zero)
    [OwnedTrayClick]::mouse_event(4,0,0,0,[UIntPtr]::Zero)
  } finally {[void][OwnedTrayClick]::SetCursorPos($oldPoint.X,$oldPoint.Y)}
}
function Find-OwnedIcon($container) {
  foreach($element in $container.FindAll([System.Windows.Automation.TreeScope]::Descendants,[System.Windows.Automation.Condition]::TrueCondition)) {
    if($element.Current.Name.Trim() -eq $tooltip){return $element}
  }
}
$root=[System.Windows.Automation.AutomationElement]::RootElement
$tooltip='Video Auto Cut · 关闭窗口后任务继续运行'
$taskbar=$root.FindFirst([System.Windows.Automation.TreeScope]::Children,(New-Object System.Windows.Automation.PropertyCondition ([System.Windows.Automation.AutomationElement]::ClassNameProperty,'Shell_TrayWnd')))
if(!$taskbar){throw 'Taskbar unavailable'}
$icon=Find-OwnedIcon $taskbar
if(!$icon) {
  $overflow=$taskbar.FindFirst([System.Windows.Automation.TreeScope]::Descendants,(New-Object System.Windows.Automation.PropertyCondition ([System.Windows.Automation.AutomationElement]::NameProperty,'显示隐藏的图标')))
  if($overflow){Invoke-TrayElement $overflow;Start-Sleep -Milliseconds 600}
  foreach($child in $root.FindAll([System.Windows.Automation.TreeScope]::Children,[System.Windows.Automation.Condition]::TrueCondition)) {
    if($child.Current.ClassName -match 'Overflow|CoreWindow|Xaml') {$icon=Find-OwnedIcon $child;if($icon){break}}
  }
}
if(!$icon){throw 'Desktop tray icon is not exposed by the current shell accessibility tree'}
Invoke-TrayElement $icon
Start-Sleep -Milliseconds 300
& (Join-Path $PSScriptRoot 'Tauri-WindowProbe.ps1') -DesktopPid $DesktopPid -Action inspect
