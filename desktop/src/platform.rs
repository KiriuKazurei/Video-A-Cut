use std::{
    io,
    process::{Child, Command},
};
#[cfg(windows)]
use windows_sys::Win32::{
    Foundation::*,
    System::{Diagnostics::ToolHelp::*, JobObjects::*, Threading::*},
};

#[cfg(windows)]
pub fn clipboard_text() -> Result<String, String> {
    use windows_sys::Win32::System::{DataExchange::*, Memory::*};
    unsafe {
        let mut opened = false;
        for _ in 0..10 {
            if OpenClipboard(std::ptr::null_mut()) != 0 {
                opened = true;
                break;
            }
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
        if !opened {
            return Err("剪贴板正被其他程序使用，请稍后重试".into());
        }
        struct Clipboard;
        impl Drop for Clipboard {
            fn drop(&mut self) {
                unsafe {
                    CloseClipboard();
                }
            }
        }
        let _clipboard = Clipboard;
        const UNICODE_TEXT: u32 = 13;
        if IsClipboardFormatAvailable(UNICODE_TEXT) == 0 {
            return Ok(String::new());
        }
        let handle = GetClipboardData(UNICODE_TEXT);
        if handle.is_null() {
            return Err(io::Error::last_os_error().to_string());
        }
        let size = GlobalSize(handle) / 2;
        if size > 8 * 1024 * 1024 {
            return Err("剪贴板文本超过可粘贴的大小".into());
        }
        let pointer = GlobalLock(handle) as *const u16;
        if pointer.is_null() {
            return Err(io::Error::last_os_error().to_string());
        }
        let text = std::slice::from_raw_parts(pointer, size);
        let end = text.iter().position(|&c| c == 0).unwrap_or(text.len());
        let result = String::from_utf16_lossy(&text[..end]);
        GlobalUnlock(handle);
        Ok(result)
    }
}

#[cfg(not(windows))]
pub fn clipboard_text() -> Result<String, String> {
    Err("当前平台使用浏览器剪贴板接口".into())
}

#[cfg(windows)]
pub fn write_clipboard_text(owner: isize, text: &str) -> Result<(), String> {
    use windows_sys::Win32::System::{DataExchange::*, Memory::*};
    let encoded: Vec<u16> = text.encode_utf16().chain(std::iter::once(0)).collect();
    if text.contains('\0') || encoded.len() > 8 * 1024 * 1024 {
        return Err("剪贴板文本无效或超过可复制的大小".into());
    }
    unsafe {
        struct Memory(HGLOBAL);
        impl Drop for Memory {
            fn drop(&mut self) {
                if !self.0.is_null() {
                    unsafe {
                        GlobalFree(self.0);
                    }
                }
            }
        }
        let mut memory = Memory(GlobalAlloc(GMEM_MOVEABLE, encoded.len() * 2));
        if memory.0.is_null() {
            return Err(io::Error::last_os_error().to_string());
        }
        let pointer = GlobalLock(memory.0) as *mut u16;
        if pointer.is_null() {
            return Err(io::Error::last_os_error().to_string());
        }
        std::ptr::copy_nonoverlapping(encoded.as_ptr(), pointer, encoded.len());
        GlobalUnlock(memory.0);
        let mut opened = false;
        for _ in 0..10 {
            if OpenClipboard(owner as HWND) != 0 {
                opened = true;
                break;
            }
            std::thread::sleep(std::time::Duration::from_millis(10));
        }
        if !opened {
            return Err("剪贴板正被其他程序使用，请稍后重试".into());
        }
        struct Clipboard;
        impl Drop for Clipboard {
            fn drop(&mut self) {
                unsafe {
                    CloseClipboard();
                }
            }
        }
        let _clipboard = Clipboard;
        if EmptyClipboard() == 0 || SetClipboardData(13, memory.0).is_null() {
            return Err(io::Error::last_os_error().to_string());
        }
        // Windows owns the movable allocation after SetClipboardData succeeds.
        memory.0 = std::ptr::null_mut();
        Ok(())
    }
}

#[cfg(not(windows))]
pub fn write_clipboard_text(_owner: isize, _text: &str) -> Result<(), String> {
    Err("当前平台使用浏览器剪贴板接口".into())
}

#[cfg(windows)]
pub struct Job(HANDLE);
#[cfg(windows)]
unsafe impl Send for Job {}
#[cfg(windows)]
unsafe impl Sync for Job {}
#[cfg(windows)]
impl Job {
    pub fn new() -> io::Result<Self> {
        unsafe {
            let handle = CreateJobObjectW(std::ptr::null(), std::ptr::null());
            if handle.is_null() {
                return Err(io::Error::last_os_error());
            }
            let job = Self(handle);
            let mut info: JOBOBJECT_EXTENDED_LIMIT_INFORMATION = std::mem::zeroed();
            info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
            if SetInformationJobObject(
                handle,
                JobObjectExtendedLimitInformation,
                &info as *const _ as _,
                std::mem::size_of_val(&info) as u32,
            ) == 0
            {
                return Err(io::Error::last_os_error());
            }
            Ok(job)
        }
    }
    pub fn spawn(&self, command: &mut Command) -> io::Result<Child> {
        use std::os::windows::{io::AsRawHandle, process::CommandExt};
        command.creation_flags(CREATE_NO_WINDOW | CREATE_SUSPENDED);
        let mut child = command.spawn()?;
        let result = unsafe {
            if AssignProcessToJobObject(self.0, child.as_raw_handle() as _) == 0 {
                Err(io::Error::last_os_error())
            } else {
                resume(child.id())
            }
        };
        if let Err(error) = result {
            let _ = child.kill();
            let _ = child.wait();
            return Err(error);
        }
        Ok(child)
    }
}
#[cfg(windows)]
impl Drop for Job {
    fn drop(&mut self) {
        unsafe {
            TerminateJobObject(self.0, 1);
            let deadline = std::time::Instant::now() + std::time::Duration::from_secs(5);
            loop {
                let mut info: JOBOBJECT_BASIC_ACCOUNTING_INFORMATION = std::mem::zeroed();
                if QueryInformationJobObject(
                    self.0,
                    JobObjectBasicAccountingInformation,
                    &mut info as *mut _ as _,
                    std::mem::size_of_val(&info) as u32,
                    std::ptr::null_mut(),
                ) == 0
                    || info.ActiveProcesses == 0
                    || std::time::Instant::now() >= deadline
                {
                    break;
                }
                std::thread::sleep(std::time::Duration::from_millis(20));
            }
            CloseHandle(self.0);
        }
    }
}

#[cfg(windows)]
unsafe fn resume(pid: u32) -> io::Result<()> {
    let snapshot = CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD, 0);
    if snapshot == INVALID_HANDLE_VALUE {
        return Err(io::Error::last_os_error());
    }
    let mut entry: THREADENTRY32 = std::mem::zeroed();
    entry.dwSize = std::mem::size_of_val(&entry) as u32;
    let mut found = Thread32First(snapshot, &mut entry);
    let mut result = Err(io::Error::new(io::ErrorKind::NotFound, "无法找到启动线程"));
    while found != 0 {
        if entry.th32OwnerProcessID == pid {
            let thread = OpenThread(THREAD_SUSPEND_RESUME, 0, entry.th32ThreadID);
            if !thread.is_null() {
                result = if ResumeThread(thread) == u32::MAX {
                    Err(io::Error::last_os_error())
                } else {
                    Ok(())
                };
                CloseHandle(thread);
            }
            break;
        }
        found = Thread32Next(snapshot, &mut entry);
    }
    CloseHandle(snapshot);
    result
}

