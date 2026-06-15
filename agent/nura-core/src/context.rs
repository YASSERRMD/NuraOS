use crate::provider::message::{ContentPart, Message, Role};

// ---------------------------------------------------------------------------
// Token estimator
// ---------------------------------------------------------------------------

/// Estimate the token count of a single message.
///
/// Uses the approximation: tokens = ceil(char_count / 4) + 4, where the +4
/// accounts for role and message-framing overhead. This matches the rule of
/// thumb for English text in BPE tokenizers; actual counts may differ for
/// code or non-ASCII content.
pub fn estimate_tokens(msg: &Message) -> u32 {
    let char_count: usize = msg
        .parts
        .iter()
        .map(|part| match part {
            ContentPart::Text { text } => text.len(),
            ContentPart::ToolCallRequest {
                name, arguments, ..
            } => name.len() + arguments.to_string().len() + 16,
            ContentPart::ToolCallResult { output, error, .. } => {
                output.to_string().len()
                    + error.as_deref().map(str::len).unwrap_or(0)
                    + 8
            }
        })
        .sum();

    // ceil(chars / 4) + 4 overhead
    ((char_count + 3) / 4 + 4) as u32
}

/// Sum token estimates for a slice of messages.
pub fn estimate_total(msgs: &[Message]) -> u32 {
    msgs.iter().map(estimate_tokens).sum()
}

// ---------------------------------------------------------------------------
// Retention policy
// ---------------------------------------------------------------------------

/// What to do with conversation turns that fall outside the recent window
/// when the context is full.
#[derive(Debug, Clone)]
pub enum OlderTurnsPolicy {
    /// Silently drop turns that do not fit. The most-recent turns are always
    /// preferred over older ones.
    Drop,
}

/// Configuration for the context assembler.
#[derive(Debug, Clone)]
pub struct ContextPolicy {
    /// Maximum tokens the assembled context may contain (system + history).
    /// A safe value is the model's context length minus the expected max
    /// response length.
    pub max_tokens: u32,
    /// Maximum number of non-system messages kept verbatim (most-recent first).
    /// Messages beyond this count are subject to `older_turns`.
    pub recent_messages: u32,
    /// What to do with turns that exceed `recent_messages` or `max_tokens`.
    pub older_turns: OlderTurnsPolicy,
}

impl Default for ContextPolicy {
    fn default() -> Self {
        Self {
            max_tokens: 3800,
            recent_messages: 40,
            older_turns: OlderTurnsPolicy::Drop,
        }
    }
}

// ---------------------------------------------------------------------------
// Context assembler
// ---------------------------------------------------------------------------

