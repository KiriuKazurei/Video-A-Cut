#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
mod config;
mod launch;
mod platform;
mod runtime;
mod shell;
mod updater;

fn main() {
    let environment = match launch::environment() {
        Ok(environment) => environment,
        Err(error) => {
            eprintln!("启动参数无效：{error}");
            return;
        }
    };
    shell::run(environment);
}
