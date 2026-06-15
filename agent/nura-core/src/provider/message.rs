use serde::{Deserialize, Serialize};

/// Canonical role tags for conversation turns.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Role {
    System,
    User,
    Assistant,
    Tool,
}

/// A single content element within a message.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum ContentPart {
    /// Plain text.
    Text { text: String },
    /// A request issued by the assistant to call a tool.
    ToolCallRequest {
        /// Opaque ID that pairs the request with its result.
        id: String,
        name: String,
        arguments: serde_json::Value,
    },
    /// The output of a tool, addressed by the call ID.
    ToolCallResult {
        call_id: String,
        output: serde_json::Value,
        /// Non-None when the tool call itself errored.
        error: Option<String>,
    },
}

impl ContentPart {
    pub fn text(s: impl Into<String>) -> Self {
        Self::Text { text: s.into() }
    }
}

/// Provider-neutral representation of one conversation turn.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Message {
    pub role: Role,
    pub parts: Vec<ContentPart>,
}

impl Message {
    pub fn user(text: impl Into<String>) -> Self {
        Self {
            role: Role::User,
            parts: vec![ContentPart::text(text)],
        }
    }

    pub fn assistant(text: impl Into<String>) -> Self {
        Self {
            role: Role::Assistant,
            parts: vec![ContentPart::text(text)],
        }
    }

    pub fn system(text: impl Into<String>) -> Self {
        Self {
            role: Role::System,
            parts: vec![ContentPart::text(text)],
        }
    }
}

/// The reason inference stopped.
#[derive(Debug, Clone, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum StopReason {
    /// Model reached a natural end-of-turn.
    #[default]
    EndOfTurn,
    /// The max-token budget was exhausted.
    MaxTokens,
    /// The model wants to call a tool before continuing.
    ToolCall,
    /// Caller cancelled via the cancellation token.
    Cancel,
    /// Provider returned an unrecoverable error mid-stream.
    Error,
}

/// Token counts reported by the provider.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Usage {
    pub prompt_tokens: u32,
    pub completion_tokens: u32,
}

impl Usage {
    pub fn total(&self) -> u32 {
        self.prompt_tokens + self.completion_tokens
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn user_message_roundtrip() {
        let m = Message::user("hello");
        let json = serde_json::to_string(&m).unwrap();
        let m2: Message = serde_json::from_str(&json).unwrap();
        assert_eq!(m2.role, Role::User);
        match &m2.parts[0] {
            ContentPart::Text { text } => assert_eq!(text, "hello"),
            _ => panic!("expected text part"),
        }
    }

    #[test]
    fn usage_total() {
        let u = Usage {
            prompt_tokens: 10,
            completion_tokens: 20,
        };
        assert_eq!(u.total(), 30);
    }
}