/// Assemble a token-bounded list of messages from conversation history.
///
/// Assembly rules (applied in order):
/// 1. System messages are always included and not counted toward
///    `recent_messages`.
/// 2. Of the non-system messages, keep at most `recent_messages` (newest
///    first). Older messages are handled per `older_turns`.
/// 3. Within the kept set, drop the oldest messages until the total fits
///    within `max_tokens`.
///
/// The returned slice is in chronological order (system messages first, then
/// oldest kept turn message, ... , newest).
pub fn assemble_context(history: &[Message], policy: &ContextPolicy) -> Vec<Message> {
    let system_msgs: Vec<Message> = history
        .iter()
        .filter(|m| m.role == Role::System)
        .cloned()
        .collect();

    let turn_msgs: Vec<&Message> = history
        .iter()
        .filter(|m| m.role != Role::System)
        .collect();

    let system_tokens: u32 = system_msgs.iter().map(estimate_tokens).sum();
    let token_budget = policy.max_tokens.saturating_sub(system_tokens);

    // Apply recent_messages cap (keep last N).
    let recent_start = turn_msgs
        .len()
        .saturating_sub(policy.recent_messages as usize);
    let candidates = &turn_msgs[recent_start..];

    // Within the candidates, drop oldest until tokens fit (greedy from newest).
    let mut kept: Vec<Message> = Vec::with_capacity(candidates.len());
    let mut used_tokens = 0u32;
    for msg in candidates.iter().rev() {
        let t = estimate_tokens(msg);
        if used_tokens.saturating_add(t) > token_budget {
            break;
        }
        used_tokens += t;
        kept.push((*msg).clone());
    }
    kept.reverse(); // restore chronological order

    let mut assembled = system_msgs;
    assembled.extend(kept);
    assembled
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::provider::message::Message;

    fn user(text: &str) -> Message {
        Message::user(text)
    }

    fn assistant(text: &str) -> Message {
        Message::assistant(text)
    }

    fn system(text: &str) -> Message {
        Message::system(text)
    }

    // ---- estimate_tokens ----

    #[test]
    fn empty_message_has_minimum_tokens() {
        let msg = user("");
        let t = estimate_tokens(&msg);
        assert!(t >= 4, "minimum overhead should be at least 4 tokens");
    }

    #[test]
    fn longer_message_has_more_tokens() {
        let short = estimate_tokens(&user("hi"));
        let long = estimate_tokens(&user(&"x".repeat(400)));
        assert!(long > short);
    }

    #[test]
    fn estimate_total_sums_messages() {
        let msgs = vec![user("hello"), assistant("world")];
        let total = estimate_total(&msgs);
        assert_eq!(
            total,
            estimate_tokens(&msgs[0]) + estimate_tokens(&msgs[1])
        );
    }

    // ---- assemble_context ----

    #[test]
    fn system_message_always_preserved() {
        let history = vec![
            system("you are an assistant"),
            user("hello"),
            assistant("hi"),
        ];
        let policy = ContextPolicy {
            max_tokens: 10000,
            recent_messages: 2,
            older_turns: OlderTurnsPolicy::Drop,
        };
        let ctx = assemble_context(&history, &policy);
        assert_eq!(ctx[0].role, Role::System);
    }

    #[test]
    fn recent_messages_cap_applied() {
        let history: Vec<Message> = (0..20).map(|i| user(&format!("msg {}", i))).collect();
        let policy = ContextPolicy {
            max_tokens: 10000,
            recent_messages: 5,
            older_turns: OlderTurnsPolicy::Drop,
        };
        let ctx = assemble_context(&history, &policy);
        // Only the 5 most recent (msg 15..19) should be present.
        assert_eq!(ctx.len(), 5);
    }

    #[test]
    fn token_budget_enforced() {
        // Very tight budget: only 1 turn should fit.
        let history: Vec<Message> = (0..10)
            .map(|_| user(&"w".repeat(200)))
            .collect();
        let one_msg_tokens = estimate_tokens(&history[0]);
        let policy = ContextPolicy {
            max_tokens: one_msg_tokens + 1, // room for exactly 1 turn
            recent_messages: 40,
            older_turns: OlderTurnsPolicy::Drop,
        };
        let ctx = assemble_context(&history, &policy);
        assert_eq!(ctx.len(), 1, "tight budget should keep exactly 1 message");
        assert!(
            estimate_total(&ctx) <= policy.max_tokens,
            "context must not exceed max_tokens"
        );
    }

    #[test]
    fn assembled_context_within_budget() {
        let history: Vec<Message> = (0..50)
            .flat_map(|i| vec![user(&format!("q{}", i)), assistant(&format!("a{}", i))])
            .collect();
        let policy = ContextPolicy::default();
        let ctx = assemble_context(&history, &policy);
        assert!(
            estimate_total(&ctx) <= policy.max_tokens,
            "assembled context must never exceed max_tokens"
        );
    }

    #[test]
    fn empty_history_returns_empty() {
        let ctx = assemble_context(&[], &ContextPolicy::default());
        assert!(ctx.is_empty());
    }

    #[test]
    fn chronological_order_preserved() {
        let history = vec![user("first"), assistant("second"), user("third")];
        let policy = ContextPolicy {
            max_tokens: 10000,
            recent_messages: 40,
            older_turns: OlderTurnsPolicy::Drop,
        };
        let ctx = assemble_context(&history, &policy);
        // All three fit; check order.
        let texts: Vec<&str> = ctx
            .iter()
            .flat_map(|m| {
                m.parts.iter().filter_map(|p| {
                    if let crate::provider::message::ContentPart::Text { text } = p {
                        Some(text.as_str())
                    } else {
                        None
                    }
                })
            })
            .collect();
        assert_eq!(texts, vec!["first", "second", "third"]);
    }
}
