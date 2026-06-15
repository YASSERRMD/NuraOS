use std::fmt;

/// Crate-wide error taxonomy for nura-agent.
///
/// Each variant maps to a stable exit code (for CLI) and an HTTP status
/// (for the gateway layer, wired in Phase 28+).
#[derive(Debug)]
pub enum NuraError {
    /// Configuration file missing, unparseable, or invalid.
    Config(String),
    /// Secrets file permission or parse error.
    Secrets(String),
    /// Inference provider unreachable or returned an error.
    Provider { provider: String, detail: String },
    /// A tool call failed validation or execution.
    Tool { name: String, detail: String },
    /// The turn budget (time or iteration limit) was exceeded.
    BudgetExceeded(String),
    /// Session store read/write failure.
    Session(String),
    /// I/O error not covered by a more specific variant.
    Io(String),
    /// An internal invariant was violated.
    Internal(String),
}

impl NuraError {
    /// Stable process exit code for this error class.
    pub fn exit_code(&self) -> i32 {
        match self {
            Self::Config(_) => 2,
            Self::Secrets(_) => 3,
            Self::Provider { .. } => 4,
            Self::Tool { .. } => 5,
            Self::BudgetExceeded(_) => 6,
            Self::Session(_) => 7,
            Self::Io(_) => 8,
            Self::Internal(_) => 1,
        }
    }

    /// HTTP status code for use in the gateway response (Phase 28+).
    pub fn http_status(&self) -> u16 {
        match self {
            Self::Config(_) => 500,
            Self::Secrets(_) => 500,
            Self::Provider { .. } => 502,
            Self::Tool { .. } => 422,
            Self::BudgetExceeded(_) => 408,
            Self::Session(_) => 500,
            Self::Io(_) => 500,
            Self::Internal(_) => 500,
        }
    }
}

impl fmt::Display for NuraError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Config(s) => write!(f, "config error: {}", s),
            Self::Secrets(s) => write!(f, "secrets error: {}", s),
            Self::Provider { provider, detail } => {
                write!(f, "provider '{}' error: {}", provider, detail)
            }
            Self::Tool { name, detail } => write!(f, "tool '{}' error: {}", name, detail),
            Self::BudgetExceeded(s) => write!(f, "budget exceeded: {}", s),
            Self::Session(s) => write!(f, "session error: {}", s),
            Self::Io(s) => write!(f, "I/O error: {}", s),
            Self::Internal(s) => write!(f, "internal error: {}", s),
        }
    }
}

impl std::error::Error for NuraError {}

impl From<crate::config::ConfigError> for NuraError {
    fn from(e: crate::config::ConfigError) -> Self {
        Self::Config(e.to_string())
    }
}

impl From<crate::secrets::SecretsError> for NuraError {
    fn from(e: crate::secrets::SecretsError) -> Self {
        Self::Secrets(e.to_string())
    }
}

pub type Result<T> = std::result::Result<T, NuraError>;
