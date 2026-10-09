use std::{
    io,
    process::{Child, Command},
};
#[cfg(windows)]
use windows_sys::Win32::{
    Foundation::*,
    System::{Diagnostics::ToolHelp::*, JobObjects::*, Threading::*},
    UI::WindowsAndMessaging::*,
};

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

#[cfg(windows)]
pub struct Instance(HANDLE);
#[cfg(windows)]
impl Instance {
    pub fn acquire() -> io::Result<Option<Self>> {
        unsafe {
            let name: Vec<u16> = "Local\\VideoAutoCutDesktop\0".encode_utf16().collect();
            let handle = CreateMutexW(std::ptr::null(), 0, name.as_ptr());
            if handle.is_null() {
                return Err(io::Error::last_os_error());
            }
            if GetLastError() == ERROR_ALREADY_EXISTS {
                CloseHandle(handle);
                let title: Vec<u16> = "Video Auto Cut\0".encode_utf16().collect();
                let window = FindWindowW(std::ptr::null(), title.as_ptr());
                if !window.is_null() {
                    ShowWindow(window, SW_RESTORE);
                    SetForegroundWindow(window);
                }
                return Ok(None);
            }
            Ok(Some(Self(handle)))
        }
    }
}
#[cfg(windows)]
impl Drop for Instance {
    fn drop(&mut self) {
        unsafe {
            CloseHandle(self.0);
        }
    }
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
