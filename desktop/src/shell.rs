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
    Manager, WindowEvent,
};
use tauri_plugin_dialog::{DialogExt, MessageDialogButtons};
use tauri_plugin_opener::OpenerExt;

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
    if runtime.exit_pending.swap(true, Ordering::SeqCst) {
        return;
    }
    let snapshot = runtime.snapshot();
    let sender = runtime.sender.clone();
    let app_clone = app.clone();
    std::thread::spawn(move || {
        let active = snapshot
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
            .and_then(|v| v["active_tasks"].as_u64());
        if active != Some(0) && snapshot.status != "stopped" {
            app_clone
                .dialog()
                .message("退出将停止后台任务；已经完成的结果会保留。是否退出？")
                .title("退出 Video Auto Cut")
                .buttons(MessageDialogButtons::OkCancelCustom(
                    "退出并停止".into(),
                    "继续运行".into(),
                ))
                .show(move |yes| {
                    if yes {
                        let _ = sender.send(Request::Exit);
                    } else {
                        app_clone
                            .state::<Runtime>()
                            .exit_pending
                            .store(false, Ordering::SeqCst);
                    }
                });
        } else {
            let _ = sender.send(Request::Exit);
        }
    });
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
    tauri::Builder::default()
        .manage(environment)
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
            open_data_directory,
            select_recording,
            download_file,
            crate::updater::update_status,
            crate::updater::check_update,
            crate::updater::download_update,
            crate::updater::install_update
        ])
        .setup(|app| {
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
                .tooltip("Video Auto Cut · 关闭窗口后任务继续运行")
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
                let _ = window.hide();
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
