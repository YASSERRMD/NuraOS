use std::fs::{self, OpenOptions};
use std::path::Path;

use tracing_subscriber::filter::EnvFilter;
use tracing_subscriber::fmt;
use tracing_subscriber::prelude::*;

use crate::config::LogLevel;

const LOG_DIR: &str = "/data/logs";
const LOG_FILE: &str = "/data/logs/agent.log";
const MAX_LOG_BYTES: u64 = 10 * 1024 * 1024; // 10 MB per file

pub struct LoggingConfig {
    pub level: LogLevel,
}

impl Default for LoggingConfig {
    fn default() -> Self {
        Self {
            level: LogLevel::Info,
        }
    }
}

/// Initialise the global tracing subscriber.
///
/// Writes structured log events to stderr (console) and, when /data/logs
/// is available, to a rotating JSON file at /data/logs/agent.log.
pub fn init(cfg: &LoggingConfig) {
    let level_str = cfg.level.to_string();
    let env_filter =
        EnvFilter::try_from_env("RUST_LOG").unwrap_or_else(|_| EnvFilter::new(&level_str));

    let console = fmt::layer().compact().with_target(true);

    if Path::new(LOG_DIR).exists() {
        rotate_if_needed();
        if let Ok(file) = OpenOptions::new().create(true).append(true).open(LOG_FILE) {
            let file_layer = fmt::layer().json().with_ansi(false).with_writer(file);
            tracing_subscriber::registry()
                .with(env_filter)
                .with(console)
                .with(file_layer)
                .init();
            return;
        }
    }

    // /data not available: console only.
    tracing_subscriber::registry()
        .with(env_filter)
        .with(console)
        .init();
}

fn rotate_if_needed() {
    let path = Path::new(LOG_FILE);
    if let Ok(meta) = fs::metadata(path) {
        if meta.len() >= MAX_LOG_BYTES {
            let rotated = format!("{}.1", LOG_FILE);
            let _ = fs::rename(path, rotated);
        }
    }
}
