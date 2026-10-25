use crate::{
    config::{plain_path, Config},
    runtime::{client, Request, Runtime, Status},
};
use std::{
    fs::{self, OpenOptions},
    io,
    sync::atomic::Ordering,
    time::Duration,
};
use tauri::{
    menu::{Menu, MenuItem},
    tray::{MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Emitter, Manager, WindowEvent,
};
use tauri_plugin_dialog::DialogExt;
use tauri_plugin_opener::OpenerExt;

struct ProjectOpenRequest(std::sync::Mutex<Option<String>>);

fn project_argument(args: &[String], cwd: &str) -> Option<String> {
    args.iter()
        .skip(1)
        .find(|arg| !arg.starts_with('-') && arg.to_lowercase().ends_with(".avp"))
        .map(|arg| {
            let path = std::path::PathBuf::from(arg);
            if path.is_absolute() {
                path
            } else {
                std::path::Path::new(cwd).join(path)
            }
            .display()
            .to_string()
        })
}

#[tauri::command]
fn project_open_request(pending: tauri::State<'_, ProjectOpenRequest>) -> Option<String> {
    pending.0.lock().ok()?.take()
}

#[tauri::command]
fn clipboard_read_text() -> Result<String, String> {
    crate::platform::clipboard_text()
}

#[tauri::command]
fn clipboard_write_text(window: tauri::WebviewWindow, text: String) -> Result<(), String> {
    #[cfg(windows)]
    let owner = window.hwnd().map_err(|error| error.to_string())?.0 as isize;
    #[cfg(not(windows))]
    let owner = {
        let _ = window;
        0
    };
    crate::platform::write_clipboard_text(owner, &text)
}

#[tauri::command]
fn set_window_title(window: tauri::WebviewWindow, title: String) -> Result<(), String> {
    window.set_title(&title).map_err(|error| error.to_string())
}

#[tauri::command]
fn confirm_project_exit(runtime: tauri::State<'_, Runtime>) -> Result<(), String> {
    if runtime.maintenance.load(Ordering::SeqCst) {
        return Err("正在安装更新".into());
    }
    runtime
        .sender
        .send(Request::Exit)
        .map_err(|error| error.to_string())
}

#[tauri::command]
async fn project_dialog(
    app: tauri::AppHandle,
    mode: String,
    default_path: Option<String>,
) -> Result<Vec<String>, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let mut dialog = app.dialog().file();
        let save = matches!(
            mode.as_str(),
            "new-project" | "save-project" | "save-script" | "save-video"
        );
        dialog = match mode.as_str() {
            "new-project" | "save-project" => dialog
                .set_title("保存 VAC 工程")
                .add_filter("VAC 工程", &["avp"]),
            "open-project" => dialog
                .set_title("打开 VAC 工程")
                .add_filter("VAC 工程", &["avp"]),
            "save-script" => dialog
                .set_title("导出脚本")
                .add_filter("VAC 脚本", &["avs"]),
            "save-video" => dialog
                .set_title("导出当前序列轨道范围")
                .add_filter("MP4 视频", &["mp4"]),
            "transcription-model" => dialog.set_title("选择本地转录模型目录"),
            "transcription-python" => dialog
                .set_title("选择本地转录 Python")
                .add_filter("Python", &["exe"]),
            "import-resources" | "relink-resource" => dialog.set_title("选择工程资源").add_filter(
                "所有支持的资源",
                &[
                    "mp4", "mkv", "mov", "avi", "webm", "m4v", "mpeg", "mpg", "ts", "wav", "mp3",
                    "m4a", "aac", "flac", "ogg", "opus", "wma", "png", "jpg", "jpeg", "webp",
                    "gif", "bmp", "tif", "tiff", "srt", "ass", "vtt", "txt", "md", "markdown",
                    "avs", "flv", "jpe", "jepg", "dng", "arw",
                ],
            ),
            _ => return Err("未知文件操作".into()),
        };
        if let Some(path) = default_path {
            let path = std::path::PathBuf::from(path);
            if mode == "transcription-model" && path.is_dir() {
                dialog = dialog.set_directory(&path);
            } else if let Some(parent) = path.parent().filter(|p| p.is_dir()) {
                dialog = dialog.set_directory(parent);
            }
            if let Some(name) = path.file_name() {
                dialog = dialog.set_file_name(name.to_string_lossy());
            }
        }
        let files = if save {
            dialog.blocking_save_file().map(|file| vec![file])
        } else if mode == "transcription-model" {
            dialog.blocking_pick_folder().map(|file| vec![file])
        } else if mode == "import-resources" {
            dialog.blocking_pick_files()
        } else {
            dialog.blocking_pick_file().map(|file| vec![file])
        };
        files
            .unwrap_or_default()
            .into_iter()
            .map(|file| {
                let mut path = file.into_path().map_err(|error| error.to_string())?;
                if save && path.extension().is_none() {
                    path.set_extension(if mode == "save-script" { "avs" } else { "avp" });
                }
                let path = std::path::absolute(path).map_err(|error| error.to_string())?;
                Ok(path.display().to_string())
            })
            .collect()
    })
    .await
    .map_err(|error| error.to_string())?
}

