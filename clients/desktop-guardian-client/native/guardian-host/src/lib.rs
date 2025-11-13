use std::{
    collections::HashMap,
    path::Path,
    sync::Arc,
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use audio::{self, AudioChunk, AudioDeviceDescriptor, CaptureOptions, CaptureSession};
use base64::engine::general_purpose::STANDARD as Base64;
use base64::Engine as _;
use diagnostics::collect_snapshot;
use guardian::{self, GuardianService};
use guardian_net::stream_client::{spawn_stream, StreamConfig, StreamError, StreamSender};
use guardian_storage::cache::SecureCache;
use napi::bindgen_prelude::{
    ErrorStrategy, JsFunction, Result as NapiResult, Status, ThreadSafeCallContext,
    ThreadsafeFunction, ThreadsafeFunctionCallMode,
};
use napi_derive::napi;
use parking_lot::Mutex;
use screenshot::{
    self, CaptureRequest as NativeCaptureRequest, RedactionRect as NativeScreenshotRect,
};
use tokio::{
    runtime::Runtime,
    sync::{mpsc, watch},
};
use tracing::{error, info, warn};

#[derive(Clone)]
struct ActiveDeviceInfo {
    id: String,
    label: String,
    sample_rate: u32,
    chunk_millis: u32,
}

struct AudioRuntime {
    session: CaptureSession,
    shutdown: watch::Sender<bool>,
    join_handle: tokio::task::JoinHandle<()>,
}

impl AudioRuntime {
    fn stop(self) {
        let _ = self.shutdown.send(true);
        self.join_handle.abort();
        self.session.handle.stop();
    }
}

const CACHE_TTL_SECONDS: u64 = 30 * 60;
const RUNTIME_CACHE_FILE: &str = "runtime_config.bin";

#[derive(Debug, Clone, Default, serde::Serialize, serde::Deserialize)]
struct RuntimePreferences {
    sample_rate: Option<u32>,
    chunk_millis: Option<u32>,
}

#[derive(Debug, Clone, Default)]
struct HostMetrics {
    last_chunk_at: Option<u64>,
    last_chunk_millis: u32,
    jitter_ms: f64,
    dropped_chunks: u64,
    guardian_events: u64,
}

impl HostMetrics {
    fn record_chunk(&mut self, chunk: &AudioChunk) {
        if let Some(previous) = self.last_chunk_at {
            let expected = previous + u64::from(chunk.chunk_millis.max(1));
            let actual = chunk.captured_at_ms;
            self.jitter_ms = if actual >= expected {
                (actual - expected) as f64
            } else {
                (expected - actual) as f64
            };
        }
        self.last_chunk_at = Some(chunk.captured_at_ms);
        self.last_chunk_millis = chunk.chunk_millis;
    }

    fn record_drop(&mut self) {
        self.dropped_chunks += 1;
    }

    fn record_guardian_event(&mut self) {
        self.guardian_events += 1;
    }

    fn reset(&mut self) {
        self.last_chunk_at = None;
        self.last_chunk_millis = 20;
        self.jitter_ms = 0.0;
        self.dropped_chunks = 0;
        self.guardian_events = 0;
    }
}

#[napi(object)]
pub struct RuntimeConfigInput {
    pub sample_rate: Option<u32>,
    pub chunk_millis: Option<u32>,
}

#[napi(object)]
pub struct AudioStartRequest {
    pub device_id: Option<String>,
    pub sample_rate: Option<u32>,
    pub channels: Option<u16>,
    pub chunk_millis: Option<u32>,
}

impl Default for AudioStartRequest {
    fn default() -> Self {
        Self {
            device_id: None,
            sample_rate: None,
            channels: None,
            chunk_millis: None,
        }
    }
}

#[napi(object)]
pub struct NativeAudioDevice {
    pub id: String,
    pub label: String,
    pub channels: u16,
    pub is_default: bool,
    pub is_loopback: bool,
}

#[napi(object)]
pub struct NativeHostStatus {
    pub running: bool,
    pub device_id: Option<String>,
    pub device_label: Option<String>,
    pub sample_rate: u32,
    pub chunk_millis: u32,
    pub mock: bool,
}

#[napi(object)]
pub struct NativePcmChunk {
    pub sequence: u64,
    pub pcm: Vec<i16>,
    pub sample_rate: u32,
    pub channels: u16,
    pub device_id: String,
    pub device_label: String,
    pub captured_at: u64,
    pub chunk_millis: u32,
    pub muted: bool,
}

#[napi(object)]
pub struct StreamMetadataEntry {
    pub key: String,
    pub value: String,
}

#[napi(object)]
pub struct StreamSessionConfig {
    pub endpoint: String,
    pub session_id: String,
    pub provider: Option<String>,
    pub token: String,
    pub sample_rate: Option<u32>,
    pub format: Option<String>,
    pub chunk_millis: Option<u32>,
    pub insecure: Option<bool>,
    pub metadata: Option<Vec<StreamMetadataEntry>>,
}

#[napi(object)]
pub struct NativeGuardianEvent {
    pub kind: String,
    pub indicator: String,
    pub process_name: Option<String>,
    pub confidence: f64,
    pub occurred_at: u64,
}

#[napi(object)]
pub struct ScreenshotRectInput {
    pub x: u32,
    pub y: u32,
    pub width: u32,
    pub height: u32,
}

#[napi(object)]
pub struct ScreenshotCaptureInput {
    pub display: Option<u32>,
    pub redactions: Option<Vec<ScreenshotRectInput>>,
}

impl Default for ScreenshotCaptureInput {
    fn default() -> Self {
        Self {
            display: None,
            redactions: None,
        }
    }
}

#[napi(object)]
pub struct ScreenshotCaptureResponse {
    pub bytes: Vec<u8>,
    pub width: u32,
    pub height: u32,
    pub captured_at: u64,
}

#[napi(object)]
pub struct DiagnosticsReport {
    pub cpu_percent: f64,
    pub memory_total: u64,
    pub memory_used: u64,
    pub collected_at: u64,
    pub jitter_ms: f64,
    pub dropped_chunks: u64,
    pub guardian_events: u64,
    pub audio_running: bool,
    pub sample_rate: u32,
    pub chunk_millis: u32,
}

impl From<AudioChunk> for NativePcmChunk {
    fn from(value: AudioChunk) -> Self {
        Self {
            sequence: value.sequence,
            pcm: value.payload,
            sample_rate: value.sample_rate,
            channels: value.channels,
            device_id: value.device_id,
            device_label: value.device_label,
            captured_at: value.captured_at_ms,
            chunk_millis: value.chunk_millis,
            muted: value.muted,
        }
    }
}

#[napi]
pub struct GuardianHost {
    runtime: Runtime,
    chunk_handler: Arc<Mutex<Option<ThreadsafeFunction<NativePcmChunk, ErrorStrategy::Fatal>>>>,
    status_handler: Arc<Mutex<Option<ThreadsafeFunction<NativeHostStatus, ErrorStrategy::Fatal>>>>,
    guardian_handler:
        Arc<Mutex<Option<ThreadsafeFunction<NativeGuardianEvent, ErrorStrategy::Fatal>>>>,
    active_audio: Arc<Mutex<Option<AudioRuntime>>>,
    active_device: Arc<Mutex<Option<ActiveDeviceInfo>>>,
    stream_sender: Arc<Mutex<Option<StreamSender>>>,
    stream_task: Arc<Mutex<Option<tokio::task::JoinHandle<Result<(), StreamError>>>>>,
    guardian_runtime: Arc<Mutex<Option<GuardianService>>>,
    guardian_forwarder: Arc<Mutex<Option<tokio::task::JoinHandle<()>>>>,
    runtime_prefs: Arc<Mutex<RuntimePreferences>>,
    config_cache: Option<SecureCache>,
    metrics: Arc<Mutex<HostMetrics>>,
}

#[napi]
impl GuardianHost {
    #[napi(constructor)]
    pub fn new() -> NapiResult<Self> {
        let runtime = Runtime::new()
            .map_err(|err| napi::Error::new(Status::GenericFailure, err.to_string()))?;
        let cache = Self::build_cache();
        let prefs = cache
            .as_ref()
            .and_then(|store| store.load::<RuntimePreferences>().ok().flatten())
            .unwrap_or_default();
        Ok(Self {
            runtime,
            chunk_handler: Arc::new(Mutex::new(None)),
            status_handler: Arc::new(Mutex::new(None)),
            guardian_handler: Arc::new(Mutex::new(None)),
            active_audio: Arc::new(Mutex::new(None)),
            active_device: Arc::new(Mutex::new(None)),
            stream_sender: Arc::new(Mutex::new(None)),
            stream_task: Arc::new(Mutex::new(None)),
            guardian_runtime: Arc::new(Mutex::new(None)),
            guardian_forwarder: Arc::new(Mutex::new(None)),
            runtime_prefs: Arc::new(Mutex::new(prefs)),
            config_cache: cache,
            metrics: Arc::new(Mutex::new(HostMetrics::default())),
        })
    }

    #[napi]
    pub fn register_chunk_handler(&self, handler: JsFunction) -> NapiResult<()> {
        let tsf = handler.create_threadsafe_function(
            0,
            |ctx: ThreadSafeCallContext<NativePcmChunk>| {
                ctx.env.to_js_value(&ctx.value).map(|value| vec![value])
            },
        )?;
        *self.chunk_handler.lock() = Some(tsf);
        Ok(())
    }

    #[napi]
    pub fn register_status_handler(&self, handler: JsFunction) -> NapiResult<()> {
        let tsf = handler.create_threadsafe_function(
            0,
            |ctx: ThreadSafeCallContext<NativeHostStatus>| {
                ctx.env.to_js_value(&ctx.value).map(|value| vec![value])
            },
        )?;
        *self.status_handler.lock() = Some(tsf);
        Ok(())
    }

    #[napi]
    pub fn register_guardian_handler(&self, handler: JsFunction) -> NapiResult<()> {
        let tsf = handler.create_threadsafe_function(
            0,
            |ctx: ThreadSafeCallContext<NativeGuardianEvent>| {
                ctx.env.to_js_value(&ctx.value).map(|value| vec![value])
            },
        )?;
        *self.guardian_handler.lock() = Some(tsf);
        Ok(())
    }

    #[napi]
    pub fn start_audio(&self, request: Option<AudioStartRequest>) -> NapiResult<()> {
        let prefs = self.runtime_prefs.lock().clone();
        let capture_options = options_from_request(request, &prefs);
        let (tx, rx) = mpsc::channel::<AudioChunk>(128);
        let session = audio::start_capture(capture_options.clone(), tx)
            .map_err(|err| napi::Error::new(Status::GenericFailure, err.to_string()))?;

        let descriptor = session.descriptor.clone();
        let device_info = ActiveDeviceInfo {
            id: descriptor.id.clone(),
            label: descriptor.label.clone(),
            sample_rate: session.target_sample_rate,
            chunk_millis: session.chunk_millis,
        };
        *self.active_device.lock() = Some(device_info.clone());

        let mut audio_slot = self.active_audio.lock();
        if let Some(existing) = audio_slot.take() {
            existing.stop();
        }

        let chunk_handler = Arc::clone(&self.chunk_handler);
        let status_handler = Arc::clone(&self.status_handler);
        let stream_tx = Arc::clone(&self.stream_sender);
        let metrics = Arc::clone(&self.metrics);
        let (shutdown_tx, mut shutdown_rx) = watch::channel(false);
        let mut receiver = rx;

        let join_handle = self.runtime.spawn(async move {
            loop {
                tokio::select! {
                    changed = shutdown_rx.changed() => {
                        if changed.is_ok() && *shutdown_rx.borrow() {
                            break;
                        }
                    }
                    chunk = receiver.recv() => {
                        match chunk {
                            Some(payload) => {
                                {
                                    let mut tracker = metrics.lock();
                                    tracker.record_chunk(&payload);
                                }
                                if let Some(sender) = stream_tx.lock().clone() {
                                    if !sender.enqueue_chunk(payload.clone()) {
                                        {
                                            let mut tracker = metrics.lock();
                                            tracker.record_drop();
                                        }
                                        warn!("stream queue saturated; dropping chunk");
                                    }
                                }
                                let message = NativePcmChunk::from(payload);
                                if let Some(handler) = chunk_handler.lock().clone() {
                                    if handler.call(message, ThreadsafeFunctionCallMode::NonBlocking).is_err() {
                                        warn!("failed to deliver PCM chunk to JS listener");
                                    }
                                }
                            }
                            None => break,
                        }
                    }
                }
            }
        });

        let runtime = AudioRuntime {
            session,
            shutdown: shutdown_tx,
            join_handle,
        };
        *audio_slot = Some(runtime);
        drop(audio_slot);

        self.emit_status(NativeHostStatus {
            running: true,
            device_id: Some(device_info.id),
            device_label: Some(device_info.label),
            sample_rate: device_info.sample_rate,
            chunk_millis: device_info.chunk_millis,
            mock: false,
        });

        info!("audio capture host started");
        Ok(())
    }

    #[napi]
    pub fn apply_runtime_config(&self, payload: RuntimeConfigInput) -> NapiResult<()> {
        {
            let mut prefs = self.runtime_prefs.lock();
            if let Some(rate) = payload.sample_rate {
                prefs.sample_rate = Some(rate);
            }
            if let Some(chunk) = payload.chunk_millis {
                prefs.chunk_millis = Some(chunk);
            }
        }
        self.persist_preferences();
        Ok(())
    }

    #[napi]
    pub fn start_guardian(&self, policy_json: String) -> NapiResult<()> {
        let policy = guardian::parse_policy(&policy_json)
            .map_err(|err| napi::Error::new(Status::InvalidArg, err.to_string()))?;
        self.stop_guardian_internal();
        let (service, mut rx) = GuardianService::spawn(policy);
        let handler = Arc::clone(&self.guardian_handler);
        let metrics = Arc::clone(&self.metrics);
        let forwarder = self.runtime.spawn(async move {
            while let Some(event) = rx.recv().await {
                if let Some(callback) = handler.lock().clone() {
                    let payload = NativeGuardianEvent::from(event);
                    if callback
                        .call(payload, ThreadsafeFunctionCallMode::NonBlocking)
                        .is_err()
                    {
                        warn!("failed to emit guardian event to JS");
                    }
                }
                {
                    let mut tracker = metrics.lock();
                    tracker.record_guardian_event();
                }
            }
        });
        *self.guardian_runtime.lock() = Some(service);
        *self.guardian_forwarder.lock() = Some(forwarder);
        Ok(())
    }

    #[napi]
    pub fn stop_guardian(&self) -> NapiResult<()> {
        self.stop_guardian_internal();
        Ok(())
    }

    #[napi]
    pub fn capture_screenshot(
        &self,
        request: Option<ScreenshotCaptureInput>,
    ) -> NapiResult<ScreenshotCaptureResponse> {
        let req = request.unwrap_or_default();
        let capture_request = NativeCaptureRequest {
            display: req.display,
            redactions: req
                .redactions
                .unwrap_or_default()
                .into_iter()
                .map(|rect| NativeScreenshotRect {
                    x: rect.x,
                    y: rect.y,
                    width: rect.width,
                    height: rect.height,
                })
                .collect(),
        };
        let payload = screenshot::capture_png(&capture_request)
            .map_err(|err| napi::Error::new(Status::GenericFailure, err.to_string()))?;
        let captured_at = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .map(|d| d.as_millis() as u64)
            .unwrap_or(0);
        Ok(ScreenshotCaptureResponse {
            bytes: payload.bytes,
            width: payload.width,
            height: payload.height,
            captured_at,
        })
    }

    #[napi]
    pub fn collect_diagnostics(&self) -> NapiResult<DiagnosticsReport> {
        let system = collect_snapshot();
        let metrics = self.metrics.lock().clone();
        let audio_running = self.active_audio.lock().is_some();
        let (sample_rate, chunk_millis) = self
            .active_device
            .lock()
            .as_ref()
            .map(|device| (device.sample_rate, device.chunk_millis))
            .unwrap_or((16_000, 20));

        Ok(DiagnosticsReport {
            cpu_percent: system.cpu_percent,
            memory_total: system.memory_total,
            memory_used: system.memory_used,
            collected_at: system.collected_at,
            jitter_ms: metrics.jitter_ms,
            dropped_chunks: metrics.dropped_chunks,
            guardian_events: metrics.guardian_events,
            audio_running,
            sample_rate,
            chunk_millis,
        })
    }

    #[napi]
    pub fn stop_audio(&self) -> NapiResult<()> {
        self.stop_streaming_internal();
        if let Some(runtime) = self.active_audio.lock().take() {
            runtime.stop();
            info!("audio capture host stopped");
        }
        self.metrics.lock().reset();
        let mut device_guard = self.active_device.lock();
        let snapshot = device_guard.clone();
        *device_guard = None;
        drop(device_guard);
        let (sample_rate, chunk_millis) = snapshot
            .map(|device| (device.sample_rate, device.chunk_millis))
            .unwrap_or((16_000, 20));
        self.emit_status(NativeHostStatus {
            running: false,
            device_id: None,
            device_label: None,
            sample_rate,
            chunk_millis,
            mock: false,
        });
        Ok(())
    }

    #[napi]
    pub fn start_streaming(&self, config: StreamSessionConfig) -> NapiResult<()> {
        let stream_config = config
            .try_into_stream_config()
            .map_err(|err| napi::Error::new(Status::InvalidArg, err))?;
        self.stop_streaming_internal();
        let (sender, handle) = spawn_stream(stream_config);
        *self.stream_sender.lock() = Some(sender);
        *self.stream_task.lock() = Some(handle);
        Ok(())
    }

    #[napi]
    pub fn stop_streaming(&self) -> NapiResult<()> {
        self.stop_streaming_internal();
        Ok(())
    }

    #[napi]
    pub fn list_audio_devices(&self) -> NapiResult<Vec<NativeAudioDevice>> {
        let devices = audio::list_devices()
            .map_err(|err| napi::Error::new(Status::GenericFailure, err.to_string()))?;
        Ok(devices
            .into_iter()
            .map(native_device_from_descriptor)
            .collect())
    }

    #[napi]
    pub fn is_running(&self) -> bool {
        self.active_audio.lock().is_some()
    }

    #[napi]
    pub fn is_mock(&self) -> bool {
        false
    }
}

impl GuardianHost {
    fn build_cache() -> Option<SecureCache> {
        let dir = std::env::var("GUARDIAN_CACHE_DIR").ok()?;
        let key_b64 = std::env::var("GUARDIAN_CACHE_KEY").ok()?;
        let decoded = Base64.decode(key_b64).ok()?;
        let path = Path::new(&dir).join(RUNTIME_CACHE_FILE);
        Some(SecureCache::new(path, &decoded))
    }

    fn persist_preferences(&self) {
        if let Some(cache) = &self.config_cache {
            let prefs = self.runtime_prefs.lock().clone();
            let ttl = Duration::from_secs(CACHE_TTL_SECONDS);
            let _ = cache.store(&prefs, ttl);
        }
    }
}

#[napi]
pub fn create_guardian_host() -> NapiResult<GuardianHost> {
    GuardianHost::new()
}

impl GuardianHost {
    fn emit_status(&self, status: NativeHostStatus) {
        if let Some(handler) = self.status_handler.lock().clone() {
            if handler
                .call(status, ThreadsafeFunctionCallMode::NonBlocking)
                .is_err()
            {
                error!("failed to deliver status update to JS listener");
            }
        }
    }

    fn stop_streaming_internal(&self) {
        if let Some(sender) = self.stream_sender.lock().take() {
            if let Err(err) = self.runtime.block_on(sender.shutdown("stop")) {
                warn!(error = %err, "failed to signal stream shutdown");
            }
        }
        if let Some(handle) = self.stream_task.lock().take() {
            let outcome = self.runtime.block_on(async { handle.await });
            match outcome {
                Ok(Ok(())) => {}
                Ok(Err(err)) => warn!(error = %err, "stream worker reported error"),
                Err(err) => warn!(error = %err, "stream worker join error"),
            }
        }
    }

    fn stop_guardian_internal(&self) {
        if let Some(runtime) = self.guardian_runtime.lock().take() {
            self.runtime.block_on(runtime.stop());
        }
        if let Some(handle) = self.guardian_forwarder.lock().take() {
            let _ = self.runtime.block_on(async { handle.await });
        }
    }
}

fn options_from_request(
    request: Option<AudioStartRequest>,
    prefs: &RuntimePreferences,
) -> CaptureOptions {
    let req = request.unwrap_or_default();
    let mut options = CaptureOptions::default();
    options.preferred_device_id = req.device_id;
    if let Some(sample_rate) = req.sample_rate.or(prefs.sample_rate) {
        options.target_sample_rate = sample_rate;
    }
    if let Some(channels) = req.channels {
        options.target_channels = channels;
    }
    if let Some(chunk_millis) = req.chunk_millis.or(prefs.chunk_millis) {
        options.chunk_millis = chunk_millis;
    }
    options
}

fn native_device_from_descriptor(descriptor: AudioDeviceDescriptor) -> NativeAudioDevice {
    NativeAudioDevice {
        id: descriptor.id,
        label: descriptor.label,
        channels: descriptor.channels,
        is_default: descriptor.is_default,
        is_loopback: descriptor.is_loopback,
    }
}

impl StreamSessionConfig {
    fn try_into_stream_config(self) -> Result<StreamConfig, String> {
        if self.endpoint.trim().is_empty() {
            return Err("endpoint required".to_string());
        }
        if self.session_id.trim().is_empty() {
            return Err("session_id required".to_string());
        }
        if self.token.trim().is_empty() {
            return Err("token required".to_string());
        }
        let mut metadata = HashMap::new();
        if let Some(entries) = self.metadata {
            for entry in entries {
                metadata.insert(entry.key, entry.value);
            }
        }
        let chunk_millis = self.chunk_millis.unwrap_or(20);
        metadata
            .entry("chunk_millis".to_string())
            .or_insert_with(|| chunk_millis.to_string());
        let provider = self.provider.unwrap_or_else(|| "aliyun".to_string());
        let mut config = StreamConfig {
            endpoint: self.endpoint,
            session_id: self.session_id,
            provider,
            token: self.token,
            sample_rate: self.sample_rate.unwrap_or(16_000),
            format: self.format.unwrap_or_else(|| "pcm16".to_string()),
            chunk_millis,
            insecure: self.insecure.unwrap_or(true),
            metadata,
            ..StreamConfig::default()
        };
        config.buffer_size = config.buffer_size.max(256);
        Ok(config)
    }
}

impl From<guardian::GuardianEvent> for NativeGuardianEvent {
    fn from(value: guardian::GuardianEvent) -> Self {
        let occurred = value.occurred_at.timestamp_millis().max(0) as u64;
        Self {
            kind: value.kind,
            indicator: value.indicator,
            process_name: value.process_name,
            confidence: value.confidence as f64,
            occurred_at: occurred,
        }
    }
}
