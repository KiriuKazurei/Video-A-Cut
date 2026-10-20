use crate::{
    config::{plain_path, Config},
    platform::Job,
};
use reqwest::blocking::Client;
use serde::Serialize;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use std::{
    collections::VecDeque,
    fs::{self, File, OpenOptions},
    io::{BufRead, BufReader, Read, Write},
    path::{Path, PathBuf},
    process::{Child, Command, Stdio},
    sync::{mpsc, Arc, Mutex},
    thread,
    time::{Duration, Instant},
};
use tauri::{AppHandle, Emitter};

#[derive(Clone, Serialize)]
pub struct Status {
    pub revision: u64,
    pub status: String,
    pub api_base_url: Option<String>,
    pub mcp_url: Option<String>,
    pub session_id: Option<String>,
    pub data_directory: String,
    pub message: String,
    pub workers: Vec<WorkerStatus>,
}
#[derive(Clone, Serialize)]
pub struct WorkerStatus {
    pub role: String,
    pub status: String,
}
pub enum Request {
    Restart,
    Stop,
    StopForUpdate(mpsc::Sender<Result<(), String>>),
    Exit,
}
pub struct Runtime {
    pub config: Arc<Mutex<Config>>,
    pub state: Arc<Mutex<Status>>,
    pub sender: mpsc::Sender<Request>,
    pub exit_pending: Arc<std::sync::atomic::AtomicBool>,
    pub maintenance: Arc<std::sync::atomic::AtomicBool>,
}
impl Runtime {
    pub fn create(config: Config, app: AppHandle) -> Self {
        let state = Arc::new(Mutex::new(Status {
            revision: 0,
            status: "starting".into(),
            api_base_url: None,
            mcp_url: None,
            session_id: None,
            data_directory: config.data_directory.display().to_string(),
            message: "正在检查本地运行环境…".into(),
            workers: vec![],
        }));
        let (sender, receiver) = mpsc::channel();
        let worker_state = state.clone();
        let config = Arc::new(Mutex::new(config));
        let worker_config = config.clone();
        thread::spawn(move || supervise(worker_config, worker_state, receiver, app));
        Self {
            config,
            state,
            sender,
            exit_pending: Arc::new(false.into()),
            maintenance: Arc::new(false.into()),
        }
    }
    pub fn config(&self) -> Config {
        self.config
            .lock()
            .unwrap_or_else(|e| e.into_inner())
            .clone()
    }
    pub fn snapshot(&self) -> Status {
        self.state.lock().unwrap_or_else(|e| e.into_inner()).clone()
    }
    pub fn stop_for_update(&self) -> Result<(), String> {
        let (reply, receive) = mpsc::channel();
        self.sender
            .send(Request::StopForUpdate(reply))
            .map_err(|e| e.to_string())?;
        receive
            .recv_timeout(Duration::from_secs(30))
            .map_err(|_| "后台未能在更新前完整停止；没有启动安装器".to_string())?
    }
}

fn publish(
    state: &Arc<Mutex<Status>>,
    app: &AppHandle,
    status: &str,
    message: &str,
    pipeline: Option<&Pipeline>,
) {
    let mut value = state.lock().unwrap_or_else(|e| e.into_inner());
    value.revision += 1;
    value.status = status.into();
    value.message = message.into();
    value.api_base_url = pipeline.map(|p| p.base.clone());
    value.mcp_url = pipeline.map(|p| format!("{}/mcp", p.base));
    value.session_id = pipeline.map(|p| p.session.clone());
    value.workers = pipeline
        .map(|p| {
            p.workers
                .iter()
                .map(|w| WorkerStatus {
                    role: w.role.clone(),
                    status: if w.blocked {
                        "blocked"
                    } else if w.pending.is_some() {
                        "restarting"
                    } else {
                        "online"
                    }
                    .into(),
                })
                .collect()
        })
        .unwrap_or_default();
    let _ = app.emit("runtime-status", value.clone());
}

