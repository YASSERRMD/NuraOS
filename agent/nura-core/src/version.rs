pub const VERSION: &str = env!("CARGO_PKG_VERSION");
pub const NAME: &str = "nura-agent";

pub fn version_string() -> String {
    format!("{} {}", NAME, VERSION)
}