#[tauri::command]
fn runtime_status(runtime: tauri::State<'_, Runtime>) -> Status {
    runtime.snapshot()
}

#[tauri::command]
fn restart_runtime(runtime: tauri::State<'_, Runtime>) -> Result<(), String> {
    if runtime.maintenance.load(Ordering::SeqCst) {
        return Err("正在安装更新，请稍候".into());
    }
    if !matches!(runtime.snapshot().status.as_str(), "fault" | "stopped") {
        return Err("后台正在运行或停止中".into());
    }
    runtime
        .sender
        .send(Request::Restart)
        .map_err(|e| e.to_string())
}

#[tauri::command]
fn stop_runtime(runtime: tauri::State<'_, Runtime>) -> Result<(), String> {
    if runtime.maintenance.load(Ordering::SeqCst) {
        return Err("正在安装更新，请稍候".into());
    }
    runtime
        .sender
        .send(Request::Stop)
        .map_err(|e| e.to_string())
}

#[tauri::command]
fn request_exit(app: tauri::AppHandle) {
    exit_prompt(app);
}

fn exit_prompt(app: tauri::AppHandle) {
    let runtime = app.state::<Runtime>();
    if runtime.maintenance.load(Ordering::SeqCst) {
        return;
    }
    if runtime.snapshot().status != "ready" {
        let _ = runtime.sender.send(Request::Exit);
    } else {
        let _ = app.emit("project-close-requested", ());
        show_window(&app);
    }
}

#[tauri::command]
fn open_data_directory(
    app: tauri::AppHandle,
    runtime: tauri::State<'_, Runtime>,
) -> Result<(), String> {
    plain_path(&runtime.config().data_directory)?;
    app.opener()
        .open_path(
            runtime.config().data_directory.display().to_string(),
            None::<&str>,
        )
        .map_err(|e| e.to_string())
}

#[tauri::command]
async fn select_recording(app: tauri::AppHandle) -> Result<Option<serde_json::Value>, String> {
    tauri::async_runtime::spawn_blocking(move || {
        let file = app.dialog().file().set_title("选择受控目录内的录像").add_filter("视频", &["mp4","mkv","mov","avi","webm"]).blocking_pick_file();
        let Some(file) = file else { return Ok(None); };
        let path = file.into_path().map_err(|e|e.to_string())?;
        plain_path(&path)?;
        let path = path.canonicalize().map_err(|e|e.to_string())?;
        let runtime = app.state::<Runtime>();
        for root in &runtime.config().ingest_roots {
            let root_path = root.path.canonicalize().map_err(|e|e.to_string())?;
            if let Ok(relative) = path.strip_prefix(root_path) {
                return Ok(Some(serde_json::json!({"root_id":root.root_id,"relative_path":relative.to_string_lossy().replace('\\',"/")})));
            }
        }
        Err("所选文件不在已配置的录像根目录内".into())
    }).await.map_err(|e|e.to_string())?
}

