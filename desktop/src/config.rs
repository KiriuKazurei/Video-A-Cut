use serde::{Deserialize, Serialize};
use std::{
    fs,
    path::{Path, PathBuf},
};

#[derive(Clone, Deserialize, Serialize)]
pub struct RecordingRoot {
    pub root_id: String,
    pub name: String,
    pub path: PathBuf,
}

#[derive(Clone, Deserialize)]
pub struct Config {
    pub data_directory: PathBuf,
    pub resource_root: PathBuf,
    pub control_binary: PathBuf,
    #[serde(default = "default_python")]
    pub python: PathBuf,
    #[serde(default = "default_node")]
    pub node: PathBuf,
    #[serde(default = "default_ffmpeg")]
    pub ffmpeg: PathBuf,
    #[serde(default = "default_ffprobe")]
    pub ffprobe: PathBuf,
    #[serde(default)]
    pub ingest_roots: Vec<RecordingRoot>,
    #[serde(default)]
    pub profile_file: Option<PathBuf>,
    /// Optional browser hosting for the retained WebUI and same-machine comparison.
    #[serde(default)]
    pub web_root: Option<PathBuf>,
    #[serde(default = "default_origin")]
    pub desktop_origin: String,
}
fn default_python() -> PathBuf {
    "python".into()
}
fn default_node() -> PathBuf {
    "node".into()
}
fn default_ffmpeg() -> PathBuf {
    "ffmpeg".into()
}
fn default_ffprobe() -> PathBuf {
    "ffprobe".into()
}
fn default_origin() -> String {
    "http://tauri.localhost".into()
}

impl Config {
    pub fn load(data: PathBuf, resources: PathBuf, executable: PathBuf) -> Result<Self, String> {
        if let Some(path) = std::env::var_os("VAC_DESKTOP_CONFIG") {
            let path = PathBuf::from(path);
            plain_path(&path)?;
            return serde_json::from_slice(&fs::read(path).map_err(|e| e.to_string())?)
                .map_err(|e| e.to_string());
        }
        let data = std::env::var_os("VAC_DATA_DIR")
            .map(PathBuf::from)
            .unwrap_or(data);
        let config = data.join("desktop.json");
        plain_path(&config)?;
        if config.exists() {
            return serde_json::from_slice(&fs::read(config).map_err(|e| e.to_string())?)
                .map_err(|e| e.to_string());
        }
        Ok(Self::defaults(data, resources, executable))
    }

    pub fn defaults(data: PathBuf, resources: PathBuf, executable: PathBuf) -> Self {
        Self {
            data_directory: native_local_path(data),
            resource_root: native_local_path(resources),
            control_binary: native_local_path(executable),
            python: default_python(),
            node: default_node(),
            ffmpeg: default_ffmpeg(),
            ffprobe: default_ffprobe(),
            ingest_roots: vec![],
            profile_file: None,
            web_root: None,
            desktop_origin: default_origin(),
        }
    }

    pub fn profile(&self) -> PathBuf {
        self.profile_file
            .clone()
            .unwrap_or_else(|| self.resource_root.join("default-profile.json"))
    }

    pub fn validate(&self) -> Result<(), String> {
        if let Some(path) = &self.web_root {
            plain_path(path)?;
        }
        for path in [
            &self.data_directory,
            &self.resource_root,
            &self.control_binary,
            &self.profile(),
        ] {
            plain_path(path)?;
        }
        let data = std::path::absolute(&self.data_directory)
            .map_err(|e| e.to_string())?
            .to_string_lossy()
            .to_lowercase();
        let resources = std::path::absolute(&self.resource_root)
            .map_err(|e| e.to_string())?
            .to_string_lossy()
            .trim_end_matches(['\\', '/'])
            .to_lowercase();
        if data == resources || data.starts_with(&(resources + "\\")) {
            return Err("业务数据目录必须独立于程序资源目录".into());
        }
        if self.data_directory.starts_with(&self.resource_root) {
            return Err("业务数据目录必须独立于程序资源目录".into());
        }
        for root in &self.ingest_roots {
            plain_path(&root.path)?;
            if !root.path.is_dir() || root.root_id.is_empty() || root.name.is_empty() {
                return Err("录像根目录配置无效".into());
            }
        }
        if self.desktop_origin != "http://tauri.localhost"
            && self.desktop_origin != "http://127.0.0.1:5173"
        {
            return Err("桌面来源只能为生产窗口或本地 Vite 地址".into());
        }
        Ok(())
    }
}

// Tauri derives its resource directory from current_exe(), which Windows may
// return with a verbatim drive prefix. Normalize only local drive paths from
// these platform defaults; UNC/device paths remain subject to plain_path().
fn native_local_path(path: PathBuf) -> PathBuf {
    #[cfg(windows)]
    if let Some(local) = path.to_str().and_then(|value| value.strip_prefix(r"\\?\")) {
        let bytes = local.as_bytes();
        if bytes.len() >= 3
            && bytes[0].is_ascii_alphabetic()
            && bytes[1] == b':'
            && bytes[2] == b'\\'
        {
            return PathBuf::from(local);
        }
    }
    path
}

#[cfg(all(test, windows))]
mod tests {
    use super::*;
    #[test]
    fn data_cannot_enter_resources_through_parent_components() {
        let root = std::env::current_dir().unwrap();
        let config = Config::defaults(
            root.join("outside/../runtime/data"),
            root.join("runtime"),
            root.join("control-plane.exe"),
        );
        assert!(config
            .validate()
            .unwrap_err()
            .contains("独立于程序资源目录"));
    }

    #[test]
    fn tauri_verbatim_drive_defaults_are_local_but_unc_is_rejected() {
        let root = native_local_path(std::env::current_dir().unwrap());
        let config = Config::defaults(
            PathBuf::from(format!(r"\\?\{}", root.join("data").display())),
            PathBuf::from(format!(r"\\?\{}", root.join("runtime").display())),
            PathBuf::from(format!(r"\\?\{}", root.join("control-plane.exe").display())),
        );
        assert_eq!(config.resource_root, root.join("runtime"));
        config.validate().unwrap();
        assert!(plain_path(&native_local_path(PathBuf::from(r"\\?\UNC\host\share"))).is_err());
    }
}

pub fn plain_path(path: &Path) -> Result<(), String> {
    if !path.is_absolute() || path.to_string_lossy().starts_with("\\\\") {
        return Err("运行路径必须是本机绝对路径".into());
    }
    for parent in path.ancestors() {
        if let Ok(metadata) = fs::symlink_metadata(parent) {
            #[cfg(windows)]
            {
                use std::os::windows::fs::MetadataExt;
                if metadata.file_attributes() & 0x400 != 0 {
                    return Err("运行路径包含重解析点".into());
                }
            }
            if metadata.file_type().is_symlink() {
                return Err("运行路径包含符号链接".into());
            }
        }
    }
    Ok(())
}
