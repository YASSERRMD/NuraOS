use std::io::{self, BufReader};

use nura_core::repl::{run_repl, StubCore};

use crate::cli::init_tracing;

pub fn cmd_repl() {
    init_tracing();

    let stdin = io::stdin();
    let stdout = io::stdout();
    let reader = BufReader::new(stdin.lock());
    let mut writer = stdout.lock();

    let mut core = StubCore;
    if let Err(e) = run_repl(reader, &mut writer, &mut core) {
        eprintln!("repl error: {}", e);
        std::process::exit(e.exit_code());
    }
}
