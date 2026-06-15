use std::io::{self, BufRead, Write};

use tracing::{debug, info, warn};

use crate::error::{NuraError, Result};
use crate::telemetry::TurnId;

const MAX_LINE_BYTES: usize = 64 * 1024; // 64 KB

/// A single request from the REPL user.
#[derive(Debug)]
pub enum ReplInput {
    /// A user message to send to the agent core.
    Message(String),
    /// A control command prefixed with ':'.
    Control(ControlCmd),
    /// Empty line -- ignored.
    Empty,
}

#[derive(Debug)]
pub enum ControlCmd {
    Help,
    Provider(Option<String>),
    Tools,
    Clear,
    Quit,
    Unknown(String),
}

/// Trait that the REPL uses to invoke the agent core.
/// Implemented by the real agent loop (Phase 25+); a stub is used until then.
pub trait AgentCore: Send {
    fn complete(&mut self, turn_id: &TurnId, message: &str) -> Result<String>;
    fn list_tools(&self) -> Vec<String>;
    fn active_provider(&self) -> String;
    fn set_provider(&mut self, name: &str) -> Result<()>;
    fn clear_session(&mut self);
}

/// Stub implementation used before the real agent core lands.
pub struct StubCore;

impl AgentCore for StubCore {
    fn complete(&mut self, turn_id: &TurnId, message: &str) -> Result<String> {
        debug!(turn_id = %turn_id, input = %message, "stub complete called");
        Ok(format!("[stub] echo: {}", message))
    }
    fn list_tools(&self) -> Vec<String> {
        vec!["(no tools registered yet -- Phase 23+)".to_string()]
    }
    fn active_provider(&self) -> String {
        "local (stub)".to_string()
    }
    fn set_provider(&mut self, name: &str) -> Result<()> {
        warn!("set_provider('{}') called on stub -- no-op", name);
        Ok(())
    }
    fn clear_session(&mut self) {}
}

/// Run the interactive REPL loop, reading from `input` and writing to `output`.
///
/// Uses `core` to process messages. Runs until EOF, `:quit`, or a fatal error.
pub fn run_repl<R, W>(input: R, mut output: W, core: &mut dyn AgentCore) -> Result<()>
where
    R: BufRead,
    W: Write,
{
    writeln!(output, "NuraOS agent REPL. Type :help for commands.")
        .map_err(|e| NuraError::Io(e.to_string()))?;

    let mut line_buf = String::new();
    let mut reader = input;

    loop {
        write!(output, "> ").map_err(|e| NuraError::Io(e.to_string()))?;
        output.flush().map_err(|e| NuraError::Io(e.to_string()))?;

        line_buf.clear();
        match reader.read_line(&mut line_buf) {
            Ok(0) => {
                // EOF
                info!("REPL: EOF received; exiting");
                writeln!(output, "\n[REPL] EOF -- goodbye").ok();
                break;
            }
            Ok(_) => {}
            Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
            Err(e) => {
                warn!("REPL: read error: {}", e);
                break;
            }
        }

        // Guard against pathologically long lines.
        if line_buf.len() > MAX_LINE_BYTES {
            writeln!(
                output,
                "[REPL] line too long (max {} bytes); ignored",
                MAX_LINE_BYTES
            )
            .ok();
            continue;
        }

        let trimmed = line_buf.trim();
        match parse_input(trimmed) {
            ReplInput::Empty => continue,

            ReplInput::Control(cmd) => {
                if handle_control(cmd, &mut output, core)? {
                    break; // :quit
                }
            }

            ReplInput::Message(msg) => {
                let turn_id = TurnId::new();
                info!(turn_id = %turn_id, "REPL turn start");

                match core.complete(&turn_id, &msg) {
                    Ok(response) => {
                        writeln!(output, "{}", response)
                            .map_err(|e| NuraError::Io(e.to_string()))?;
                        info!(turn_id = %turn_id, "REPL turn complete");
                    }
                    Err(e) => {
                        warn!(turn_id = %turn_id, "REPL turn error: {}", e);
                        writeln!(output, "[error] {}", e)
                            .map_err(|e2| NuraError::Io(e2.to_string()))?;
                    }
                }
            }
        }
    }

    Ok(())
}

