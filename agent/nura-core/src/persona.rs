use std::path::{Path, PathBuf};
use std::sync::{Arc, RwLock};

use tracing::{info, warn};

use crate::error::{NuraError, Result};

// ---------------------------------------------------------------------------
// Built-in default system prompt
// ---------------------------------------------------------------------------

/// Embedded fallback used when /data/etc/system_prompt.md is missing or empty.
pub const DEFAULT_SYSTEM_PROMPT: &str = "\
You are the NuraOS assistant, a local-first AI embedded directly in the operating\
 system. You run entirely on-device and operate without sending data to external\
 servers unless the operator has explicitly configured a remote provider.\n\
\n\
Your primary responsibilities:\n\
- Help the operator understand and manage this NuraOS appliance.\n\
- Execute read-only system tools (system.info, fs.read, net.status, time.now)\
 when they are relevant to the operator's question.\n\
- Provide concise, accurate answers grounded in what you can observe locally.\n\
\n\
Guidelines:\n\
- Prefer brevity. The console display is narrow.\n\
- Do not speculate about external network services you cannot reach.\n\
- When a tool would help, use it rather than guessing.\n\
- Acknowledge uncertainty explicitly rather than fabricating information.\
";

// ---------------------------------------------------------------------------
// Persona configuration
// ---------------------------------------------------------------------------

/// Verbosity level for the agent's text responses.
#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub enum Verbosity {
    /// Single-sentence or brief-paragraph answers.
    Concise,
    /// Standard paragraph-length answers (default).
    #[default]
    Normal,
    /// Detailed answers with explanations and examples.
    Verbose,
}

impl std::str::FromStr for Verbosity {
    type Err = String;
    fn from_str(s: &str) -> std::result::Result<Self, Self::Err> {
        match s.to_ascii_lowercase().as_str() {
            "concise" | "brief" => Ok(Verbosity::Concise),
            "normal" | "standard" => Ok(Verbosity::Normal),
            "verbose" | "detailed" => Ok(Verbosity::Verbose),
            other => Err(format!("unknown verbosity '{}'; use concise, normal, or verbose", other)),
        }
    }
}

/// Tool-category gating for persona configuration.
///
/// Only tools whose category appears in `allowed_tool_categories` are offered
/// to the model. An empty set means all categories are allowed.
#[derive(Debug, Clone, Default)]
pub struct PersonaConfig {
    /// Response verbosity preference injected into context-window instructions.
    pub verbosity: Verbosity,
    /// Allowed tool categories. Empty = no restriction.
    pub allowed_tool_categories: Vec<String>,
}

// ---------------------------------------------------------------------------
// Prompt loader
// ---------------------------------------------------------------------------

/// Loads and validates the system prompt from disk; falls back to the built-in
/// default when the file is absent or empty. Supports hot-reload via `reload()`.
pub struct PromptLoader {
    path: PathBuf,
    current: Arc<RwLock<String>>,
}

impl PromptLoader {
    /// Create a loader pointing at `path` (typically `/data/etc/system_prompt.md`).
    /// Reads the file immediately; falls back to the built-in default on failure.
    pub fn new(path: impl Into<PathBuf>) -> Self {
        let path = path.into();
        let prompt = load_from_path(&path);
        Self {
            path,
            current: Arc::new(RwLock::new(prompt)),
        }
    }

    /// Return the currently loaded prompt text.
    pub fn prompt(&self) -> String {
        self.current.read().unwrap().clone()
    }

    /// Re-read the prompt file and update the in-memory copy without restarting
    /// the agent. Returns `Ok(())` even when falling back to the built-in default
    /// (a warning is logged in that case).
    pub fn reload(&self) -> Result<()> {
        let text = load_from_path(&self.path);
        *self.current.write().unwrap() = text;
        info!(path = %self.path.display(), "system prompt reloaded");
        Ok(())
    }

    /// Validate that the in-memory prompt is non-empty and within a reasonable
    /// size (1 MB). Called at boot to surface misconfiguration early.
    pub fn validate(&self) -> Result<()> {
        let prompt = self.prompt();
        if prompt.trim().is_empty() {
            return Err(NuraError::Config(
                "system prompt is empty after loading".into(),
            ));
        }
        if prompt.len() > 1_048_576 {
            return Err(NuraError::Config(format!(
                "system prompt is too large ({} bytes); max is 1 MiB",
                prompt.len()
            )));
        }
        Ok(())
    }

    /// Apply the `verbosity` persona setting by appending a short instruction to
    /// the prompt. Returns a new string (the stored prompt is unchanged).
    pub fn with_persona(&self, config: &PersonaConfig) -> String {
        let base = self.prompt();
        match config.verbosity {
            Verbosity::Concise => {
                format!("{}\n\nReply concisely. Prefer one or two sentences.", base)
            }
            Verbosity::Normal => base,
            Verbosity::Verbose => {
                format!("{}\n\nProvide detailed answers with explanations.", base)
            }
        }
    }
}

