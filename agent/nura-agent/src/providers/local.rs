use std::io::{BufRead, BufReader};
use std::time::Duration;

use nura_core::error::{NuraError, Result};
use nura_core::provider::message::{ContentPart, Message, StopReason, Usage};
use nura_core::provider::traits::{CancelToken, Capabilities, Provider, SamplingParams};
use nura_core::provider::StreamEvent;
use tracing::{debug, warn};

/// Default llama-server base URL (localhost, port 8081).
pub const DEFAULT_BASE_URL: &str = "http://127.0.0.1:8081";

/// LocalProvider drives llama-server over plain HTTP on 127.0.0.1.
///
/// The server must be running and healthy before the first call to `complete()`.
/// Use `wait_for_health()` to block until the server is ready.
pub struct LocalProvider {
    base_url: String,
}

impl LocalProvider {
    pub fn new(base_url: impl Into<String>) -> Self {
        Self {
            base_url: base_url.into(),
        }
    }

    fn completion_url(&self) -> String {
        format!("{}/completion", self.base_url.trim_end_matches('/'))
    }
}

impl Provider for LocalProvider {
    fn name(&self) -> &str {
        "local"
    }

    fn capabilities(&self) -> Capabilities {
        Capabilities {
            streaming: true,
            tool_calling: false,
            system_messages: true,
            max_context_tokens: 2048,
        }
    }

    fn complete(
        &self,
        messages: &[Message],
        params: &SamplingParams,
        cancel: &CancelToken,
    ) -> Box<dyn Iterator<Item = Result<StreamEvent>> + Send + '_> {
        if cancel.is_cancelled() {
            return Box::new(std::iter::once(Ok(StreamEvent::done(StopReason::Cancel))));
        }

        let prompt = messages_to_prompt(messages);
        let body = build_completion_body(&prompt, params);

        debug!(
            prompt_chars = prompt.len(),
            "local provider: sending completion request"
        );

        let response = match ureq::post(&self.completion_url())
            .set("Content-Type", "application/json")
            .timeout(Duration::from_secs(120))
            .send_string(&body)
        {
            Ok(r) => r,
            Err(e) => {
                return Box::new(std::iter::once(Err(NuraError::Provider {
                    provider: "local".into(),
                    detail: format!("HTTP request failed: {}", e),
                })));
            }
        };

        let reader = BufReader::new(response.into_reader());
        Box::new(LocalStream {
            reader,
            cancel: cancel.clone(),
            done: false,
        })
    }
}

/// Block until llama-server's /health endpoint returns 200 or the timeout
/// is exceeded. Called from the agent `run` command in Phase 46+.
#[allow(dead_code)]
pub fn wait_for_health(base_url: &str, timeout_secs: u64) -> Result<()> {
    let health_url = format!("{}/health", base_url.trim_end_matches('/'));
    let deadline = std::time::Instant::now() + Duration::from_secs(timeout_secs);

    loop {
        if std::time::Instant::now() >= deadline {
            return Err(NuraError::Provider {
                provider: "local".into(),
                detail: format!(
                    "llama-server not healthy after {}s at {}",
                    timeout_secs, health_url
                ),
            });
        }

        match ureq::get(&health_url)
            .timeout(Duration::from_secs(2))
            .call()
        {
            Ok(resp) if resp.status() == 200 => {
                debug!("local provider: llama-server healthy");
                return Ok(());
            }
            Ok(resp) => {
                debug!("local provider: health check status {}", resp.status());
            }
            Err(e) => {
                debug!("local provider: health check error: {}", e);
            }
        }

        std::thread::sleep(Duration::from_millis(500));
    }
}

// ---- internal ----

