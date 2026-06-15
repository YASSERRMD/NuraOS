use std::process;

use nura_core::version;

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
    println!("[nura-agent] starting ({})", version::version_string());
    println!("[nura-agent] idle -- inference and REPL arrive in later phases");

    // Block until interrupted.
    loop {
        std::thread::sleep(std::time::Duration::from_secs(60));
    }
}

fn cmd_doctor() {
    crate::doctor::run();
}
