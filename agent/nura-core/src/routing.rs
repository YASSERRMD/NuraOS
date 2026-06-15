use serde::{Deserialize, Serialize};
use tracing::info;

/// Attributes of the current turn that influence provider selection.
#[derive(Debug, Clone, Default)]
pub struct RoutingRequest {
    /// Turn requires tool calling (Phase 23+).
    pub needs_tools: bool,
    /// Estimated context token count (history + system + current input).
    pub context_tokens: u32,
    /// True when NURA_OFFLINE=1 or network probing indicates no connectivity.
    pub offline_only: bool,
    /// Per-turn provider override (from REPL `:provider <name>` or HTTP header).
    pub force_provider: Option<String>,
}

/// Matching conditions for one routing rule. Every non-None field must match.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RoutingConditions {
    /// If Some, the turn's `needs_tools` flag must equal this.
    pub needs_tools: Option<bool>,
    /// If Some, the turn's `context_tokens` must exceed this value.
    pub context_tokens_gt: Option<u32>,
    /// If Some, the offline_only flag must equal this.
    pub offline_only: Option<bool>,
}

impl RoutingConditions {
    pub fn matches(&self, req: &RoutingRequest) -> bool {
        if let Some(t) = self.needs_tools {
            if req.needs_tools != t {
                return false;
            }
        }
        if let Some(threshold) = self.context_tokens_gt {
            if req.context_tokens <= threshold {
                return false;
            }
        }
        if let Some(offline) = self.offline_only {
            if req.offline_only != offline {
                return false;
            }
        }
        true
    }
}

/// A single routing rule: conditions -> target provider.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RoutingRule {
    /// Human-readable name logged when this rule matches.
    pub name: String,
    pub conditions: RoutingConditions,
    /// Name of the provider to use when this rule matches.
    pub provider: String,
}

/// Ordered list of rules with a mandatory default.
///
/// Rules are evaluated in order; the first matching rule wins.
/// If no rule matches, `default_provider` is used.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RoutingPolicy {
    pub rules: Vec<RoutingRule>,
    /// Provider to use when no rule matches. Must be registered.
    pub default_provider: String,
}

impl Default for RoutingPolicy {
    /// Default policy: always use the local provider.
    fn default() -> Self {
        Self {
            rules: vec![],
            default_provider: "local".into(),
        }
    }
}

/// Outcome of a routing decision.
#[derive(Debug, Clone)]
pub struct RoutingDecision {
    /// The provider selected for this turn.
    pub provider: String,
    /// Name of the rule that produced this decision.
    pub matched_rule: String,
}

impl RoutingPolicy {
    /// Deterministically select a provider for `req`.
    ///
    /// Evaluation order:
    ///   1. `req.force_provider` (manual override -- always wins)
    ///   2. Rules in declared order (first match wins)
    ///   3. `default_provider` (fallback)
    ///
    /// The decision is logged at INFO level with the matched rule name.
    pub fn select(&self, req: &RoutingRequest) -> RoutingDecision {
        if let Some(forced) = &req.force_provider {
            let decision = RoutingDecision {
                provider: forced.clone(),
                matched_rule: "manual-override".into(),
            };
            info!(
                provider = %decision.provider,
                rule = %decision.matched_rule,
                "route selected"
            );
            return decision;
        }

        for rule in &self.rules {
            if rule.conditions.matches(req) {
                let decision = RoutingDecision {
                    provider: rule.provider.clone(),
                    matched_rule: rule.name.clone(),
                };
                info!(
                    provider = %decision.provider,
                    rule = %decision.matched_rule,
                    needs_tools = req.needs_tools,
                    context_tokens = req.context_tokens,
                    offline_only = req.offline_only,
                    "route selected"
                );
                return decision;
            }
        }

        let decision = RoutingDecision {
            provider: self.default_provider.clone(),
            matched_rule: "default".into(),
        };
        info!(
            provider = %decision.provider,
            rule = %decision.matched_rule,
            "route selected"
        );
        decision
    }

    /// Build a policy from the simple `RoutingPolicy` enum in config.
    ///
    /// This bridges the existing config representation to the new rules engine.
    pub fn from_config_policy(policy: &crate::config::RoutingPolicy) -> Self {
        use crate::config::RoutingPolicy as Cfg;
        match policy {
            Cfg::LocalFirst => Self {
                rules: vec![RoutingRule {
                    name: "local-first".into(),
                    conditions: RoutingConditions {
                        needs_tools: None,
                        context_tokens_gt: None,
                        offline_only: None,
                    },
                    provider: "local".into(),
                }],
                default_provider: "local".into(),
            },
            Cfg::RemoteFirst => Self {
                rules: vec![
                    RoutingRule {
                        name: "remote-first-tools".into(),
                        conditions: RoutingConditions {
                            needs_tools: Some(true),
                            context_tokens_gt: None,
                            offline_only: Some(false),
                        },
                        provider: "anthropic".into(),
                    },
                    RoutingRule {
                        name: "remote-first".into(),
                        conditions: RoutingConditions {
                            needs_tools: None,
                            context_tokens_gt: None,
                            offline_only: Some(false),
                        },
                        provider: "anthropic".into(),
                    },
                ],
                default_provider: "local".into(),
            },
            Cfg::LocalOnly => Self {
                rules: vec![RoutingRule {
                    name: "local-only".into(),
                    conditions: RoutingConditions {
                        needs_tools: None,
                        context_tokens_gt: None,
                        offline_only: None,
                    },
                    provider: "local".into(),
                }],
                default_provider: "local".into(),
            },
        }
    }
}

