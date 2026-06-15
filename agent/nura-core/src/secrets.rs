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
    check_permissions(path)?;
    let text = std::fs::read_to_string(path)
        .map_err(|e| SecretsError::Io(path.display().to_string(), e.to_string()))?;
    let s: Secrets = toml::from_str(&text)
        .map_err(|e| SecretsError::Parse(path.display().to_string(), e.to_string()))?;
    Ok(Some(s))
}

/// Abort if the secrets file is group- or world-readable (mode & 0o044 != 0).
#[cfg(unix)]
fn check_permissions(path: &Path) -> Result<(), SecretsError> {
    use std::os::unix::fs::MetadataExt;
    let meta = std::fs::metadata(path)
        .map_err(|e| SecretsError::Io(path.display().to_string(), e.to_string()))?;
    if meta.mode() & 0o044 != 0 {
        return Err(SecretsError::UnsafePermissions(path.display().to_string()));
    }
    Ok(())
}

#[cfg(not(unix))]
fn check_permissions(_path: &Path) -> Result<(), SecretsError> {
    Ok(())
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
    /// Secrets file has unsafe permissions (group- or world-readable).
    UnsafePermissions(String),
}

impl fmt::Display for SecretsError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Io(p, e) => write!(f, "cannot read {}: {}", p, e),
            Self::Parse(p, e) => write!(f, "cannot parse {}: {}", p, e),
            Self::UnsafePermissions(p) => write!(
                f,
                "secrets file {} is group- or world-readable; \
                 run 'chmod 600 {}' and restart",
                p, p
            ),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::os::unix::fs::PermissionsExt;

    #[test]
    #[cfg(unix)]
    fn test_world_readable_rejected() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("secrets.toml");
        std::fs::write(&path, "gateway_token = \"tok\"\n").unwrap();
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o644)).unwrap();
        let err = load_file(&path).unwrap_err();
        assert!(
            matches!(err, SecretsError::UnsafePermissions(_)),
            "expected UnsafePermissions, got: {err}"
        );
    }

    #[test]
    #[cfg(unix)]
    fn test_owner_only_accepted() {
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("secrets.toml");
        std::fs::write(&path, "gateway_token = \"tok\"\n").unwrap();
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600)).unwrap();
        let result = load_file(&path).unwrap();
        assert!(result.is_some());
    }

    #[test]
    fn test_env_override_takes_precedence() {
        std::env::set_var("ANTHROPIC_API_KEY", "env-key");
        let s = Secrets::load().unwrap();
        assert_eq!(s.anthropic_api_key.unwrap().expose(), "env-key");
        std::env::remove_var("ANTHROPIC_API_KEY");
    }

    #[test]
    fn test_secret_string_redacted_in_display() {
        let s = SecretString("my-secret".to_string());
        assert!(!s.to_string().contains("my-secret"));
        assert!(!format!("{:?}", s).contains("my-secret"));
    }
}
