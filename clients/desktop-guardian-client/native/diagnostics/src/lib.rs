use std::time::{SystemTime, UNIX_EPOCH};

use serde::Serialize;
use sysinfo::{CpuRefreshKind, RefreshKind, System, SystemExt};

#[derive(Debug, Clone, Serialize, Default)]
pub struct SystemSnapshot {
    pub cpu_percent: f64,
    pub memory_total: u64,
    pub memory_used: u64,
    pub collected_at: u64,
}

pub fn collect_snapshot() -> SystemSnapshot {
    let refresh = RefreshKind::new()
        .with_cpu(CpuRefreshKind::everything())
        .with_memory();
    let mut system = System::new_with_specifics(refresh);
    system.refresh_cpu();
    system.refresh_memory();

    let cpu_percent = system.global_cpu_info().cpu_usage() as f64;
    let memory_total = system.total_memory();
    let memory_used = system.used_memory();
    let collected_at = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as u64)
        .unwrap_or_default();

    SystemSnapshot {
        cpu_percent,
        memory_total,
        memory_used,
        collected_at,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn snapshot_has_reasonable_values() {
        let snapshot = collect_snapshot();
        assert!(snapshot.cpu_percent >= 0.0);
        assert!(snapshot.memory_total > 0);
        assert!(snapshot.memory_used <= snapshot.memory_total);
        assert!(snapshot.collected_at > 0);
    }
}