fn load_from_path(path: &Path) -> String {
    match std::fs::read_to_string(path) {
        Ok(text) if !text.trim().is_empty() => {
            info!(path = %path.display(), bytes = text.len(), "system prompt loaded from file");
            text
        }
        Ok(_) => {
            warn!(path = %path.display(), "system prompt file exists but is empty; using built-in default");
            DEFAULT_SYSTEM_PROMPT.to_string()
        }
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
            warn!(path = %path.display(), "system prompt file not found; using built-in default");
            DEFAULT_SYSTEM_PROMPT.to_string()
        }
        Err(e) => {
            warn!(path = %path.display(), error = %e, "cannot read system prompt; using built-in default");
            DEFAULT_SYSTEM_PROMPT.to_string()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn write_tmp(content: &str) -> (tempfile::TmpFile, PathBuf) {
        let dir = std::env::temp_dir();
        let id = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .subsec_nanos();
        let path = dir.join(format!("nura-prompt-test-{}.md", id));
        std::fs::write(&path, content).unwrap();
        (tempfile::TmpFile(path.clone()), path)
    }

    // Minimal tmp file guard.
    mod tempfile {
        use std::path::PathBuf;
        pub struct TmpFile(pub PathBuf);
        impl Drop for TmpFile {
            fn drop(&mut self) {
                let _ = std::fs::remove_file(&self.0);
            }
        }
    }

    #[test]
    fn loads_file_when_present() {
        let (_guard, path) = write_tmp("hello from file");
        let loader = PromptLoader::new(&path);
        assert_eq!(loader.prompt(), "hello from file");
    }

    #[test]
    fn falls_back_to_default_when_missing() {
        let loader = PromptLoader::new("/nonexistent/path/prompt.md");
        assert!(!loader.prompt().is_empty());
        assert!(loader.prompt().contains("NuraOS"));
    }

    #[test]
    fn falls_back_to_default_when_empty() {
        let (_guard, path) = write_tmp("   \n  ");
        let loader = PromptLoader::new(&path);
        assert!(loader.prompt().contains("NuraOS"));
    }

    #[test]
    fn validate_passes_for_non_empty_prompt() {
        let loader = PromptLoader::new("/nonexistent/path/prompt.md");
        assert!(loader.validate().is_ok());
    }

    #[test]
    fn hot_reload_picks_up_new_content() {
        let (_guard, path) = write_tmp("initial content");
        let loader = PromptLoader::new(&path);
        assert_eq!(loader.prompt(), "initial content");

        std::fs::write(&path, "updated content").unwrap();
        loader.reload().unwrap();
        assert_eq!(loader.prompt(), "updated content");
    }

    #[test]
    fn hot_reload_falls_back_when_file_removed() {
        let (guard, path) = write_tmp("original");
        let loader = PromptLoader::new(&path);
        drop(guard); // remove the file
        loader.reload().unwrap();
        assert!(loader.prompt().contains("NuraOS"), "should fall back to default");
    }

    #[test]
    fn verbosity_concise_appends_instruction() {
        let loader = PromptLoader::new("/nonexistent/path/prompt.md");
        let persona = PersonaConfig {
            verbosity: Verbosity::Concise,
            ..Default::default()
        };
        let prompt = loader.with_persona(&persona);
        assert!(prompt.contains("concisely"), "concise instruction must be present");
    }

    #[test]
    fn verbosity_verbose_appends_instruction() {
        let loader = PromptLoader::new("/nonexistent/path/prompt.md");
        let persona = PersonaConfig {
            verbosity: Verbosity::Verbose,
            ..Default::default()
        };
        let prompt = loader.with_persona(&persona);
        assert!(prompt.contains("detailed"), "verbose instruction must be present");
    }

    #[test]
    fn verbosity_normal_returns_base_prompt_unchanged() {
        let loader = PromptLoader::new("/nonexistent/path/prompt.md");
        let base = loader.prompt();
        let persona = PersonaConfig::default();
        assert_eq!(loader.with_persona(&persona), base);
    }

    #[test]
    fn verbosity_from_str() {
        assert_eq!("concise".parse::<Verbosity>().unwrap(), Verbosity::Concise);
        assert_eq!("verbose".parse::<Verbosity>().unwrap(), Verbosity::Verbose);
        assert_eq!("normal".parse::<Verbosity>().unwrap(), Verbosity::Normal);
        assert!("unknown".parse::<Verbosity>().is_err());
    }
}
