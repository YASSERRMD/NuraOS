use nura_core::config::Config;
use nura_core::provider::traits::{Capabilities, Provider};
use nura_core::secrets::Secrets;

use crate::providers::local::{LocalProvider, DEFAULT_BASE_URL as LOCAL_BASE_URL};

// Environment variables that opt-in to sovereign/self-hosted providers.
const ENV_OLLAMA: &str = "NURA_OLLAMA";
const ENV_OLLAMA_MODEL: &str = "NURA_OLLAMA_MODEL";
const ENV_LM_STUDIO: &str = "NURA_LMSTUDIO";
const ENV_LM_STUDIO_MODEL: &str = "NURA_LMSTUDIO_MODEL";
/// Arbitrary OpenAI-compatible endpoint: NURA_CUSTOM_ENDPOINT=http://host:port
const ENV_CUSTOM_ENDPOINT: &str = "NURA_CUSTOM_ENDPOINT";
/// Model name for the custom endpoint.
const ENV_CUSTOM_MODEL: &str = "NURA_CUSTOM_MODEL";

/// Summary of one registered provider, used by doctor and boot validation.
#[derive(Debug)]
pub struct ProviderEntry {
    pub name: String,
    /// True for llama-server (127.0.0.1); false for cloud APIs.
    pub is_local: bool,
    pub capabilities: Capabilities,
    /// Approximate cost/latency tier.
    pub tier: ProviderTier,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ProviderTier {
    Local,
    Cloud,
}

impl std::fmt::Display for ProviderTier {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Local => write!(f, "local"),
            Self::Cloud => write!(f, "cloud"),
        }
    }
}

/// Holds all constructed providers for this session.
pub struct ProviderRegistry {
    entries: Vec<(ProviderEntry, Box<dyn Provider>)>,
    default_name: String,
}

impl ProviderRegistry {
    /// Build the registry from config and secrets.
    ///
    /// Always adds the local provider. Remote providers are added only when
    /// the `remote-providers` feature is enabled and API keys are present.
    pub fn from_config(cfg: &Config, secrets: &Secrets) -> Self {
        let mut entries: Vec<(ProviderEntry, Box<dyn Provider>)> = Vec::new();

        // ---- local provider (always available) ----
        let local = LocalProvider::new(LOCAL_BASE_URL);
        entries.push((
            ProviderEntry {
                name: "local".into(),
                is_local: true,
                capabilities: local.capabilities(),
                tier: ProviderTier::Local,
            },
            Box::new(local),
        ));

        // ---- remote providers (feature-gated) ----
        #[cfg(feature = "remote-providers")]
        {
            use crate::providers::{AnthropicProvider, OpenAiCompatProvider};

            if let Some(key) = secrets.anthropic_api_key.as_ref() {
                let p = AnthropicProvider::new(
                    key.expose(),
                    crate::providers::anthropic::DEFAULT_BASE_URL,
                    crate::providers::anthropic::DEFAULT_MODEL,
                );
                entries.push((
                    ProviderEntry {
                        name: "anthropic".into(),
                        is_local: false,
                        capabilities: p.capabilities(),
                        tier: ProviderTier::Cloud,
                    },
                    Box::new(p),
                ));
            }

            if let Some(key) = secrets.openai_api_key.as_ref() {
                let p = OpenAiCompatProvider::new(
                    Some(key.expose().to_string()),
                    crate::providers::openai::DEFAULT_OPENAI_BASE_URL,
                    crate::providers::openai::DEFAULT_MODEL,
                );
                entries.push((
                    ProviderEntry {
                        name: "openai".into(),
                        is_local: false,
                        capabilities: p.capabilities(),
                        tier: ProviderTier::Cloud,
                    },
                    Box::new(p),
                ));
            }
        }

        // ---- sovereign / self-hosted OpenAI-compatible providers ----
        // These are registered when opt-in env vars are set.
        #[cfg(feature = "remote-providers")]
        {
            use crate::providers::openai::{
                LM_STUDIO_BASE_URL, LM_STUDIO_DEFAULT_MODEL, OLLAMA_BASE_URL, OLLAMA_DEFAULT_MODEL,
            };
            use crate::providers::OpenAiCompatProvider;

            // Ollama: NURA_OLLAMA=1 enables; NURA_OLLAMA_MODEL overrides model.
            if std::env::var(ENV_OLLAMA).as_deref() == Ok("1") {
                let model =
                    std::env::var(ENV_OLLAMA_MODEL).unwrap_or_else(|_| OLLAMA_DEFAULT_MODEL.into());
                let p = OpenAiCompatProvider::new(None::<String>, OLLAMA_BASE_URL, model);
                entries.push((
                    ProviderEntry {
                        name: "ollama".into(),
                        is_local: true,
                        capabilities: p.capabilities(),
                        tier: ProviderTier::Local,
                    },
                    Box::new(p),
                ));
            }

            // LM Studio: NURA_LMSTUDIO=1 enables; NURA_LMSTUDIO_MODEL overrides.
            if std::env::var(ENV_LM_STUDIO).as_deref() == Ok("1") {
                let model = std::env::var(ENV_LM_STUDIO_MODEL)
                    .unwrap_or_else(|_| LM_STUDIO_DEFAULT_MODEL.into());
                let p = OpenAiCompatProvider::new(None::<String>, LM_STUDIO_BASE_URL, model);
                entries.push((
                    ProviderEntry {
                        name: "lm-studio".into(),
                        is_local: true,
                        capabilities: p.capabilities(),
                        tier: ProviderTier::Local,
                    },
                    Box::new(p),
                ));
            }

            // Custom endpoint: NURA_CUSTOM_ENDPOINT=http://host:port
            if let Ok(base_url) = std::env::var(ENV_CUSTOM_ENDPOINT) {
                let model = std::env::var(ENV_CUSTOM_MODEL).unwrap_or_else(|_| "custom".into());
                // Custom endpoints may not need a key; pass secrets.openai_api_key
                // as a fallback for endpoints that require one.
                let key = secrets
                    .openai_api_key
                    .as_ref()
                    .map(|s| s.expose().to_string());
                let p = OpenAiCompatProvider::new(key, &base_url, model);
                entries.push((
                    ProviderEntry {
                        name: "custom".into(),
                        is_local: false,
                        capabilities: p.capabilities(),
                        tier: ProviderTier::Cloud,
                    },
                    Box::new(p),
                ));
            }
        }

        // ---- llama-ffi provider (feature-gated) ----
        #[cfg(feature = "llama-ffi")]
        {
            use crate::providers::LlamaFfiProvider;

            let model_path = cfg
                .provider
                .model_manifest
                .to_str()
                .unwrap_or(crate::providers::llama_ffi::DEFAULT_MODEL_PATH)
                .to_string();
            let p = LlamaFfiProvider::new(model_path, crate::providers::llama_ffi::DEFAULT_N_CTX);
            entries.push((
                ProviderEntry {
                    name: "llama-ffi".into(),
                    is_local: true,
                    capabilities: p.capabilities(),
                    tier: ProviderTier::Local,
                },
                Box::new(p),
            ));
        }

        // Suppress unused warnings when remote-providers feature is off.
        let _ = secrets;

        Self {
            default_name: cfg.provider.active.clone(),
            entries,
        }
    }

