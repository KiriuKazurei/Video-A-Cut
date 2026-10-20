param([Parameter(Mandatory)][int]$DesktopPid,[ValidateSet('inspect','close','resize','choose','cancel','confirm')][string]$Action='inspect',[string]$DialogTitle,[string]$FilePath,[int]$Width=1900,[int]$Height=1200)
$ErrorActionPreference='Stop'
$owned=Get-Process -Id $DesktopPid -ErrorAction Stop
if($owned.ProcessName -ne 'video-auto-cut-desktop'){throw 'Probe requires the owned desktop process'}
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class DesktopWindowProbe {
  public delegate bool EnumProc(IntPtr h,IntPtr p);
  [DllImport("user32.dll")] public static extern bool EnumWindows(EnumProc callback,IntPtr data);
  [DllImport("user32.dll")] public static extern bool EnumChildWindows(IntPtr parent,EnumProc callback,IntPtr data);
  [DllImport("user32.dll")] public static extern int GetDlgCtrlID(IntPtr h);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr h,System.Text.StringBuilder s,int n);
  public static IntPtr Control(IntPtr parent,int id,string cls) {
    IntPtr found=IntPtr.Zero;
    EnumChildWindows(parent,(h,p)=>{var name=new System.Text.StringBuilder(256);GetClassName(h,name,256);if(GetDlgCtrlID(h)==id&&name.ToString()==cls){found=h;return false;}return true;},IntPtr.Zero);
    return found;
  }
  public static string Controls(IntPtr parent) {
    var result=new System.Collections.Generic.List<string>();
    EnumChildWindows(parent,(h,p)=>{var cls=new System.Text.StringBuilder(256);var title=new System.Text.StringBuilder(256);GetClassName(h,cls,256);GetWindowText(h,title,256);result.Add(h.ToInt64()+":"+GetDlgCtrlID(h)+":"+cls+":"+title);return true;},IntPtr.Zero);
    return string.Join(", ",result);
  }
  [StructLayout(LayoutKind.Sequential)] public struct Rect { public int Left,Top,Right,Bottom; }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h,out Rect r);
  public static IntPtr FilenameEdit(IntPtr parent) {
    // Filename is the bottom visible Edit; address/search edits are above it.
    IntPtr found=IntPtr.Zero;int bottom=int.MinValue;
    EnumChildWindows(parent,(h,p)=>{var cls=new System.Text.StringBuilder(256);GetClassName(h,cls,256);Rect r;if(cls.ToString()=="Edit"&&IsWindowVisible(h)&&GetWindowRect(h,out r)&&r.Bottom>bottom){found=h;bottom=r.Bottom;}return true;},IntPtr.Zero);
    return found;
  }
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr h,System.Text.StringBuilder s,int n);
  public static IntPtr Main(uint pid) {
    IntPtr found=IntPtr.Zero;
    EnumWindows((h,p)=>{uint owner;GetWindowThreadProcessId(h,out owner);if(owner==pid){var title=new System.Text.StringBuilder(256);GetWindowText(h,title,256);if(title.ToString()=="Video Auto Cut"){found=h;return false;}}return true;},IntPtr.Zero);
    return found;
  }
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h,IntPtr after,int x,int y,int w,int height,uint flags);
  [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h,uint m,IntPtr w,IntPtr l);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern IntPtr SendMessage(IntPtr h,uint m,IntPtr w,string l);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string cls,string title);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h,out uint pid);
}
'@
if($Action -in @('inspect','close','resize')) {
  $handle=[DesktopWindowProbe]::Main([uint32]$DesktopPid)
  [uint32]$windowOwner=0
  [DesktopWindowProbe]::GetWindowThreadProcessId($handle,[ref]$windowOwner)|Out-Null
  if($windowOwner -ne $DesktopPid){throw 'Main window owner mismatch'}
  if($Action -eq 'close') {
    if($handle -eq 0){throw 'No visible window to close'}
    [DesktopWindowProbe]::PostMessage($handle,0x0010,[IntPtr]::Zero,[IntPtr]::Zero)|Out-Null
  }
  if($Action -eq 'resize') {
    if($Width -lt 1400 -or $Height -lt 950){throw 'Test size is below desktop minimum'}
    if(![DesktopWindowProbe]::SetWindowPos($handle,[IntPtr]::Zero,0,0,$Width,$Height,6)){throw 'Owned window resize failed'}
  }
  @{pid=$DesktopPid;handle=$handle.ToInt64();visible=($handle -ne 0 -and [DesktopWindowProbe]::IsWindowVisible($handle))}|ConvertTo-Json -Compress
  exit
}
Add-Type -AssemblyName UIAutomationClient,UIAutomationTypes
$root=[System.Windows.Automation.AutomationElement]::RootElement
$condition=New-Object System.Windows.Automation.AndCondition @(
  (New-Object System.Windows.Automation.PropertyCondition ([System.Windows.Automation.AutomationElement]::ProcessIdProperty,$DesktopPid)),
  (New-Object System.Windows.Automation.PropertyCondition ([System.Windows.Automation.AutomationElement]::NameProperty,$DialogTitle))
)
$dialog=$null
function Find-OwnedDialog {
  foreach($candidate in $root.FindAll([System.Windows.Automation.TreeScope]::Children,$condition)) {
    if([DesktopWindowProbe]::IsWindowVisible([IntPtr]$candidate.Current.NativeWindowHandle)){return $candidate}
  }
}
for($attempt=0;$attempt -lt 80 -and !$dialog;$attempt++){
  $dialog=Find-OwnedDialog
  if(!$dialog){Start-Sleep -Milliseconds 100}
}
if(!$dialog){throw "Owned dialog not found: $DialogTitle"}
[DesktopWindowProbe]::SetForegroundWindow([IntPtr]$dialog.Current.NativeWindowHandle)|Out-Null
if($Action -eq 'choose') {
  if(![IO.Path]::IsPathFullyQualified($FilePath)){throw 'Absolute file path required'}
  $FilePath=[IO.Path]::GetFullPath($FilePath)
  $filename=[IntPtr]::Zero
  $valuePattern=$null
  for($attempt=0;$attempt -lt 80 -and $filename -eq [IntPtr]::Zero -and !$valuePattern;$attempt++) {
    $hostControl=$dialog.FindFirst([System.Windows.Automation.TreeScope]::Descendants,(New-Object System.Windows.Automation.PropertyCondition ([System.Windows.Automation.AutomationElement]::AutomationIdProperty,'FileNameControlHost')))
    if($hostControl) {
      foreach($candidate in $hostControl.FindAll([System.Windows.Automation.TreeScope]::Descendants,[System.Windows.Automation.Condition]::TrueCondition)) {
        $pattern=$null
        if($candidate.TryGetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern,[ref]$pattern) -and !$pattern.Current.IsReadOnly){$valuePattern=$pattern;break}
        if($candidate.Current.ClassName -eq 'Edit' -and $candidate.Current.NativeWindowHandle -ne 0){$filename=[IntPtr]$candidate.Current.NativeWindowHandle;break}
      }
    }
    if($filename -eq [IntPtr]::Zero -and !$valuePattern){$filename=[DesktopWindowProbe]::FilenameEdit([IntPtr]$dialog.Current.NativeWindowHandle)}
    if($filename -eq [IntPtr]::Zero -and !$valuePattern){Start-Sleep -Milliseconds 100}
  }
  if($valuePattern){$valuePattern.SetValue($FilePath)}
  elseif($filename -ne [IntPtr]::Zero){
    try{[System.Windows.Automation.AutomationElement]::FromHandle($filename).SetFocus()}catch{}
    [DesktopWindowProbe]::SendMessage($filename,0x000C,[IntPtr]::Zero,$FilePath)|Out-Null
  } else {
    throw ('Filename value control not found; owned Win32 controls: '+[DesktopWindowProbe]::Controls([IntPtr]$dialog.Current.NativeWindowHandle))
  }
  Start-Sleep -Milliseconds 250
}
$buttonId=$(if($Action -eq 'cancel'){'2'}else{'1'})
$buttonHandle=[DesktopWindowProbe]::Control([IntPtr]$dialog.Current.NativeWindowHandle,[int]$buttonId,'Button')
if($DialogTitle -eq '退出 Video Auto Cut') {
  $buttonName=$(if($Action -eq 'cancel'){'继续运行'}else{'退出并停止'})
  $button=$dialog.FindFirst([System.Windows.Automation.TreeScope]::Descendants,(New-Object System.Windows.Automation.PropertyCondition ([System.Windows.Automation.AutomationElement]::NameProperty,$buttonName)))
  if($button){$buttonHandle=[IntPtr]$button.Current.NativeWindowHandle}
}
if($DialogTitle -eq '安装 Video Auto Cut 更新') {
  $buttonName=$(if($Action -eq 'cancel'){'稍后安装'}else{'安装并重启'})
  $button=$dialog.FindFirst([System.Windows.Automation.TreeScope]::Descendants,(New-Object System.Windows.Automation.PropertyCondition ([System.Windows.Automation.AutomationElement]::NameProperty,$buttonName)))
  if($button){$buttonHandle=[IntPtr]$button.Current.NativeWindowHandle}
}
if($buttonHandle -eq [IntPtr]::Zero){throw 'Dialog action button not found'}
[DesktopWindowProbe]::PostMessage($buttonHandle,0x00F5,[IntPtr]::Zero,[IntPtr]::Zero)|Out-Null
if($Action -eq 'choose') {
  # The shell may consume the first action to navigate to an absolute directory.
  # Only retry inside this same owned dialog, never use global keyboard input.
  for($attempt=0;$attempt -lt 3;$attempt++) {
    Start-Sleep -Milliseconds 400
    $same=Find-OwnedDialog
    if(!$same){break}
    if($valuePattern){$valuePattern.SetValue($FilePath)}elseif($filename -ne [IntPtr]::Zero){[DesktopWindowProbe]::SendMessage($filename,0x000C,[IntPtr]::Zero,$FilePath)|Out-Null}
    $accept=[DesktopWindowProbe]::Control([IntPtr]$same.Current.NativeWindowHandle,1,'Button')
    if($accept -ne [IntPtr]::Zero){[DesktopWindowProbe]::PostMessage($accept,0x00F5,[IntPtr]::Zero,[IntPtr]::Zero)|Out-Null}
  }
}
@{dialog=$DialogTitle;action=$Action;pid=$DesktopPid}|ConvertTo-Json -Compress