fn supervise(
    config: Arc<Mutex<Config>>,
    state: Arc<Mutex<Status>>,
    receiver: mpsc::Receiver<Request>,
    app: AppHandle,
) {
    let defaults = config.lock().unwrap_or_else(|e| e.into_inner()).clone();
    let mut pipeline = None;
    let mut start = true;
    loop {
        if start {
            publish(&state, &app, "starting", "正在启动 Go 与媒体 Worker…", None);
            let result = Config::load(
                defaults.data_directory.clone(),
                defaults.resource_root.clone(),
                defaults.control_binary.clone(),
            )
            .and_then(|loaded| {
                *config.lock().unwrap_or_else(|e| e.into_inner()) = loaded.clone();
                state
                    .lock()
                    .unwrap_or_else(|e| e.into_inner())
                    .data_directory = loaded.data_directory.display().to_string();
                Pipeline::start(&loaded)
            });
            match result {
                Ok(value) => {
                    pipeline = Some(value);
                    publish(&state, &app, "ready", "后台服务已就绪", pipeline.as_ref());
                }
                Err(error) => publish(&state, &app, "fault", &error, None),
            }
            start = false;
        }
        match receiver.recv_timeout(Duration::from_millis(250)) {
            Ok(request) => {
                let acknowledgement = match &request {
                    Request::StopForUpdate(reply) => Some(reply.clone()),
                    _ => None,
                };
                publish(
                    &state,
                    &app,
                    "stopping",
                    "正在停止后台任务，请稍候…",
                    pipeline.as_ref(),
                );
                let stop_result = pipeline.take().map(|mut p| p.stop()).transpose();
                if let Some(reply) = acknowledgement {
                    // The Pipeline, including its Job Object and data lock, has
                    // already dropped before acknowledging a safe install.
                    let _ = reply.send(stop_result.as_ref().map(|_| ()).map_err(|e| e.to_string()));
                }
                if let Err(error) = stop_result {
                    publish(&state, &app, "fault", &error, None);
                    if matches!(request, Request::Exit) {
                        app.exit(1);
                        break;
                    }
                } else {
                    publish(&state, &app, "stopped", "后台服务已停止", None);
                    if matches!(request, Request::Exit) {
                        app.exit(0);
                        break;
                    }
                    start = matches!(request, Request::Restart);
                }
            }
            Err(mpsc::RecvTimeoutError::Disconnected) => break,
            Err(mpsc::RecvTimeoutError::Timeout) => {
                if let Some(active) = &mut pipeline {
                    match active.poll() {
                        Ok(changed) => {
                            if changed {
                                publish(
                                    &state,
                                    &app,
                                    "ready",
                                    "后台服务已就绪；请检查 Worker 状态",
                                    pipeline.as_ref(),
                                );
                            }
                        }
                        Err(error) => {
                            if let Some(mut p) = pipeline.take() {
                                let _ = p.stop();
                            }
                            publish(&state, &app, "fault", &error, None);
                        }
                    }
                }
            }
        }
    }
}

struct Worker {
    role: String,
    child: Child,
    executable: PathBuf,
    args: Vec<String>,
    cwd: PathBuf,
    token_name: String,
    token: String,
    recent: VecDeque<Instant>,
    blocked: bool,
    pending: Option<Instant>,
    stdout: PathBuf,
    stderr: PathBuf,
}
struct Pipeline {
    // Drop the job before releasing the database lock, including startup failures.
    job: Job,
    control: Option<Child>,
    workers: Vec<Worker>,
    lock: File,
    session: String,
    directory: PathBuf,
    data: PathBuf,
    base: String,
    path: std::ffi::OsString,
}
impl Drop for Pipeline {
    fn drop(&mut self) {
        if let Some(child) = &mut self.control {
            let _ = child.kill();
            let _ = child.wait();
        }
        for worker in &mut self.workers {
            let _ = worker.child.kill();
            let _ = worker.child.wait();
        }
        // The Job field closes next and kills every remaining descendant.
        let _ = self.lock.sync_all();
        if fs::read(self.data.join("desktop-session.json"))
            .ok()
            .and_then(|b| serde_json::from_slice::<Value>(&b).ok())
            .is_some_and(|v| v["session_id"] == self.session && v["status"] != "stopped")
        {
            let _ = self.journal("fault");
        }
    }
}

