use std::process;

use nura_core::config::Config;
use nura_core::logging::{init as init_logging, LoggingConfig};
use nura_core::secrets::Secrets;
use nura_core::telemetry::TurnId;
use nura_core::version;
use tracing::{error, info, warn};

use crate::registry::ProviderRegistry;

pub fn run() {
    let args: Vec<String> = std::env::args().collect();
    let subcommand = args.get(1).map(String::as_str).unwrap_or("run");

    match subcommand {
        "run" => cmd_run(),
        "repl" => crate::repl_cmd::cmd_repl(),
        "version" | "--version" | "-V" => cmd_version(),
        "doctor" => cmd_doctor(),
        "--help" | "-h" | "help" => cmd_help(),
        unknown => {
            eprintln!("nura-agent: unknown subcommand '{}'", unknown);
            eprintln!("Run 'nura-agent help' for usage.");
            process::exit(1);
        }
    }
}

pub fn init_tracing() {
    let cfg = Config::load().unwrap_or_default();
    init_logging(&LoggingConfig {
        level: cfg.log_level,
    });
}

fn cmd_version() {
    println!("{}", version::version_string());
}

fn cmd_help() {
    println!("nura-agent {}", version::VERSION);
    println!();
    println!("USAGE:");
    println!("    nura-agent [SUBCOMMAND]");
    println!();
    println!("SUBCOMMANDS:");
    println!("    run        Start the NuraOS agent (default)");
    println!("    repl       Start an interactive serial REPL session");
    println!("    version    Print the version string");
    println!("    doctor     Check environment and configuration");
    println!("    help       Print this help message");
}

fn cmd_run() {
    init_tracing();

    let turn_id = TurnId::new();
    info!(turn_id = %turn_id, "nura-agent starting ({})", version::version_string());

    // Validate provider registry at boot -- fail closed if nothing is usable.
    let cfg = Config::load().unwrap_or_default();
    let secrets = Secrets::load().unwrap_or_default();
    let reg = ProviderRegistry::from_config(&cfg, &secrets);

    if reg.is_empty() {
        error!("no providers configured; cannot start -- run 'nura-agent doctor'");
        process::exit(2);
    }

    for entry in reg.list_entries() {
        info!(
            provider = %entry.name,
            tier = %entry.tier,
            streaming = entry.capabilities.streaming,
            tool_calling = entry.capabilities.tool_calling,
            "provider registered"
        );
    }

    let default_name = reg.default_name().to_string();
    if reg.get(&default_name).is_none() {
        warn!(
            wanted = %default_name,
            using = "local",
            "configured provider not found; falling back to local"
        );
    } else {
        info!(provider = %default_name, "default provider selected");
    }

    info!(turn_id = %turn_id, "agent ready -- inference loop arrives in Phase 25");

    loop {
        std::thread::sleep(std::time::Duration::from_secs(60));
    }
}

fn cmd_doctor() {
    init_tracing();
    let result = crate::doctor::run();
    if let Err(e) = result {
        error!("doctor failed: {}", e);
        process::exit(1);
    }
}
