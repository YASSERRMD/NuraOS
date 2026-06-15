use serde::{Deserialize, Serialize};

use super::message::{StopReason, Usage};

/// One event in a streaming completion response.
///
/// Providers map their native streaming protocol into this enum. The agent
/// loop must ONLY consume this enum -- never a provider-specific type.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum StreamEvent {
    /// A chunk of assistant text, appended to the current turn.
    TokenDelta { text: String },
    /// A chunk of a tool-call being assembled. `name` is only present in the
    /// first delta for a given `id`; subsequent deltas extend `arguments_chunk`.
    ToolCallDelta {
        id: String,
        name: Option<String>,
        arguments_chunk: String,
    },
    /// Cumulative token counts from the provider (may arrive mid-stream or at
    /// the end, depending on the backend).
    Usage(Usage),
    /// The provider has finished the turn.
    Done { stop_reason: StopReason },
    /// The provider encountered an unrecoverable error; the stream ends here.
    Error { message: String },
}

impl StreamEvent {
    pub fn token(text: impl Into<String>) -> Self {
        Self::TokenDelta { text: text.into() }
    }

    pub fn done(stop_reason: StopReason) -> Self {
        Self::Done { stop_reason }
    }

    pub fn error(message: impl Into<String>) -> Self {
        Self::Error {
            message: message.into(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn token_delta_roundtrip() {
        let ev = StreamEvent::token("hello");
        let json = serde_json::to_string(&ev).unwrap();
        let ev2: StreamEvent = serde_json::from_str(&json).unwrap();
        match ev2 {
            StreamEvent::TokenDelta { text } => assert_eq!(text, "hello"),
            _ => panic!("wrong variant"),
        }
    }

    #[test]
    fn done_roundtrip() {
        let ev = StreamEvent::done(StopReason::EndOfTurn);
        let json = serde_json::to_string(&ev).unwrap();
        let ev2: StreamEvent = serde_json::from_str(&json).unwrap();
        match ev2 {
            StreamEvent::Done { stop_reason } => {
                assert_eq!(stop_reason, StopReason::EndOfTurn)
            }
            _ => panic!("wrong variant"),
        }
    }
}
