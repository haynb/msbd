use std::{collections::HashMap, time::Instant};

use anyhow::Result;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sysinfo::{ProcessExt, System, SystemExt};
use tokio::{
    select,
    sync::{mpsc, watch},
    task::JoinHandle,
    time::{sleep, Duration},
};
use tracing::{debug, warn};

const DEFAULT_POLL_MS: u64 = 2_000;
const DEFAULT_COOLDOWN_MS: u64 = 5_000;

#[derive(Debug, Clone, Deserialize)]
pub struct GuardianPolicy {
    #[serde(default)]
    pub process_watchlist: Vec<String>,
    #[serde(default)]
    pub vm_indicators: Vec<String>,
    #[serde(default = "default_poll_ms")]
    pub poll_interval_ms: u64,
    #[serde(default = "default_cooldown_ms")]
    pub cooldown_ms: u64,
}

#[derive(Debug, Clone, Serialize)]
pub struct GuardianEvent {
    pub kind: String,
    pub indicator: String,
    pub process_name: Option<String>,
    pub confidence: f32,
    pub occurred_at: DateTime<Utc>,
}

pub struct GuardianService {
    shutdown: watch::Sender<bool>,
    join_handle: JoinHandle<()>,
}

impl GuardianService {
    pub fn spawn(policy: GuardianPolicy) -> (Self, mpsc::Receiver<GuardianEvent>) {
        let (shutdown_tx, shutdown_rx) = watch::channel(false);
        let (event_tx, event_rx) = mpsc::channel(32);
        let join_handle = tokio::spawn(run_guardian(policy, shutdown_rx, event_tx));
        (
            Self {
                shutdown: shutdown_tx,
                join_handle,
            },
            event_rx,
        )
    }

    pub async fn stop(self) {
        let _ = self.shutdown.send(true);
        if let Err(err) = self.join_handle.await {
            warn!(error = %err, "guardian runtime exit error");
        }
    }
}

async fn run_guardian(
    policy: GuardianPolicy,
    mut shutdown: watch::Receiver<bool>,
    events: mpsc::Sender<GuardianEvent>,
) {
    let mut system = System::new_all();
    let poll = Duration::from_millis(policy.poll_interval_ms.max(250));
    let cooldown = Duration::from_millis(policy.cooldown_ms.max(500));
    let mut last_fire: HashMap<String, Instant> = HashMap::new();
    let watch_terms: Vec<String> = policy
        .process_watchlist
        .iter()
        .map(|term| term.to_ascii_lowercase())
        .collect();
    let vm_terms: Vec<String> = policy
        .vm_indicators
        .iter()
        .map(|term| term.to_ascii_lowercase())
        .collect();

    loop {
        select! {
            changed = shutdown.changed() => {
                if changed.is_ok() && *shutdown.borrow() {
                    break;
                }
            }
            _ = sleep(poll) => {
                system.refresh_processes();
                for process in system.processes().values() {
                    let name_lc = process.name().to_ascii_lowercase();
                    if matches_indicator(&name_lc, &watch_terms) {
                        if should_emit(&mut last_fire, format!("proc:{name_lc}"), cooldown) {
                            let _ = events.send(GuardianEvent {
                                kind: "recorder".into(),
                                indicator: name_lc.clone(),
                                process_name: Some(process.name().to_string()),
                                confidence: 0.95,
                                occurred_at: Utc::now(),
                            }).await;
                        }
                    }
                    if matches_indicator(&name_lc, &vm_terms) {
                        if should_emit(&mut last_fire, format!("vm:{name_lc}"), cooldown) {
                            let _ = events.send(GuardianEvent {
                                kind: "vm".into(),
                                indicator: name_lc.clone(),
                                process_name: Some(process.name().to_string()),
                                confidence: 0.8,
                                occurred_at: Utc::now(),
                            }).await;
                        }
                    }
                }
            }
        }
    }
    debug!("guardian runtime stopped");
}

fn matches_indicator(name: &str, indicators: &[String]) -> bool {
    indicators.iter().any(|needle| name.contains(needle))
}

fn should_emit(last_fire: &mut HashMap<String, Instant>, key: String, cooldown: Duration) -> bool {
    let now = Instant::now();
    match last_fire.get(&key) {
        Some(previous) if now.duration_since(*previous) < cooldown => false,
        _ => {
            last_fire.insert(key, now);
            true
        }
    }
}

fn default_poll_ms() -> u64 {
    DEFAULT_POLL_MS
}

fn default_cooldown_ms() -> u64 {
    DEFAULT_COOLDOWN_MS
}

pub fn parse_policy(json: &str) -> Result<GuardianPolicy> {
    let policy = serde_json::from_str(json)?;
    Ok(policy)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{collections::HashMap, time::Duration};

    #[test]
    fn cooldown_blocks_duplicates() {
        let mut last = HashMap::new();
        assert!(should_emit(
            &mut last,
            "proc:obs".into(),
            Duration::from_millis(100)
        ));
        assert!(!should_emit(
            &mut last,
            "proc:obs".into(),
            Duration::from_millis(100)
        ));
    }
}
