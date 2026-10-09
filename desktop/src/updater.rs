use crate::runtime::{client, Runtime};
use serde::Serialize;
use std::{
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc, Mutex,
    },
    time::{Duration, Instant},
};
use tauri::{AppHandle, Emitter, Manager};
use tauri_plugin_dialog::{DialogExt, MessageDialogButtons};
use tauri_plugin_updater::{Update, UpdaterExt};

#[derive(Clone, Serialize)]
pub struct UpdateStatus {
    revision: u64,
    phase: String,
    current_version: String,
    available_version: Option<String>,
    notes: Option<String>,
    downloaded_bytes: u64,
    total_bytes: Option<u64>,
    ready_to_install: bool,
    message: String,
}

pub struct Updates {
    status: Mutex<UpdateStatus>,
    available: Mutex<Option<Update>>,
    payload: Mutex<Option<Vec<u8>>>,
    operation: Arc<AtomicBool>,
}
impl Updates {
    pub fn new(version: String) -> Self {
        Self {
            status: Mutex::new(UpdateStatus {
                revision: 0,
                phase: "idle".into(),
                current_version: version,
                available_version: None,
                notes: None,
                downloaded_bytes: 0,
                total_bytes: None,
                ready_to_install: false,
                message: "可手动检查更新".into(),
            }),
            available: Mutex::new(None),
            payload: Mutex::new(None),
            operation: Arc::new(false.into()),
        }
    }
    fn snapshot(&self) -> UpdateStatus {
        self.status
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .clone()
    }
    fn publish(&self, app: &AppHandle, phase: &str, message: &str) {
        self.change(app, |state| {
            state.phase = phase.into();
            state.message = message.into();
        });
    }
    fn change(&self, app: &AppHandle, change: impl FnOnce(&mut UpdateStatus)) {
        let mut status = self.status.lock().unwrap_or_else(|e| e.into_inner());
        change(&mut status);
        status.revision += 1;
        let _ = app.emit("update-status", status.clone());
    }
    fn begin(&self) -> Result<Operation, String> {
        Operation::acquire(self.operation.clone(), "已有更新操作正在进行")
    }
}

// Each operation owns its gate until completion, including every error/cancel path.
struct Operation(Arc<AtomicBool>);
impl Operation {
    fn acquire(flag: Arc<AtomicBool>, message: &str) -> Result<Self, String> {
        flag.compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
            .map_err(|_| message.to_string())?;
        Ok(Self(flag))
    }
}
impl Drop for Operation {
    fn drop(&mut self) {
        self.0.store(false, Ordering::SeqCst);
    }
}

#[tauri::command]
pub fn update_status(updates: tauri::State<'_, Updates>) -> UpdateStatus {
    updates.snapshot()
}