#[tauri::command]
async fn download_file(app: tauri::AppHandle, url: String) -> Result<(), String> {
    tauri::async_runtime::spawn_blocking(move || {
        let runtime = app.state::<Runtime>();
        let base = runtime.snapshot().api_base_url.ok_or("后台尚未就绪")?;
        let parsed = reqwest::Url::parse(&url).map_err(|e| e.to_string())?;
        if parsed.origin().ascii_serialization() != base
            || !parsed.path().starts_with("/api/")
            || !parsed.username().is_empty()
            || parsed.password().is_some()
        {
            return Err("只能下载当前后台服务中的交付文件".into());
        }
        let mut response = client()?
            .get(&url)
            .timeout(Duration::from_secs(300))
            .send()
            .map_err(|e| e.to_string())?
            .error_for_status()
            .map_err(|e| e.to_string())?;
        let extension = match response
            .headers()
            .get("content-type")
            .and_then(|v| v.to_str().ok())
            .unwrap_or("")
            .split(';')
            .next()
            .unwrap_or("")
        {
            "application/zip" => "zip",
            "video/mp4" => "mp4",
            "audio/wav" | "audio/x-wav" => "wav",
            _ => "bin",
        };
        let target = app
            .dialog()
            .file()
            .set_title("保存交付文件")
            .set_file_name(format!("video-auto-cut.{extension}"))
            .blocking_save_file();
        if let Some(target) = target {
            let path = target.into_path().map_err(|e| e.to_string())?;
            plain_path(&path)?;
            let temporary = path.with_file_name(format!(
                ".vac-download-{}.part",
                uuid::Uuid::new_v4().simple()
            ));
            let result = (|| {
                let mut file = OpenOptions::new()
                    .write(true)
                    .create_new(true)
                    .open(&temporary)
                    .map_err(|e| e.to_string())?;
                io::copy(&mut response, &mut file).map_err(|e| e.to_string())?;
                file.sync_all().map_err(|e| e.to_string())?;
                drop(file);
                fs::rename(&temporary, &path).map_err(|e| e.to_string())
            })();
            if result.is_err() {
                let _ = fs::remove_file(&temporary);
            }
            result?;
        }
        Ok(())
    })
    .await
    .map_err(|e| e.to_string())?
}

pub fn run(environment: tauri::Env) {
    let args: Vec<String> = std::env::args().collect();
    let cwd = std::env::current_dir()
        .unwrap_or_default()
        .display()
        .to_string();
    tauri::Builder::default()
        .manage(environment)
        .manage(ProjectOpenRequest(std::sync::Mutex::new(project_argument(
            &args, &cwd,
        ))))
        .plugin(tauri_plugin_single_instance::init(|app, args, cwd| {
            if let Some(path) = project_argument(&args, &cwd) {
                if let Some(pending) = app.try_state::<ProjectOpenRequest>() {
                    if let Ok(mut value) = pending.0.lock() {
                        *value = Some(path);
                    }
                }
                let _ = app.emit("project-open-requested", ());
            }
            show_window(app);
        }))
        .plugin(tauri_plugin_updater::Builder::new().build())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(
            tauri_plugin_window_state::Builder::default()
                .with_state_flags(
                    tauri_plugin_window_state::StateFlags::SIZE
                        | tauri_plugin_window_state::StateFlags::POSITION
                        | tauri_plugin_window_state::StateFlags::MAXIMIZED,
                )
                .build(),
        )
        .invoke_handler(tauri::generate_handler![
            runtime_status,
            restart_runtime,
            stop_runtime,
            request_exit,
            confirm_project_exit,
            project_dialog,
            project_open_request,
            clipboard_read_text,
            clipboard_write_text,
            set_window_title,
            open_data_directory,
            select_recording,
            download_file,
            crate::updater::update_status,
            crate::updater::check_update,
            crate::updater::download_update,
            crate::updater::install_update
        ])
        .setup(|app| {
            #[cfg(windows)]
            if let Some(window) = app.get_webview_window("main") {
                let owner = window.hwnd()?.0 as isize;
                if let Err(error) = crate::platform::set_caption_colors(owner) {
                    // Older Windows versions retain the native dark title bar.
                    eprintln!("{error}");
                }
            }
            app.manage(crate::updater::Updates::new(
                app.package_info().version.to_string(),
            ));
            let resources = app.path().resource_dir()?.join("runtime");
            let binary = app.path().resource_dir()?.join("control-plane.exe");
            let config = Config::defaults(
                app.path().app_local_data_dir()?.join("data"),
                resources,
                binary,
            );
            app.manage(Runtime::create(config, app.handle().clone()));
            let show = MenuItem::with_id(app, "show", "显示主窗口", true, None::<&str>)?;
            let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&show, &quit])?;
            let mut builder = TrayIconBuilder::with_id("main-tray")
                .menu(&menu)
                .tooltip("Video Auto Cut · 工程工作区")
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "show" => show_window(app),
                    "quit" => exit_prompt(app.clone()),
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if matches!(
                        event,
                        TrayIconEvent::Click {
                            button_state: MouseButtonState::Up,
                            ..
                        }
                    ) {
                        show_window(tray.app_handle());
                    }
                });
            if let Some(icon) = app.default_window_icon() {
                builder = builder.icon(icon.clone());
            }
            builder.build(app)?;
            Ok(())
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                exit_prompt(window.app_handle().clone());
            }
        })
        .run(tauri::generate_context!())
        .expect("Tauri desktop runtime failed");
}

fn show_window(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}
