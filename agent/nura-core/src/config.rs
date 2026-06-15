use std::fmt;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

const SYSTEM_CONFIG: &str = "/etc/nura/agent.toml";
const DATA_CONFIG: &str = "/data/etc/agent.toml";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerConfig {
    pub bind: String,
    pub port: u16,
    pub metrics_port: u16,
}

impl Default for ServerConfig {
    fn default() -> Self {
        Self {
            bind: "127.0.0.1".to_string(),
            port: 8080,
            metrics_port: 9090,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum LogLevel {
    Trace,
    Debug,
    Info,
    Warn,
    Error,
}

impl Default for LogLevel {
    fn default() -> Self {
        Self::Info
    }
}

impl fmt::Display for LogLevel {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Trace => write!(f, "trace"),
            Self::Debug => write!(f, "debug"),
            Self::Info => write!(f, "info"),
            Self::Warn => write!(f, "warn"),
            Self::Error => write!(f, "error"),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum RoutingPolicy {
    LocalFirst,
    RemoteFirst,
    LocalOnly,
}

impl Default for RoutingPolicy {
    fn default() -> Self {
        Self::LocalFirst
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ProviderConfig {
    pub active: String,
    pub routing: RoutingPolicy,
    pub model_manifest: PathBuf,
    pub tool_allowlist: PathBuf,
}

impl Default for ProviderConfig {
    fn default() -> Self {
        Self {
            active: "local".to_string(),
            routing: RoutingPolicy::LocalFirst,
            model_manifest: PathBuf::from("/data/models/model.json"),
            tool_allowlist: PathBuf::from("/data/etc/tool_allowlist.toml"),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TimeoutsConfig {
    pub turn_secs: u64,
    pub tool_call_secs: u64,
    pub provider_connect_secs: u64,
}

impl Default for TimeoutsConfig {
    fn default() -> Self {
        Self {
            turn_secs: 120,
            tool_call_secs: 30,
            provider_connect_secs: 10,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TokenBudgetConfig {
    pub max_context_tokens: u32,
    pub max_output_tokens: u32,
    pub max_tool_iterations: u32,
}

impl Default for TokenBudgetConfig {
    fn default() -> Self {
        Self {
            max_context_tokens: 4096,
            max_output_tokens: 1024,
            max_tool_iterations: 10,
        }
    }
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Config {
    #[serde(default)]
    pub server: ServerConfig,
    #[serde(default)]
    pub log_level: LogLevel,
    #[serde(default)]
    pub provider: ProviderConfig,
    #[serde(default)]
    pub timeouts: TimeoutsConfig,
    #[serde(default)]
    pub token_budget: TokenBudgetConfig,
}

impl Config {
    pub fn load() -> Result<Self, ConfigError> {
        let mut cfg = Self::default();

        // Layer 2: /etc/nura/agent.toml (system-wide)
        if let Some(c) = load_file(Path::new(SYSTEM_CONFIG))? {
            cfg.merge(c);
        }

        // Layer 3: /data/etc/agent.toml (per-device)
        if let Some(c) = load_file(Path::new(DATA_CONFIG))? {
            cfg.merge(c);
        }

        // Layer 4: environment variable overrides
        cfg.apply_env();

        Ok(cfg)
    }

    fn merge(&mut self, other: Config) {
        // Shallow merge: override non-default fields from 'other'.
        // For simplicity we replace the entire sub-struct when the toml
        // file supplies the relevant section.
        self.server = other.server;
        self.log_level = other.log_level;
        self.provider = other.provider;
        self.timeouts = other.timeouts;
        self.token_budget = other.token_budget;
    }

    fn apply_env(&mut self) {
        if let Ok(level) = std::env::var("NURA_LOG_LEVEL") {
            self.log_level = match level.to_lowercase().as_str() {
                "trace" => LogLevel::Trace,
                "debug" => LogLevel::Debug,
                "warn" => LogLevel::Warn,
                "error" => LogLevel::Error,
                _ => LogLevel::Info,
            };
        }
        if let Ok(port) = std::env::var("NURA_PORT") {
            if let Ok(p) = port.parse() {
                self.server.port = p;
            }
        }
        if let Ok(provider) = std::env::var("NURA_PROVIDER") {
            self.provider.active = provider;
        }
    }

    pub fn validate(&self) -> Vec<String> {
        let mut errors = Vec::new();
        if self.server.port == 0 {
            errors.push("server.port must not be 0".to_string());
        }
        if self.token_budget.max_tool_iterations == 0 {
            errors.push("token_budget.max_tool_iterations must be > 0".to_string());
        }
        if self.timeouts.turn_secs == 0 {
            errors.push("timeouts.turn_secs must be > 0".to_string());
        }
        errors
    }
}

fn load_file(path: &Path) -> Result<Option<Config>, ConfigError> {
    if !path.exists() {
        return Ok(None);
    }
    let text = std::fs::read_to_string(path)
        .map_err(|e| ConfigError::Io(path.to_owned(), e.to_string()))?;
    let cfg: Config =
        toml::from_str(&text).map_err(|e| ConfigError::Parse(path.to_owned(), e.to_string()))?;
    Ok(Some(cfg))
}

#[derive(Debug)]
pub enum ConfigError {
    Io(PathBuf, String),
    Parse(PathBuf, String),
}

impl fmt::Display for ConfigError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Io(p, e) => write!(f, "cannot read {}: {}", p.display(), e),
            Self::Parse(p, e) => write!(f, "cannot parse {}: {}", p.display(), e),
        }
    }
}
