pub mod local;

#[cfg(feature = "remote-providers")]
pub mod anthropic;

pub use local::LocalProvider;

#[cfg(feature = "remote-providers")]
pub use anthropic::AnthropicProvider;