    /// Look up a provider by name.
    pub fn get(&self, name: &str) -> Option<&dyn Provider> {
        self.entries
            .iter()
            .find(|(e, _)| e.name == name)
            .map(|(_, p)| p.as_ref())
    }

    /// The provider chosen by the config `provider.active` setting,
    /// falling back to the first registered provider.
    #[allow(dead_code)]
    pub fn default_provider(&self) -> Option<&dyn Provider> {
        self.get(&self.default_name)
            .or_else(|| self.entries.first().map(|(_, p)| p.as_ref()))
    }

    pub fn default_name(&self) -> &str {
        &self.default_name
    }

    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }

    #[allow(dead_code)]
    pub fn len(&self) -> usize {
        self.entries.len()
    }

    /// Summarise all registered providers (without touching the network).
    pub fn list_entries(&self) -> impl Iterator<Item = &ProviderEntry> {
        self.entries.iter().map(|(e, _)| e)
    }

    /// Probe reachability for local providers (HTTP /health) and return results.
    ///
    /// Remote providers are listed as reachable=None (no network call is made
    /// to avoid leaking keys or incurring costs during doctor).
    pub fn probe_local_reachability(&self) -> Vec<(&ProviderEntry, ProbeResult)> {
        self.entries
            .iter()
            .map(|(entry, _)| {
                let result = if entry.is_local {
                    probe_local_health(LOCAL_BASE_URL)
                } else {
                    ProbeResult::Skipped("remote: no probe (avoids key use)")
                };
                (entry, result)
            })
            .collect()
    }
}

#[derive(Debug)]
pub enum ProbeResult {
    Reachable,
    Unreachable(String),
    Skipped(&'static str),
}

fn probe_local_health(base_url: &str) -> ProbeResult {
    let url = format!("{}/health", base_url.trim_end_matches('/'));
    match ureq::get(&url)
        .timeout(std::time::Duration::from_secs(2))
        .call()
    {
        Ok(resp) if resp.status() == 200 => ProbeResult::Reachable,
        Ok(resp) => ProbeResult::Unreachable(format!("HTTP {}", resp.status())),
        Err(e) => ProbeResult::Unreachable(format!("{}", e)),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use nura_core::config::Config;
    use nura_core::secrets::Secrets;

    #[test]
    fn registry_always_has_local() {
        let cfg = Config::default();
        let secrets = Secrets::default();
        let reg = ProviderRegistry::from_config(&cfg, &secrets);
        assert!(!reg.is_empty(), "registry must always contain local");
        assert!(reg.get("local").is_some(), "local provider must be present");
    }

    #[test]
    fn default_provider_falls_back_to_first() {
        let mut cfg = Config::default();
        cfg.provider.active = "nonexistent".into();
        let secrets = Secrets::default();
        let reg = ProviderRegistry::from_config(&cfg, &secrets);
        assert!(
            reg.default_provider().is_some(),
            "should fall back to first provider"
        );
    }

    #[test]
    fn local_entry_has_correct_tier() {
        let reg = ProviderRegistry::from_config(&Config::default(), &Secrets::default());
        let entry = reg.list_entries().find(|e| e.name == "local").unwrap();
        assert_eq!(entry.tier, ProviderTier::Local);
        assert!(entry.is_local);
    }
}