/// Flatten a message slice into a simple prompt string.
///
/// Uses a ChatML-like format that llama-server's /completion endpoint accepts
/// without the OpenAI compat layer. Phase 17 keeps this simple; Phase 25
/// upgrades to the v1/chat/completions endpoint with proper role tokens.
fn messages_to_prompt(messages: &[Message]) -> String {
    let mut out = String::new();
    for msg in messages {
        let role_tag = match msg.role {
            nura_core::provider::message::Role::System => "<|system|>",
            nura_core::provider::message::Role::User => "<|user|>",
            nura_core::provider::message::Role::Assistant => "<|assistant|>",
            nura_core::provider::message::Role::Tool => "<|tool|>",
        };
        out.push_str(role_tag);
        out.push('\n');
        for part in &msg.parts {
            if let ContentPart::Text { text } = part {
                out.push_str(text);
                out.push('\n');
            }
        }
    }
    out.push_str("<|assistant|>\n");
    out
}

/// Build the JSON body for POST /completion.
fn build_completion_body(prompt: &str, params: &SamplingParams) -> String {
    let stop_json = params
        .stop
        .iter()
        .map(|s| format!("\"{}\"", s.replace('"', "\\\"")))
        .collect::<Vec<_>>()
        .join(",");

    format!(
        r#"{{"prompt":{prompt_json},"temperature":{temp},"top_p":{top_p},"n_predict":{max_tokens},"stop":[{stop}],"stream":true}}"#,
        prompt_json = serde_json::to_string(prompt).unwrap_or_else(|_| "\"\"".into()),
        temp = params.temperature,
        top_p = params.top_p,
        max_tokens = params.max_tokens,
        stop = stop_json,
    )
}

/// Parse one SSE data line from llama-server's /completion stream.
///
/// Returns `None` for blank or non-data lines.
fn parse_sse_line(line: &str) -> Option<Result<StreamEvent>> {
    let json_str = line.strip_prefix("data: ")?;
    if json_str.trim().is_empty() {
        return None;
    }

    let v: serde_json::Value = match serde_json::from_str(json_str) {
        Ok(v) => v,
        Err(e) => {
            warn!("local provider: bad SSE JSON: {}: {}", e, json_str);
            return Some(Err(NuraError::Provider {
                provider: "local".into(),
                detail: format!("invalid SSE JSON: {}", e),
            }));
        }
    };

    if v.get("stop").and_then(|s| s.as_bool()).unwrap_or(false) {
        let usage = Usage {
            prompt_tokens: v
                .get("tokens_evaluated")
                .and_then(|t| t.as_u64())
                .unwrap_or(0) as u32,
            completion_tokens: v
                .get("tokens_predicted")
                .and_then(|t| t.as_u64())
                .unwrap_or(0) as u32,
        };
        return Some(Ok(StreamEvent::Usage(usage)));
    }

    let content = v
        .get("content")
        .and_then(|c| c.as_str())
        .unwrap_or("")
        .to_string();

    if content.is_empty() {
        None
    } else {
        Some(Ok(StreamEvent::token(content)))
    }
}

/// Streaming iterator over SSE events from llama-server.
struct LocalStream {
    reader: BufReader<Box<dyn std::io::Read + Send + Sync + 'static>>,
    cancel: CancelToken,
    done: bool,
}

impl Iterator for LocalStream {
    type Item = Result<StreamEvent>;

