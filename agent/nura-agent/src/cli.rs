use std::process;

use nura_core::config::Config;
use nura_core::logging::{init as init_logging, LoggingConfig};
use nura_core::telemetry::TurnId;
use nura_core::version;
use tracing::{error, info};

pub fn run() {
    let args: Vec<String> = std::env::args().collect();
    let subcommand = args.get(1).map(String::as_str).unwrap_or("run");

    match subcommand {
        "run" => cmd_run(),
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

fn init_tracing() {
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
    println!("    version    Print the version string");
    println!("    doctor     Check environment and configuration");
    println!("    help       Print this help message");
}

fn cmd_run() {
    init_tracing();

    let turn_id = TurnId::new();
    info!(turn_id = %turn_id, "nura-agent starting ({})", version::version_string());
    info!(turn_id = %turn_id, "idle -- inference and REPL arrive in later phases");

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
