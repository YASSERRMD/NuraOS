use std::path::Path;

use nura_core::config::Config;
use nura_core::secrets::Secrets;
use nura_core::version;

use crate::registry::{ProbeResult, ProviderRegistry};

struct Check {
    label: &'static str,
    result: Result<String, String>,
}

impl Check {
    fn pass(label: &'static str, detail: impl Into<String>) -> Self {
        Self {
            label,
            result: Ok(detail.into()),
        }
    }
    fn fail(label: &'static str, reason: impl Into<String>) -> Self {
        Self {
            label,
            result: Err(reason.into()),
        }
    }
    fn warn(label: &'static str, detail: impl Into<String>) -> Self {
        // Use Ok with a WARN prefix so callers see it in the pass column.
        Self {
            label,
            result: Ok(format!("WARN: {}", detail.into())),
        }
    }
}

pub fn run() -> nura_core::error::Result<()> {
    println!("nura-agent doctor ({})", version::version_string());
    println!("{}", "=".repeat(50));

    let checks = vec![
        check_proc_fs(),
        check_sys_fs(),
        check_data_mount(),
        check_data_dirs(),
        check_llama_server(),
        check_model(),
        check_config(),
        check_secrets_redacted(),
        check_provider_registry(),
    ];

    let mut failures = 0;
    for c in &checks {
        match &c.result {
            Ok(detail) => println!("[PASS] {:30} {}", c.label, detail),
            Err(reason) => {
                println!("[FAIL] {:30} {}", c.label, reason);
                failures += 1;
            }
        }
    }

    println!("{}", "=".repeat(50));
    if failures == 0 {
        println!("All checks passed.");
        Ok(())
    } else {
        println!("{} check(s) failed.", failures);
        Err(nura_core::error::NuraError::Internal(format!(
            "{} doctor check(s) failed",
            failures
        )))
    }
}

fn check_proc_fs() -> Check {
    if Path::new("/proc/uptime").exists() {
        Check::pass("/proc mounted", "ok")
    } else {
        Check::fail("/proc mounted", "/proc/uptime not found")
    }
}

fn check_sys_fs() -> Check {
    if Path::new("/sys/class").exists() {
        Check::pass("/sys mounted", "ok")
    } else {
        Check::fail("/sys mounted", "/sys/class not found")
    }
}

fn check_data_mount() -> Check {
    if Path::new("/data").exists() {
        // Check if it is a real mount (not just the initramfs /data dir).
        match std::fs::read_to_string("/proc/mounts") {
            Ok(mounts) => {
                if mounts.lines().any(|l| l.contains("/data")) {
                    Check::pass("/data mounted", "ok")
                } else {
                    Check::warn(
                        "/data mounted",
                        "/data exists but not in /proc/mounts (tmpfs fallback)",
                    )
                }
            }
            Err(_) => Check::warn("/data mounted", "could not read /proc/mounts"),
        }
    } else {
        Check::fail("/data mounted", "/data directory missing")
    }
}

fn check_data_dirs() -> Check {
    let required = ["models", "logs", "sessions", "etc"];
    let mut missing = Vec::new();
    for d in &required {
        if !Path::new("/data").join(d).exists() {
            missing.push(*d);
        }
    }
    if missing.is_empty() {
        Check::pass("/data subdirs", "models logs sessions etc")
    } else {
        Check::fail("/data subdirs", format!("missing: {}", missing.join(", ")))
    }
}

fn check_llama_server() -> Check {
    if Path::new("/sbin/llama-server").exists() {
        Check::pass("llama-server", "found at /sbin/llama-server")
    } else {
        Check::warn("llama-server", "not installed yet (Phase 16)")
    }
}

fn check_config() -> Check {
    match Config::load() {
        Ok(cfg) => {
            let errs = cfg.validate();
            if errs.is_empty() {
                Check::pass(
                    "config",
                    format!("provider={} port={}", cfg.provider.active, cfg.server.port),
                )
            } else {
                Check::fail("config", errs.join("; "))
            }
        }
        Err(e) => Check::fail("config", e.to_string()),
    }
}

fn check_secrets_redacted() -> Check {
    match Secrets::load() {
        Ok(s) => {
            let mut present = Vec::new();
            if s.anthropic_api_key.is_some() {
                present.push("anthropic_api_key");
            }
            if s.openai_api_key.is_some() {
                present.push("openai_api_key");
            }
            if s.gateway_token.is_some() {
                present.push("gateway_token");
            }
            if present.is_empty() {
                Check::pass("secrets", "none configured (local-only mode)")
            } else {
                Check::pass(
                    "secrets",
                    format!("present: {} (values redacted)", present.join(", ")),
                )
            }
        }
        Err(e) => Check::fail("secrets", e.to_string()),
    }
}

fn check_provider_registry() -> Check {
    let cfg = Config::load().unwrap_or_default();
    let secrets = Secrets::load().unwrap_or_default();
    let reg = ProviderRegistry::from_config(&cfg, &secrets);

    if reg.is_empty() {
        return Check::fail("provider registry", "no providers configured");
    }

    let probes = reg.probe_local_reachability();
    let mut lines: Vec<String> = Vec::new();
    let mut any_reachable = false;

    for (entry, probe) in &probes {
        let status = match probe {
            ProbeResult::Reachable => {
                any_reachable = true;
                "up".to_string()
            }
            ProbeResult::Unreachable(reason) => format!("down ({})", reason),
            ProbeResult::Skipped(msg) => {
                any_reachable = true; // assume remote providers reachable if key present
                format!("key present, {}", msg)
            }
        };
        lines.push(format!("{}/{}: {}", entry.name, entry.tier, status));
    }

    let detail = lines.join("; ");
    if !any_reachable {
        Check::warn(
            "provider registry",
            format!("no provider reachable: {}", detail),
        )
    } else {
        Check::pass("provider registry", detail)
    }
}

fn check_model() -> Check {
    let manifest = Path::new("/data/models/model.json");
    let models_dir = Path::new("/data/models");

    if manifest.exists() {
        Check::pass("model manifest", "/data/models/model.json present")
    } else if models_dir.exists() {
        // Look for any .gguf file.
        match std::fs::read_dir(models_dir) {
            Ok(entries) => {
                let has_gguf = entries
                    .filter_map(|e| e.ok())
                    .any(|e| e.path().extension().map(|x| x == "gguf").unwrap_or(false));
                if has_gguf {
                    Check::warn(
                        "model manifest",
                        "found .gguf but no model.json; create /data/models/model.json",
                    )
                } else {
                    Check::warn("model", "no .gguf files in /data/models (Phase 16)")
                }
            }
            Err(e) => Check::fail("model", format!("cannot read /data/models: {}", e)),
        }
    } else {
        Check::warn("model", "/data/models missing -- is /data mounted?")
    }
}