#[cfg(not(windows))]
pub struct Job;
#[cfg(not(windows))]
impl Job {
    pub fn new() -> io::Result<Self> {
        Ok(Self)
    }
    pub fn spawn(&self, c: &mut Command) -> io::Result<Child> {
        c.spawn()
    }
}

#[cfg(all(test, windows))]
mod tests {
    use super::*;
    use std::{
        io::{BufRead, BufReader},
        process::Stdio,
    };

    #[test]
    fn job_cleans_descendant_after_parent_has_exited() {
        let job = Job::new().unwrap();
        let mut command = Command::new("node.exe");
        command.args(["-e", "const c=require('node:child_process').spawn(process.execPath,['-e','setTimeout(()=>{},90000)'],{stdio:'ignore',windowsHide:true});console.log(c.pid);c.unref()"])
            .stdout(Stdio::piped()).stderr(Stdio::null());
        let mut parent = job.spawn(&mut command).unwrap();
        let mut line = String::new();
        BufReader::new(parent.stdout.take().unwrap())
            .read_line(&mut line)
            .unwrap();
        let pid: u32 = line.trim().parse().unwrap();
        unsafe {
            let child = OpenProcess(PROCESS_SYNCHRONIZE, 0, pid);
            assert!(!child.is_null(), "descendant must be alive before cleanup");
            assert!(parent.wait().unwrap().success());
            drop(job);
            let wait = WaitForSingleObject(child, 5000);
            CloseHandle(child);
            assert_eq!(wait, WAIT_OBJECT_0, "job must clean orphaned descendants");
        }
    }
}
