use std::{ffi::OsString, path::Path};

// NSIS restarts through Explorer and does not preserve per-process environment
// variables. Forward selected paths as normal application arguments so an
// update keeps the same business store and custom startup configuration.
pub fn environment() -> Result<tauri::Env, String> {
    let mut environment = tauri::Env::default();
    let mut args = environment.args_os.iter().skip(1);
    while let Some(option) = args.next() {
        let variable = match option.to_str() {
            Some("--desktop-config") => "VAC_DESKTOP_CONFIG",
            Some("--data-dir") => "VAC_DATA_DIR",
            Some("--webview-data") => "WEBVIEW2_USER_DATA_FOLDER",
            Some("--webview-debug-port") => {
                let port = args
                    .next()
                    .and_then(|value| value.to_str())
                    .ok_or("调试端口缺失")?
                    .parse::<u16>()
                    .map_err(|_| "调试端口无效")?;
                if port < 1024 {
                    return Err("调试端口必须大于 1023".into());
                }
                std::env::set_var(
                    "WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
                    format!("--remote-debugging-port={port} --remote-debugging-address=127.0.0.1"),
                );
                continue;
            }
            _ => return Err(format!("未知参数：{}", option.to_string_lossy())),
        };
        let value = args.next().ok_or("启动路径参数缺失")?;
        if !Path::new(value).is_absolute() {
            return Err("启动路径参数必须为绝对路径".into());
        }
        if variable == "VAC_DATA_DIR" {
            std::env::remove_var("VAC_DESKTOP_CONFIG");
        }
        if variable == "VAC_DESKTOP_CONFIG" {
            std::env::remove_var("VAC_DATA_DIR");
        }
        std::env::set_var(variable, value);
    }
    let executable = environment.args_os.first().cloned().unwrap_or_default();
    environment.args_os = vec![executable];
    let mut forward = |flag: &str, variable: &str| {
        if let Some(value) = std::env::var_os(variable) {
            environment.args_os.extend([OsString::from(flag), value]);
        }
    };
    if std::env::var_os("VAC_DESKTOP_CONFIG").is_some() {
        forward("--desktop-config", "VAC_DESKTOP_CONFIG");
    } else {
        forward("--data-dir", "VAC_DATA_DIR");
    }
    forward("--webview-data", "WEBVIEW2_USER_DATA_FOLDER");
    if let Ok(options) = std::env::var("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS") {
        if let Some(port) = options.split_whitespace().find_map(|value| {
            value
                .strip_prefix("--remote-debugging-port=")
                .and_then(|value| value.parse::<u16>().ok())
        }) {
            if port >= 1024 {
                environment.args_os.extend([
                    OsString::from("--webview-debug-port"),
                    OsString::from(port.to_string()),
                ]);
            }
        }
    }
    Ok(environment)
}
