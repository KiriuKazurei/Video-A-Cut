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
for($attempt=0;$attempt -lt 80 -and !$dialog;$attempt++){
  $dialog=$root.FindFirst([System.Windows.Automation.TreeScope]::Children,$condition)
  if(!$dialog){Start-Sleep -Milliseconds 100}
}
if(!$dialog){throw "Owned dialog not found: $DialogTitle"}
[DesktopWindowProbe]::SetForegroundWindow([IntPtr]$dialog.Current.NativeWindowHandle)|Out-Null
if($Action -eq 'choose') {
  if(![IO.Path]::IsPathFullyQualified($FilePath)){throw 'Absolute file path required'}
  $FilePath=[IO.Path]::GetFullPath($FilePath)
  $filename=[IntPtr]::Zero
  for($attempt=0;$attempt -lt 80 -and $filename -eq [IntPtr]::Zero;$attempt++) {
    $filename=[DesktopWindowProbe]::Control([IntPtr]$dialog.Current.NativeWindowHandle,1148,'Edit')
    if($filename -eq [IntPtr]::Zero){Start-Sleep -Milliseconds 100}
  }
  if($filename -eq [IntPtr]::Zero){throw 'Filename edit not found'}
  [DesktopWindowProbe]::SendMessage($filename,0x000C,[IntPtr]::Zero,$FilePath)|Out-Null
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
    $same=$root.FindFirst([System.Windows.Automation.TreeScope]::Children,$condition)
    if(!$same){break}
    $accept=[DesktopWindowProbe]::Control([IntPtr]$same.Current.NativeWindowHandle,1,'Button')
    if($accept -ne [IntPtr]::Zero){[DesktopWindowProbe]::PostMessage($accept,0x00F5,[IntPtr]::Zero,[IntPtr]::Zero)|Out-Null}
  }
}
@{dialog=$DialogTitle;action=$Action;pid=$DesktopPid}|ConvertTo-Json -Compress
