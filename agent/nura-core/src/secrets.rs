use std::fmt;
use std::path::Path;

use serde::Deserialize;

const DATA_SECRETS: &str = "/data/etc/secrets.toml";

const REDACTED: &str = "[REDACTED]";

#[derive(Debug, Clone, Default, Deserialize)]
pub struct Secrets {
    pub anthropic_api_key: Option<SecretString>,
    pub openai_api_key: Option<SecretString>,
    pub gateway_token: Option<SecretString>,
}

impl Secrets {
    pub fn load() -> Result<Self, SecretsError> {
        let mut s = Self::default();

        // From /data/etc/secrets.toml.
        if let Some(from_file) = load_file(Path::new(DATA_SECRETS))? {
            s.merge(from_file);
        }

        // Environment variable overrides (take precedence over file).
        if let Ok(key) = std::env::var("ANTHROPIC_API_KEY") {
            s.anthropic_api_key = Some(SecretString(key));
        }
        if let Ok(key) = std::env::var("OPENAI_API_KEY") {
            s.openai_api_key = Some(SecretString(key));
        }
        if let Ok(tok) = std::env::var("NURA_GATEWAY_TOKEN") {
            s.gateway_token = Some(SecretString(tok));
        }

        Ok(s)
    }

    fn merge(&mut self, other: Secrets) {
        if let Some(k) = other.anthropic_api_key {
            self.anthropic_api_key = Some(k);
        }
        if let Some(k) = other.openai_api_key {
            self.openai_api_key = Some(k);
        }
        if let Some(t) = other.gateway_token {
            self.gateway_token = Some(t);
        }
    }
}

fn load_file(path: &Path) -> Result<Option<Secrets>, SecretsError> {
    if !path.exists() {
        return Ok(None);
    }
    let text = std::fs::read_to_string(path)
        .map_err(|e| SecretsError::Io(path.display().to_string(), e.to_string()))?;
    let s: Secrets = toml::from_str(&text)
        .map_err(|e| SecretsError::Parse(path.display().to_string(), e.to_string()))?;
    Ok(Some(s))
}

/// A string that is never printed or logged in plain text.
#[derive(Clone, Deserialize)]
pub struct SecretString(String);

impl SecretString {
    pub fn expose(&self) -> &str {
        &self.0
    }
}

impl fmt::Debug for SecretString {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", REDACTED)
    }
}

impl fmt::Display for SecretString {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", REDACTED)
    }
}

#[derive(Debug)]
pub enum SecretsError {
    Io(String, String),
    Parse(String, String),
}

impl fmt::Display for SecretsError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Io(p, e) => write!(f, "cannot read {}: {}", p, e),
            Self::Parse(p, e) => write!(f, "cannot parse {}: {}", p, e),
        }
    }
}
