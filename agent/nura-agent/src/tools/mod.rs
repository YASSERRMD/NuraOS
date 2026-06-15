pub mod fs_read;
pub mod net_status;
pub mod system_info;
pub mod time_now;

use nura_core::tool::ToolRegistry;

/// Register all built-in read-only tools and add them to the allowlist.
///
/// Called from the agent `run` command once the tool registry is wired up.
#[allow(dead_code)]
pub fn register_all(registry: &mut ToolRegistry) {
    registry.register(system_info::SystemInfoTool);
    registry.allowlist("system.info");

    registry.register(fs_read::FsReadTool::default());
    registry.allowlist("fs.read");

    registry.register(net_status::NetStatusTool);
    registry.allowlist("net.status");

    registry.register(time_now::TimeNowTool);
    registry.allowlist("time.now");
}