fn builder(app: &AppHandle) -> Result<tauri_plugin_updater::UpdaterBuilder, String> {
    let mut builder = app.updater_builder().timeout(Duration::from_secs(20));
    #[cfg(not(debug_assertions))]
    {
        builder = builder.configure_client(|client| client.https_only(true));
    }
    // A bounded loopback feed is available only in debug builds for real updater
    // acceptance. Production cannot override its HTTPS feed or embedded public key.
    #[cfg(debug_assertions)]
    if let Ok(endpoint) = std::env::var("VAC_UPDATE_TEST_ENDPOINT") {
        let url = reqwest::Url::parse(&endpoint).map_err(|e| e.to_string())?;
        if url.scheme() != "http" || url.host_str() != Some("127.0.0.1") || url.port().is_none() {
            return Err("调试更新源必须为有端口的 127.0.0.1 HTTP 地址".into());
        }
        builder = builder.endpoints(vec![url]).map_err(|e| e.to_string())?;
    }
    // NSIS accepts /D as the last argument, including unquoted spaces. Update
    // exactly the running app directory, including the current portable delivery.
    let executable = std::env::current_exe().map_err(|e| e.to_string())?;
    let parent = executable
        .parent()
        .ok_or("无法定位程序目录")?
        .to_string_lossy();
    let parent = parent.strip_prefix(r"\\?\").unwrap_or(&parent);
    builder = builder.installer_arg(format!("/D={parent}"));
    Ok(builder)
}

fn trusted_download(url: &reqwest::Url) -> bool {
    if url.scheme() == "https" && url.username().is_empty() && url.password().is_none() {
        return true;
    }
    #[cfg(debug_assertions)]
    if std::env::var_os("VAC_UPDATE_TEST_ENDPOINT").is_some()
        && url.scheme() == "http"
        && url.host_str() == Some("127.0.0.1")
        && url.port().is_some()
        && url.username().is_empty()
        && url.password().is_none()
    {
        return true;
    }
    false
}

#[tauri::command]
pub async fn check_update(app: AppHandle) -> Result<(), String> {
    let updates = app.state::<Updates>();
    let _operation = updates.begin()?;
    *updates.available.lock().unwrap_or_else(|e| e.into_inner()) = None;
    *updates.payload.lock().unwrap_or_else(|e| e.into_inner()) = None;
    updates.change(&app, |state| {
        state.available_version = None;
        state.notes = None;
        state.ready_to_install = false;
        state.downloaded_bytes = 0;
        state.total_bytes = None;
        state.phase = "checking".into();
        state.message = "正在检查更新…".into();
    });
    let result = async {
        let update = builder(&app)?
            .build()
            .map_err(|e| e.to_string())?
            .check()
            .await
            .map_err(|e| e.to_string())?;
        if let Some(update) = update {
            if !trusted_download(&update.download_url) {
                return Err("更新包地址必须使用 HTTPS".into());
            }
            *updates.available.lock().unwrap_or_else(|e| e.into_inner()) = Some(update.clone());
            updates.change(&app, |state| {
                state.available_version = Some(update.version.clone());
                state.notes = update.body.clone();
                state.phase = "available".into();
                state.message = "发现新版本，可先下载更新".into();
            });
        } else {
            updates.publish(&app, "up_to_date", "当前已是最新版本");
        }
        Ok(())
    }
    .await;
    if let Err(error) = &result {
        updates.publish(&app, "error", &format!("检查更新失败：{error}"));
    }
    result
}

#[tauri::command]
pub async fn download_update(app: AppHandle) -> Result<(), String> {
    let updates = app.state::<Updates>();
    let _operation = updates.begin()?;
    let update = updates
        .available
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .clone()
        .ok_or("请先检查更新")?;
    // Download time is separate from the short manifest request budget.
    let mut update = update;
    update.timeout = Some(Duration::from_secs(600));
    *updates.payload.lock().unwrap_or_else(|e| e.into_inner()) = None;
    updates.change(&app, |state| {
        state.phase = "downloading".into();
        state.message = "正在下载并验证更新包…".into();
        state.downloaded_bytes = 0;
        state.total_bytes = None;
        state.ready_to_install = false;
    });
    let mut downloaded = 0u64;
    let mut last_progress = Instant::now();
    let result = update
        .download(
            |bytes, total| {
                downloaded = downloaded.saturating_add(bytes as u64);
                if last_progress.elapsed() < Duration::from_millis(100) && total != Some(downloaded)
                {
                    return;
                }
                last_progress = Instant::now();
                updates.change(&app, |state| {
                    state.downloaded_bytes = downloaded;
                    state.total_bytes = total;
                });
            },
            || {},
        )
        .await;
    match result {
        Ok(bytes) => {
            let length = bytes.len() as u64;
            *updates.payload.lock().unwrap_or_else(|e| e.into_inner()) = Some(bytes);
            updates.change(&app, |state| {
                state.phase = "downloaded".into();
                state.downloaded_bytes = length;
                state.ready_to_install = true;
                state.message = "更新包签名已验证，安装后将重启程序".into();
            });
            Ok(())
        }
        Err(error) => {
            let message = format!("下载或签名验证失败：{error}");
            updates.publish(&app, "error", &message);
            Err(message)
        }
    }
}

#[tauri::command]
pub async fn install_update(app: AppHandle) -> Result<bool, String> {
    let updates = app.state::<Updates>();
    let _operation = updates.begin()?;
    let result = install_inner(&app, &updates).await;
    if let Err(error) = &result {
        updates.publish(&app, "error", error);
    }
    result
}

async fn install_inner(app: &AppHandle, updates: &Updates) -> Result<bool, String> {
    let update = updates
        .available
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .clone()
        .ok_or("请先检查更新")?;
    let bytes = updates
        .payload
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .clone()
        .ok_or("请先下载并验证更新包")?;
    let runtime = app.state::<Runtime>();
    let _maintenance = Operation::acquire(runtime.maintenance.clone(), "后台维护正在进行")?;
    let _exit = Operation::acquire(
        runtime.exit_pending.clone(),
        "正在确认退出，请先关闭退出确认框",
    )?;
    if matches!(runtime.snapshot().status.as_str(), "starting" | "stopping") {
        return Err("请等待后台启动或停止完成，再安装更新".into());
    }
    updates.publish(&app, "awaiting_confirmation", "等待确认安装更新…");
    let prompt_app = app.clone();
    let current = runtime.snapshot();
    let yes = tauri::async_runtime::spawn_blocking(move || {
        let activity = current
            .api_base_url
            .as_ref()
            .and_then(|base| {
                client()
                    .ok()?
                    .get(format!("{base}/api/runtime/activity"))
                    .send()
                    .ok()?
                    .json::<serde_json::Value>()
                    .ok()
            })
            .and_then(|value| value["active_tasks"].as_u64());
        let detail = match activity {
            Some(0) => "安装更新将短暂停止后台并重启程序；已有业务数据会保留。".to_string(),
            Some(count) => format!(
                "当前有 {count} 项后台任务。安装更新将停止这些任务并重启程序；已完成结果会保留。"
            ),
            None => "无法确认后台任务状态。安装更新将停止后台并重启程序；已有业务数据会保留。"
                .to_string(),
        };
        prompt_app
            .dialog()
            .message(detail)
            .title("安装 Video Auto Cut 更新")
            .buttons(MessageDialogButtons::OkCancelCustom(
                "安装并重启".into(),
                "稍后安装".into(),
            ))
            .blocking_show()
    })
    .await
    .map_err(|e| e.to_string())?;
    if !yes {
        updates.publish(&app, "downloaded", "已取消安装，可稍后继续");
        return Ok(false);
    }
    updates.publish(&app, "stopping", "正在停止本次后台进程…");
    let stop_app = app.clone();
    let stopped =
        tauri::async_runtime::spawn_blocking(move || stop_app.state::<Runtime>().stop_for_update())
            .await
            .map_err(|e| e.to_string())?;
    if let Err(error) = stopped {
        updates.publish(&app, "error", &error);
        return Err(error);
    }
    updates.publish(&app, "installing", "后台已停止，正在安装更新并重启…");
    // On Windows successful install() launches NSIS and exits the app. The
    // plugin's cleanup hook saves window state; all business processes are gone.
    let installed = tauri::async_runtime::spawn_blocking(move || update.install(&bytes))
        .await
        .map_err(|e| e.to_string())?;
    if let Err(error) = installed {
        let message = format!("安装更新失败：{error}；后台已停止，可重新启动后台或重试安装");
        updates.publish(&app, "error", &message);
        return Err(message);
    }
    Ok(true)
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn update_gate_is_released_after_cancel_or_error() {
        let flag = Arc::new(AtomicBool::new(false));
        let operation = Operation::acquire(flag.clone(), "busy").unwrap();
        assert!(Operation::acquire(flag.clone(), "busy").is_err());
        drop(operation);
        assert!(Operation::acquire(flag, "busy").is_ok());
    }
    #[test]
    fn production_feed_requires_https_without_url_credentials() {
        assert!(trusted_download(
            &reqwest::Url::parse("https://github.com/example/update.exe").unwrap()
        ));
        assert!(!trusted_download(
            &reqwest::Url::parse("http://example.com/update.exe").unwrap()
        ));
        assert!(!trusted_download(
            &reqwest::Url::parse("https://user:password@example.com/update.exe").unwrap()
        ));
    }
}