    fn next(&mut self) -> Option<Self::Item> {
        if self.done {
            return None;
        }

        if self.cancel.is_cancelled() {
            self.done = true;
            return Some(Ok(StreamEvent::done(StopReason::Cancel)));
        }

        loop {
            let mut line = String::new();
            match self.reader.read_line(&mut line) {
                Ok(0) => {
                    // EOF: server closed the stream.
                    self.done = true;
                    return Some(Ok(StreamEvent::done(StopReason::EndOfTurn)));
                }
                Ok(_) => {}
                Err(e) => {
                    self.done = true;
                    return Some(Err(NuraError::Provider {
                        provider: "local".into(),
                        detail: format!("stream read error: {}", e),
                    }));
                }
            }

            if self.cancel.is_cancelled() {
                self.done = true;
                return Some(Ok(StreamEvent::done(StopReason::Cancel)));
            }

            match parse_sse_line(line.trim()) {
                None => continue, // blank / non-data line
                Some(Ok(StreamEvent::Usage(u))) => {
                    // Emit usage then done.
                    // We emit usage now; done will come from EOF or the loop ending.
                    self.done = true;
                    return Some(Ok(StreamEvent::Usage(u)));
                }
                Some(ev) => return Some(ev),
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn build_body_contains_stream() {
        let body = build_completion_body("hello", &SamplingParams::default());
        assert!(
            body.contains("\"stream\":true"),
            "body must request streaming"
        );
        assert!(
            body.contains("\"temperature\":0.7"),
            "should include temperature"
        );
    }

    #[test]
    fn messages_to_prompt_roles() {
        use nura_core::provider::message::Message;
        let msgs = vec![Message::system("You are helpful."), Message::user("hi")];
        let prompt = messages_to_prompt(&msgs);
        assert!(prompt.contains("<|system|>"), "should tag system role");
        assert!(
            prompt.contains("You are helpful."),
            "should include system text"
        );
        assert!(prompt.contains("<|user|>"), "should tag user role");
        assert!(
            prompt.ends_with("<|assistant|>\n"),
            "should end with assistant tag"
        );
    }

    #[test]
    fn parse_sse_token_line() {
        let line = r#"data: {"content": "hello", "stop": false}"#;
        let ev = parse_sse_line(line).unwrap().unwrap();
        match ev {
            StreamEvent::TokenDelta { text } => assert_eq!(text, "hello"),
            _ => panic!("expected TokenDelta"),
        }
    }

    #[test]
    fn parse_sse_stop_line() {
        let line =
            r#"data: {"content": "", "stop": true, "tokens_predicted": 5, "tokens_evaluated": 10}"#;
        let ev = parse_sse_line(line).unwrap().unwrap();
        match ev {
            StreamEvent::Usage(u) => {
                assert_eq!(u.prompt_tokens, 10);
                assert_eq!(u.completion_tokens, 5);
            }
            _ => panic!("expected Usage"),
        }
    }

    #[test]
    fn parse_sse_blank_returns_none() {
        assert!(parse_sse_line("").is_none());
        assert!(parse_sse_line(": keep-alive").is_none());
    }

    #[test]
    fn cancel_token_before_complete() {
        let provider = LocalProvider::new("http://127.0.0.1:8081");
        let msgs = vec![Message::user("ping")];
        let params = SamplingParams::default();
        let cancel = CancelToken::new();
        cancel.cancel();

        let events: Vec<_> = provider.complete(&msgs, &params, &cancel).collect();
        assert_eq!(events.len(), 1);
        match events[0].as_ref().unwrap() {
            StreamEvent::Done { stop_reason } => {
                assert_eq!(*stop_reason, StopReason::Cancel)
            }
            e => panic!("expected Done(Cancel), got {:?}", e),
        }
    }

    /// Integration test: requires llama-server running on localhost:8081.
    /// Run with: LLAMA_SERVER_RUNNING=1 cargo test -- --ignored
    #[test]
    #[ignore]
    fn integration_streams_real_tokens() {
        if std::env::var("LLAMA_SERVER_RUNNING").is_err() {
            return;
        }
        let provider = LocalProvider::new(DEFAULT_BASE_URL);
        wait_for_health(DEFAULT_BASE_URL, 5).expect("llama-server not healthy");

        let msgs = vec![Message::user("Say the word 'hello' and nothing else.")];
        let params = SamplingParams {
            max_tokens: 20,
            ..Default::default()
        };
        let cancel = CancelToken::new();
        let events: Vec<_> = provider.complete(&msgs, &params, &cancel).collect();

        let tokens: Vec<_> = events
            .iter()
            .filter_map(|r| r.as_ref().ok())
            .filter(|e| matches!(e, StreamEvent::TokenDelta { .. }))
            .collect();

        assert!(!tokens.is_empty(), "should receive at least one token");
    }
}
