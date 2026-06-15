use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use serde::{Deserialize, Serialize};

use crate::error::Result;

use super::event::StreamEvent;
use super::message::Message;

/// What a provider can do. Queried at startup so the agent loop can gate
/// features (tool calling, streaming) without trying and failing at runtime.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Capabilities {
    pub streaming: bool,
    pub tool_calling: bool,
    pub system_messages: bool,
    pub max_context_tokens: u32,
}

/// Inference parameters that every provider must honour.
///
/// Providers map these into their own request format; the agent loop never
/// passes provider-specific params.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SamplingParams {
    pub temperature: f32,
    pub top_p: f32,
    pub max_tokens: u32,
    pub stop: Vec<String>,
}

impl Default for SamplingParams {
    fn default() -> Self {
        Self {
            temperature: 0.7,
            top_p: 0.95,
            max_tokens: 2048,
            stop: vec![],
        }
    }
}

/// A lightweight cancellation token backed by an atomic flag.
///
/// Clone it cheaply; cancel() is safe to call from any thread.
#[derive(Clone, Default)]
pub struct CancelToken(Arc<AtomicBool>);

impl CancelToken {
    pub fn new() -> Self {
        Self(Arc::new(AtomicBool::new(false)))
    }

    pub fn cancel(&self) {
        self.0.store(true, Ordering::SeqCst);
    }

    pub fn is_cancelled(&self) -> bool {
        self.0.load(Ordering::SeqCst)
    }
}

/// The provider-agnostic abstraction the agent loop depends on.
///
/// INVARIANT: the agent loop (nura-core::agent, Phase 25+) must ONLY use
/// this trait and the canonical IR types in nura-core::provider. It must
/// never import a concrete provider type. Concrete providers live outside
/// nura-core and depend on it, not the other way around.
///
/// Every implementation must be Send + Sync so it can be boxed and shared
/// across async tasks.
pub trait Provider: Send + Sync {
    /// Stable, human-readable name (e.g. "local", "anthropic", "openai").
    fn name(&self) -> &str;

    /// Static capabilities for this provider.
    fn capabilities(&self) -> Capabilities;

    /// Generate a completion, yielding a stream of canonical events.
    ///
    /// Implementations should poll `cancel.is_cancelled()` between chunks
    /// and emit `StreamEvent::Done { stop_reason: StopReason::Cancel }` and
    /// return when the token is set.
    fn complete(
        &self,
        messages: &[Message],
        params: &SamplingParams,
        cancel: &CancelToken,
    ) -> Box<dyn Iterator<Item = Result<StreamEvent>> + Send + '_>;
}

/// A stub provider used in tests and before a real provider is wired up.
pub struct StubProvider;

impl Provider for StubProvider {
    fn name(&self) -> &str {
        "stub"
    }

    fn capabilities(&self) -> Capabilities {
        Capabilities {
            streaming: true,
            tool_calling: false,
            system_messages: false,
            max_context_tokens: 4096,
        }
    }

    fn complete(
        &self,
        messages: &[Message],
        _params: &SamplingParams,
        cancel: &CancelToken,
    ) -> Box<dyn Iterator<Item = Result<StreamEvent>> + Send + '_> {
        use super::event::StreamEvent;
        use super::message::StopReason;

        let last_text = messages
            .iter()
            .rev()
            .find_map(|m| {
                m.parts.iter().find_map(|p| match p {
                    super::message::ContentPart::Text { text } => Some(text.clone()),
                    _ => None,
                })
            })
            .unwrap_or_default();

        let echo = format!("[stub] echo: {}", last_text);
        let cancel = cancel.clone();

        let events: Vec<Result<StreamEvent>> = if cancel.is_cancelled() {
            vec![Ok(StreamEvent::done(StopReason::Cancel))]
        } else {
            vec![
                Ok(StreamEvent::token(echo)),
                Ok(StreamEvent::done(StopReason::EndOfTurn)),
            ]
        };

        Box::new(events.into_iter())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::provider::message::Message;

    #[test]
    fn stub_provider_echoes() {
        let provider = StubProvider;
        let msgs = vec![Message::user("ping")];
        let params = SamplingParams::default();
        let cancel = CancelToken::new();

        let events: Vec<_> = provider.complete(&msgs, &params, &cancel).collect();
        assert_eq!(events.len(), 2);

        match events[0].as_ref().unwrap() {
            StreamEvent::TokenDelta { text } => {
                assert!(text.contains("ping"), "should echo the message")
            }
            e => panic!("expected TokenDelta, got {:?}", e),
        }

        match events[1].as_ref().unwrap() {
            StreamEvent::Done { .. } => {}
            e => panic!("expected Done, got {:?}", e),
        }
    }

    #[test]
    fn cancel_token_propagates() {
        let token = CancelToken::new();
        let clone = token.clone();
        token.cancel();
        assert!(clone.is_cancelled());
    }

    #[test]
    fn sampling_params_defaults() {
        let p = SamplingParams::default();
        assert_eq!(p.temperature, 0.7);
        assert_eq!(p.max_tokens, 2048);
    }
}