fn parse_input(s: &str) -> ReplInput {
    if s.is_empty() {
        return ReplInput::Empty;
    }
    if let Some(rest) = s.strip_prefix(':') {
        let mut parts = rest.splitn(2, ' ');
        let cmd = parts.next().unwrap_or("").to_lowercase();
        let arg = parts.next().map(str::trim).filter(|s| !s.is_empty());
        ReplInput::Control(match cmd.as_str() {
            "help" | "h" | "?" => ControlCmd::Help,
            "provider" | "p" => ControlCmd::Provider(arg.map(str::to_string)),
            "tools" | "t" => ControlCmd::Tools,
            "clear" | "c" => ControlCmd::Clear,
            "quit" | "q" | "exit" => ControlCmd::Quit,
            _ => ControlCmd::Unknown(cmd),
        })
    } else {
        ReplInput::Message(s.to_string())
    }
}

fn handle_control<W: Write>(
    cmd: ControlCmd,
    output: &mut W,
    core: &mut dyn AgentCore,
) -> Result<bool> {
    match cmd {
        ControlCmd::Help => {
            writeln!(
                output,
                ":help            show this help\n\
                 :provider [name] show or set active provider\n\
                 :tools           list available tools\n\
                 :clear           clear the current session\n\
                 :quit            exit the REPL"
            )
            .map_err(|e| NuraError::Io(e.to_string()))?;
        }
        ControlCmd::Provider(None) => {
            writeln!(output, "active provider: {}", core.active_provider())
                .map_err(|e| NuraError::Io(e.to_string()))?;
        }
        ControlCmd::Provider(Some(name)) => {
            core.set_provider(&name)?;
            writeln!(output, "provider set to: {}", name)
                .map_err(|e| NuraError::Io(e.to_string()))?;
        }
        ControlCmd::Tools => {
            let tools = core.list_tools();
            writeln!(output, "tools: {}", tools.join(", "))
                .map_err(|e| NuraError::Io(e.to_string()))?;
        }
        ControlCmd::Clear => {
            core.clear_session();
            writeln!(output, "session cleared").map_err(|e| NuraError::Io(e.to_string()))?;
        }
        ControlCmd::Quit => {
            writeln!(output, "goodbye").map_err(|e| NuraError::Io(e.to_string()))?;
            return Ok(true);
        }
        ControlCmd::Unknown(s) => {
            writeln!(output, "unknown command ':{}'  (try :help)", s)
                .map_err(|e| NuraError::Io(e.to_string()))?;
        }
    }
    Ok(false)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn run_with(input: &str) -> String {
        let mut core = StubCore;
        let mut out = Vec::new();
        run_repl(input.as_bytes(), &mut out, &mut core).unwrap();
        String::from_utf8(out).unwrap()
    }

    #[test]
    fn test_help_command() {
        let out = run_with(":help\n:quit\n");
        assert!(out.contains(":provider"), "help should mention :provider");
    }

    #[test]
    fn test_quit_on_eof() {
        let out = run_with("");
        assert!(out.contains("EOF"), "should exit cleanly on EOF");
    }

    #[test]
    fn test_unknown_control() {
        let out = run_with(":xyzzy\n:quit\n");
        assert!(
            out.contains("unknown command"),
            "should report unknown command"
        );
    }

    #[test]
    fn test_message_echo() {
        let out = run_with("hello world\n:quit\n");
        assert!(out.contains("[stub] echo: hello world"), "stub should echo");
    }

    #[test]
    fn test_provider_show() {
        let out = run_with(":provider\n:quit\n");
        assert!(out.contains("active provider:"), "should show provider");
    }

    #[test]
    fn test_clear() {
        let out = run_with(":clear\n:quit\n");
        assert!(out.contains("session cleared"), "should confirm clear");
    }
}