/// Session-level provider override state (set by REPL `:provider` command or HTTP).
///
/// Stored in the session; a None means no override is active (use the policy).
#[derive(Debug, Clone, Default)]
pub struct ProviderOverride(Option<String>);

impl ProviderOverride {
    pub fn set(&mut self, provider: impl Into<String>) {
        self.0 = Some(provider.into());
    }

    pub fn clear(&mut self) {
        self.0 = None;
    }

    pub fn get(&self) -> Option<&str> {
        self.0.as_deref()
    }

    /// Apply the override to a routing request (mutates `force_provider`).
    pub fn apply_to(&self, req: &mut RoutingRequest) {
        if let Some(p) = &self.0 {
            req.force_provider = Some(p.clone());
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_request() -> RoutingRequest {
        RoutingRequest::default()
    }

    #[test]
    fn default_policy_routes_to_local() {
        let policy = RoutingPolicy::default();
        let req = make_request();
        let decision = policy.select(&req);
        assert_eq!(decision.provider, "local");
        assert_eq!(decision.matched_rule, "default");
    }

    #[test]
    fn force_provider_overrides_rules() {
        let policy = RoutingPolicy {
            rules: vec![RoutingRule {
                name: "always-local".into(),
                conditions: RoutingConditions {
                    needs_tools: None,
                    context_tokens_gt: None,
                    offline_only: None,
                },
                provider: "local".into(),
            }],
            default_provider: "local".into(),
        };
        let req = RoutingRequest {
            force_provider: Some("anthropic".into()),
            ..Default::default()
        };
        let decision = policy.select(&req);
        assert_eq!(decision.provider, "anthropic");
        assert_eq!(decision.matched_rule, "manual-override");
    }

    #[test]
    fn first_matching_rule_wins() {
        let policy = RoutingPolicy {
            rules: vec![
                RoutingRule {
                    name: "needs-tools-rule".into(),
                    conditions: RoutingConditions {
                        needs_tools: Some(true),
                        context_tokens_gt: None,
                        offline_only: None,
                    },
                    provider: "anthropic".into(),
                },
                RoutingRule {
                    name: "fallthrough".into(),
                    conditions: RoutingConditions {
                        needs_tools: None,
                        context_tokens_gt: None,
                        offline_only: None,
                    },
                    provider: "openai".into(),
                },
            ],
            default_provider: "local".into(),
        };

        let req_no_tools = RoutingRequest {
            needs_tools: false,
            ..Default::default()
        };
        let req_tools = RoutingRequest {
            needs_tools: true,
            ..Default::default()
        };

        let d1 = policy.select(&req_no_tools);
        assert_eq!(d1.provider, "openai", "fallthrough rule should match");

        let d2 = policy.select(&req_tools);
        assert_eq!(d2.provider, "anthropic", "tools rule should match first");
    }

    #[test]
    fn context_length_condition() {
        let policy = RoutingPolicy {
            rules: vec![RoutingRule {
                name: "long-context".into(),
                conditions: RoutingConditions {
                    needs_tools: None,
                    context_tokens_gt: Some(4000),
                    offline_only: None,
                },
                provider: "anthropic".into(),
            }],
            default_provider: "local".into(),
        };

        let short = RoutingRequest {
            context_tokens: 100,
            ..Default::default()
        };
        let long = RoutingRequest {
            context_tokens: 5000,
            ..Default::default()
        };

        assert_eq!(policy.select(&short).provider, "local");
        assert_eq!(policy.select(&long).provider, "anthropic");
    }

    #[test]
    fn offline_only_routes_local() {
        let policy = RoutingPolicy {
            rules: vec![
                RoutingRule {
                    name: "offline-guard".into(),
                    conditions: RoutingConditions {
                        needs_tools: None,
                        context_tokens_gt: None,
                        offline_only: Some(true),
                    },
                    provider: "local".into(),
                },
                RoutingRule {
                    name: "cloud".into(),
                    conditions: RoutingConditions {
                        needs_tools: None,
                        context_tokens_gt: None,
                        offline_only: Some(false),
                    },
                    provider: "anthropic".into(),
                },
            ],
            default_provider: "local".into(),
        };

        let offline = RoutingRequest {
            offline_only: true,
            ..Default::default()
        };
        let online = RoutingRequest {
            offline_only: false,
            ..Default::default()
        };

        assert_eq!(policy.select(&offline).provider, "local");
        assert_eq!(policy.select(&online).provider, "anthropic");
    }

    #[test]
    fn provider_override_apply_to() {
        let mut ovr = ProviderOverride::default();
        ovr.set("openai");

        let mut req = RoutingRequest::default();
        ovr.apply_to(&mut req);
        assert_eq!(req.force_provider.as_deref(), Some("openai"));

        ovr.clear();
        let mut req2 = RoutingRequest::default();
        ovr.apply_to(&mut req2);
        assert!(req2.force_provider.is_none());
    }

    #[test]
    fn from_config_local_first() {
        let policy = RoutingPolicy::from_config_policy(&crate::config::RoutingPolicy::LocalFirst);
        let req = make_request();
        assert_eq!(policy.select(&req).provider, "local");
    }

    #[test]
    fn from_config_local_only() {
        let policy = RoutingPolicy::from_config_policy(&crate::config::RoutingPolicy::LocalOnly);
        let req = make_request();
        assert_eq!(policy.select(&req).provider, "local");
    }
}