impl Pipeline {
    fn start(config: &Config) -> Result<Self, String> {
        config.validate()?;
        fs::create_dir_all(&config.data_directory).map_err(|e| e.to_string())?;
        let lock = OpenOptions::new()
            .create(true)
            .truncate(false)
            .read(true)
            .write(true)
            .open(config.data_directory.join("runtime.lock"))
            .map_err(|e| e.to_string())?;
        lock.try_lock()
            .map_err(|_| "业务数据目录已被其他程序使用，请先退出原程序".to_string())?;
        let session = Uuid::new_v4().simple().to_string();
        let directory = config.data_directory.join("sessions").join(&session);
        plain_path(&directory)?;
        fs::create_dir_all(&directory).map_err(|e| e.to_string())?;
        fs::create_dir_all(config.data_directory.join("delivery")).map_err(|e| e.to_string())?;
        let mut paths: Vec<PathBuf> = [&config.ffmpeg, &config.ffprobe]
            .into_iter()
            .filter_map(|p| {
                p.parent()
                    .filter(|p| !p.as_os_str().is_empty())
                    .map(Path::to_path_buf)
            })
            .collect();
        paths.extend(std::env::split_paths(
            &std::env::var_os("PATH").unwrap_or_default(),
        ));
        let path = std::env::join_paths(paths).map_err(|e| e.to_string())?;
        let mut p = Self {
            job: Job::new().map_err(|e| e.to_string())?,
            control: None,
            workers: vec![],
            lock,
            session,
            directory,
            data: config.data_directory.clone(),
            base: String::new(),
            path,
        };
        p.journal("starting")?;
        p.check(config)?;

        let manifest_path =
            PathBuf::from(format!("{}.runtime.json", config.control_binary.display()));
        let manifest: Value = read_json(&manifest_path)?;
        if manifest["schema_version"] != 1 {
            return Err("Go 程序清单格式不兼容".into());
        }
        let mut binary = File::open(&config.control_binary).map_err(|e| e.to_string())?;
        let mut hash = Sha256::new();
        let mut chunk = [0u8; 65536];
        loop {
            let count = binary.read(&mut chunk).map_err(|e| e.to_string())?;
            if count == 0 {
                break;
            }
            hash.update(&chunk[..count]);
        }
        if format!("{:x}", hash.finalize()) != manifest["binary_sha256"].as_str().unwrap_or("") {
            return Err("Go 程序与版本清单不匹配，请重新构建".into());
        }
        let schemas = manifest["schema_versions"]
            .as_array()
            .ok_or("版本清单缺少数据库迁移信息")?
            .iter()
            .map(Value::to_string)
            .collect::<Vec<_>>()
            .join(",");
        let database = p.data.join("control.db");
        let preflight = p.output(
            Command::new(&config.python)
                .arg(config.resource_root.join("scripts/runtime_state.py"))
                .args([
                    "--data-directory",
                    &p.data.display().to_string(),
                    "--database",
                    &database.display().to_string(),
                    "--supported-schemas",
                    &schemas,
                    "--backup",
                    &p.directory.join("pre-start.db").display().to_string(),
                ]),
        )?;
        fs::write(p.directory.join("database-preflight.json"), preflight)
            .map_err(|e| e.to_string())?;

        let roles = if config.ingest_roots.is_empty() {
            vec!["recognizer", "narrator", "exporter"]
        } else {
            vec!["recognizer", "narrator", "exporter", "ingester"]
        };
        let tokens: Vec<_> = roles
            .iter()
            .map(|role| {
                let token = format!("{}{}", Uuid::new_v4().simple(), Uuid::new_v4().simple());
                (
                    *role,
                    format!("VAC_LOCAL_{}_TOKEN", role.to_uppercase()),
                    token,
                )
            })
            .collect();
        let agents: Vec<_> = tokens.iter().map(|(role, _, token)| json!({"agent_id":role,"role":role,"token_sha256":format!("{:x}",Sha256::digest(token.as_bytes()))})).collect();
        write_json(&p.directory.join("agents.json"), &json!({"agents":agents}))?;
        let control_config = json!({"http_addr":"127.0.0.1:0", "desktop_origin":config.desktop_origin,
            "delivery_root":p.data.join("delivery"), "mcp_agents_file":p.directory.join("agents.json"), "mcp_local_access":true,
            "web_root":config.web_root.as_ref().map(|p|p.display().to_string()).unwrap_or_default(),
            "lease_seconds":60, "tts_python":config.python, "tts_worker_dir":config.resource_root.join("workers/python"), "ingest_roots":config.ingest_roots});
        write_json(&p.directory.join("control.json"), &control_config)?;
        let mut command = Command::new(&config.control_binary);
        command
            .arg("-desktop")
            .arg("-config")
            .arg(p.directory.join("control.json"))
            .arg("-db")
            .arg(&database)
            .env("PATH", &p.path)
            .env("VAC_SESSION_STOP", p.directory.join("session.stop"))
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::from(log_file(&p.directory.join("control.err"))?));
        let mut child = p
            .job
            .spawn(&mut command)
            .map_err(|e| format!("Go 启动失败：{e}"))?;
        let stdout = child.stdout.take().ok_or("无法读取 Go 启动信息")?;
        p.control = Some(child);
        let (send, receive) = mpsc::channel();
        let stdout_log = p.directory.join("control.out");
        thread::spawn(move || {
            let mut log = log_file(&stdout_log).ok();
            for line in BufReader::new(stdout).lines() {
                match line {
                    Ok(line) => {
                        if let Some(file) = &mut log {
                            let _ = writeln!(file, "{line}");
                        }
                        if let Ok(value) = serde_json::from_str::<Value>(&line) {
                            if value["event"] == "ready" {
                                let _ = send.send(value);
                            }
                        }
                    }
                    Err(_) => break,
                }
            }
        });
        let ready = receive
            .recv_timeout(Duration::from_secs(30))
            .map_err(|_| "Go 启动失败或超时，请查看本次 control.err".to_string())?;
        if ready["protocol"] != 1 {
            return Err("Go 桌面协议版本不兼容".into());
        }
        p.base = ready["api_base_url"].as_str().ok_or("缺少后台地址")?.into();
        let url = reqwest::Url::parse(&p.base).map_err(|e| e.to_string())?;
        if url.scheme() != "http" || url.host_str() != Some("127.0.0.1") || url.port().is_none() {
            return Err("后台地址无效".into());
        }
        let client = client()?;
        let deadline = Instant::now() + Duration::from_secs(10);
        loop {
            if client
                .get(format!("{}/api/runtime/activity", p.base))
                .timeout(Duration::from_millis(500))
                .send()
                .is_ok_and(|r| r.status().is_success())
            {
                break;
            }
            if Instant::now() > deadline {
                return Err("后台服务未能就绪".into());
            }
            thread::sleep(Duration::from_millis(100));
        }
        let profile: Value = read_json(&config.profile())?;
        let id = profile["profile_id"].as_str().ok_or("处理预设缺少 ID")?;
        if !id
            .bytes()
            .all(|c| c.is_ascii_alphanumeric() || b"._-".contains(&c))
        {
            return Err("处理预设 ID 无效".into());
        }
        let revision = profile["revision"]
            .as_u64()
            .filter(|r| *r > 0)
            .ok_or("处理预设版本无效")?;
        let profile_url = format!(
            "{}/api/processing-profiles/{}/revisions/{}",
            p.base, id, revision
        );
        let response = client.get(&profile_url).send().map_err(|e| e.to_string())?;
        if response.status() == 404 && revision == 1 {
            client.post(format!("{}/api/processing-profiles", p.base)).json(&json!({"profile":profile,"expected_revision":0,"idempotency_key":format!("desktop-bootstrap-{id}")}))
                .send().map_err(|e| e.to_string())?.error_for_status().map_err(|e| e.to_string())?;
        } else {
            response
                .error_for_status()
                .map_err(|e| format!("选定预设版本不可用：{e}"))?;
        }
        let resolved: Value = client
            .get(&profile_url)
            .send()
            .map_err(|e| e.to_string())?
            .error_for_status()
            .map_err(|e| e.to_string())?
            .json()
            .map_err(|e| e.to_string())?;
        for (role, token_name, token) in tokens {
            let saved = &resolved["profile"];
            let mut cfg = json!({"mcp_url":format!("{}/mcp",p.base),"delivery_root":p.data.join("delivery"),"token_env":token_name,
                "profile_sha256":resolved["profile_sha256"],"processing_profile":saved,"role":role,"ffmpeg":config.ffmpeg,"ffprobe":config.ffprobe});
            let (executable, args, cwd) = if role == "exporter" {
                cfg["cli_path"] = json!(config
                    .resource_root
                    .join("workers/node/timeline-cli/src/cli.mjs"));
                cfg["adapters_path"] = json!(config
                    .resource_root
                    .join("workers/node/timeline-cli/adapters/index.mjs"));
                (
                    config.node.clone(),
                    vec![config
                        .resource_root
                        .join("workers/node/export-worker/src/main.mjs")
                        .display()
                        .to_string()],
                    config.resource_root.clone(),
                )
            } else {
                if role == "ingester" {
                    cfg.as_object_mut().unwrap().remove("profile_sha256");
                    cfg.as_object_mut().unwrap().remove("processing_profile");
                    cfg["ingest_roots"] = json!(config.ingest_roots);
                } else {
                    let provider = &saved[if role == "recognizer" {
                        "vision"
                    } else {
                        "narration"
                    }];
                    cfg["content_provider"] = provider["adapter"].clone();
                    cfg["tts_auto_approve"] = json!(false);
                    cfg["provider_timeout_seconds"] = json!(30);
                    if provider["adapter"] != "builtin" {
                        cfg["content_provider_config"] = json!({"endpoint":provider["endpoint"],"model":provider["model"],"token_env":provider["token_env"],"api_format":provider["api_format"],"allow_external":true});
                    }
                }
                (
                    config.python.clone(),
                    vec!["-m".into(), "vac_worker".into()],
                    config.resource_root.join("workers/python"),
                )
            };
            let cfg_path = p.directory.join(format!("{role}.json"));
            write_json(&cfg_path, &cfg)?;
            let mut args = args;
            args.extend(["--config".into(), cfg_path.display().to_string()]);
            let out = p.directory.join(format!("{role}.out"));
            let err = p.directory.join(format!("{role}.err"));
            let mut command = Command::new(&executable);
            command
                .args(&args)
                .current_dir(&cwd)
                .env("PATH", &p.path)
                .env(&token_name, &token)
                .env("VAC_SESSION_STOP", p.directory.join("session.stop"))
                .stdin(Stdio::null())
                .stdout(Stdio::from(log_file(&out)?))
                .stderr(Stdio::from(log_file(&err)?));
            let child = p
                .job
                .spawn(&mut command)
                .map_err(|e| format!("Worker {role} 启动失败：{e}"))?;
            p.workers.push(Worker {
                role: role.into(),
                child,
                executable,
                args,
                cwd,
                token_name,
                token,
                recent: VecDeque::new(),
                blocked: false,
                pending: None,
                stdout: out,
                stderr: err,
            });
        }
        p.journal("running")?;
        Ok(p)
    }

    fn output(&self, command: &mut Command) -> Result<Vec<u8>, String> {
        command
            .env("PATH", &self.path)
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::from(log_file(
                &self.directory.join("preflight.err"),
            )?));
        let mut child = self.job.spawn(command).map_err(|e| e.to_string())?;
        let stdout = child.stdout.take().ok_or("无法读取环境检查结果")?;
        let read = thread::spawn(move || {
            let mut bytes = Vec::new();
            let _ = BufReader::new(stdout)
                .take(4 * 1024 * 1024)
                .read_to_end(&mut bytes);
            bytes
        });
        let deadline = Instant::now() + Duration::from_secs(40);
        loop {
            match child.try_wait().map_err(|e| e.to_string())? {
                Some(status) => {
                    let bytes = read.join().unwrap_or_default();
                    if status.success() {
                        return Ok(bytes);
                    }
                    if let Ok(mut log) = log_file(&self.directory.join("preflight.err")) {
                        let _ = log.write_all(&bytes);
                    }
                    return Err("环境或数据库检查失败，请查看 preflight.err".into());
                }
                None if Instant::now() > deadline => {
                    let _ = child.kill();
                    let _ = child.wait();
                    return Err("本地环境检查超时".into());
                }
                None => thread::sleep(Duration::from_millis(50)),
            }
        }
    }
    fn check(&self, cfg: &Config) -> Result<(), String> {
        self.output(Command::new(&cfg.python).args([
            "-c",
            "import sys,sqlite3; assert sys.version_info >= (3,11)",
        ]))?;
        self.output(Command::new(&cfg.node).args(["-e","let [a,b]=process.versions.node.split('.').map(Number);process.exit(a>22||(a===22&&b>=12)?0:1)"]))?;
        self.output(Command::new(&cfg.ffmpeg).arg("-version"))?;
        self.output(Command::new(&cfg.ffprobe).arg("-version"))?;
        for path in [
            cfg.resource_root
                .join("workers/python/vac_worker/__main__.py"),
            cfg.resource_root
                .join("workers/node/export-worker/src/main.mjs"),
            cfg.profile(),
            cfg.control_binary.clone(),
        ] {
            if !path.is_file() {
                return Err(format!("缺少本地运行资源：{}", path.display()));
            }
        }
        Ok(())
    }
    fn journal(&self, status: &str) -> Result<(), String> {
        write_json(
            &self.data.join("desktop-session.json"),
            &json!({"schema_version":1,"session_id":self.session,"status":status,"supervisor_pid":std::process::id(),"api_base_url":self.base,"mcp_url":format!("{}/mcp",self.base),
            "control_pid":self.control.as_ref().map(Child::id),"workers":self.workers.iter().map(|w|json!({"role":w.role,"pid":w.child.id()})).collect::<Vec<_>>() }),
        )
    }
    fn poll(&mut self) -> Result<bool, String> {
        if self
            .control
            .as_mut()
            .ok_or("Go 进程未启动")?
            .try_wait()
            .map_err(|e| e.to_string())?
            .is_some()
        {
            return Err("Go 服务意外退出；本次 Worker 已停止，可重新启动".into());
        }
        let mut changed = false;
        for worker in &mut self.workers {
            if worker.blocked {
                continue;
            }
            if let Some(at) = worker.pending {
                if Instant::now() < at {
                    continue;
                }
                let mut command = Command::new(&worker.executable);
                command
                    .args(&worker.args)
                    .current_dir(&worker.cwd)
                    .env("PATH", &self.path)
                    .env(&worker.token_name, &worker.token)
                    .env("VAC_SESSION_STOP", self.directory.join("session.stop"))
                    .stdin(Stdio::null())
                    .stdout(Stdio::from(log_file(&worker.stdout)?))
                    .stderr(Stdio::from(log_file(&worker.stderr)?));
                worker.child = self.job.spawn(&mut command).map_err(|e| e.to_string())?;
                worker.pending = None;
                changed = true;
            } else if let Some(exit) = worker.child.try_wait().map_err(|e| e.to_string())? {
                let log = fs::read_to_string(&worker.stderr)
                    .unwrap_or_default()
                    .to_lowercase();
                let permanent = exit.code() == Some(3)
                    || [
                        "fatal: config:",
                        "mcp http 401",
                        "mcp http 403",
                        "protocol incompatible",
                        "schema incompatible",
                    ]
                    .iter()
                    .any(|s| log.contains(s));
                while worker
                    .recent
                    .front()
                    .is_some_and(|t| t.elapsed() > Duration::from_secs(60))
                {
                    worker.recent.pop_front();
                }
                if permanent || worker.recent.len() >= 3 {
                    worker.blocked = true;
                } else {
                    worker.pending =
                        Some(Instant::now() + Duration::from_secs(1 << worker.recent.len()));
                    worker.recent.push_back(Instant::now());
                }
                changed = true;
            }
        }
        if changed {
            self.journal("running")?;
        }
        Ok(changed)
    }
    fn stop(&mut self) -> Result<(), String> {
        fs::write(
            self.directory.join("session.stop"),
            b"desktop stop requested",
        )
        .map_err(|e| e.to_string())?;
        self.journal("stopping")?;
        let deadline = Instant::now() + Duration::from_secs(15);
        loop {
            let live = self
                .workers
                .iter_mut()
                .any(|w| w.child.try_wait().ok().flatten().is_none());
            if !live || Instant::now() >= deadline {
                break;
            }
            thread::sleep(Duration::from_millis(100));
        }
        for worker in &mut self.workers {
            if worker
                .child
                .try_wait()
                .map_err(|e| e.to_string())?
                .is_none()
            {
                worker.child.kill().map_err(|e| e.to_string())?;
            }
            let _ = worker.child.wait();
        }
        if let Some(control) = &mut self.control {
            if let Some(stdin) = &mut control.stdin {
                let _ = stdin.write_all(b"{\"command\":\"shutdown\"}\n");
            }
            let deadline = Instant::now() + Duration::from_secs(5);
            while control.try_wait().map_err(|e| e.to_string())?.is_none()
                && Instant::now() < deadline
            {
                thread::sleep(Duration::from_millis(50));
            }
            if control.try_wait().map_err(|e| e.to_string())?.is_none() {
                control.kill().map_err(|e| e.to_string())?;
            }
            let _ = control.wait();
        }
        self.journal("stopped")
    }
}
use uuid::Uuid;
pub fn client() -> Result<Client, String> {
    Client::builder()
        .no_proxy()
        .timeout(Duration::from_secs(10))
        .build()
        .map_err(|e| e.to_string())
}
pub fn read_json(path: &Path) -> Result<Value, String> {
    plain_path(path)?;
    serde_json::from_slice(&fs::read(path).map_err(|e| e.to_string())?).map_err(|e| e.to_string())
}
fn write_json(path: &Path, value: &Value) -> Result<(), String> {
    plain_path(path)?;
    let temporary = path.with_file_name(format!(".vac-state-{}.tmp", Uuid::new_v4().simple()));
    let result = (|| {
        let mut file = OpenOptions::new()
            .write(true)
            .create_new(true)
            .open(&temporary)
            .map_err(|e| e.to_string())?;
        file.write_all(&serde_json::to_vec_pretty(value).map_err(|e| e.to_string())?)
            .map_err(|e| e.to_string())?;
        file.sync_all().map_err(|e| e.to_string())?;
        drop(file);
        fs::rename(&temporary, path).map_err(|e| e.to_string())
    })();
    if result.is_err() {
        let _ = fs::remove_file(&temporary);
    }
    result
}
fn log_file(path: &Path) -> Result<File, String> {
    plain_path(path)?;
    OpenOptions::new()
        .create(true)
        .append(true)
        .open(path)
        .map_err(|e| e.to_string())
}
