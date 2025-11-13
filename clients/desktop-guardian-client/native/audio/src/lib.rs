use std::{
    fmt,
    sync::{
        atomic::{AtomicU64, Ordering},
        Arc,
    },
    time::{Duration, SystemTime, UNIX_EPOCH},
};

use cpal::{
    self,
    traits::{DeviceTrait, HostTrait, StreamTrait},
    Device, PlayStreamError, SampleFormat, SizedSample, Stream,
};
use parking_lot::Mutex;
use thiserror::Error;
use tokio::sync::mpsc::Sender;
use tracing::{debug, warn};

#[derive(Debug, Clone, serde::Serialize)]
pub struct AudioChunk {
    pub sequence: u64,
    pub device_id: String,
    pub device_label: String,
    pub sample_rate: u32,
    pub channels: u16,
    pub payload: Vec<i16>,
    pub muted: bool,
    pub captured_at_ms: u64,
    pub chunk_millis: u32,
}

#[derive(Debug, Clone)]
pub struct CaptureOptions {
    pub preferred_device_id: Option<String>,
    pub target_sample_rate: u32,
    pub target_channels: u16,
    pub chunk_millis: u32,
    pub silence_threshold: i16,
}

impl Default for CaptureOptions {
    fn default() -> Self {
        Self {
            preferred_device_id: None,
            target_sample_rate: 16_000,
            target_channels: 1,
            chunk_millis: 20,
            silence_threshold: 4,
        }
    }
}

#[derive(Debug, Clone, serde::Serialize)]
pub struct AudioDeviceDescriptor {
    pub id: String,
    pub label: String,
    pub channels: u16,
    pub is_default: bool,
    pub is_loopback: bool,
}

pub struct CaptureSession {
    pub handle: AudioCaptureHandle,
    pub descriptor: AudioDeviceDescriptor,
    pub target_sample_rate: u32,
    pub target_channels: u16,
    pub chunk_millis: u32,
}

pub struct AudioCaptureHandle {
    stream: Option<Stream>,
}

impl AudioCaptureHandle {
    pub fn stop(mut self) {
        if let Some(stream) = self.stream.take() {
            drop(stream);
        }
    }
}

#[derive(Debug, Error)]
pub enum AudioError {
    #[error("audio host unavailable: {0}")]
    HostUnavailable(String),
    #[error("no audio input device available")]
    DeviceUnavailable,
    #[error("failed to build audio stream: {0}")]
    StreamError(String),
}

impl From<cpal::DevicesError> for AudioError {
    fn from(value: cpal::DevicesError) -> Self {
        AudioError::HostUnavailable(value.to_string())
    }
}

impl From<cpal::BuildStreamError> for AudioError {
    fn from(value: cpal::BuildStreamError) -> Self {
        AudioError::StreamError(value.to_string())
    }
}

impl From<PlayStreamError> for AudioError {
    fn from(value: PlayStreamError) -> Self {
        AudioError::StreamError(value.to_string())
    }
}

/// Enumerate capture capable devices with synthetic identifiers used elsewhere in the app.
pub fn list_devices() -> Result<Vec<AudioDeviceDescriptor>, AudioError> {
    let host = cpal::default_host();
    let default_name = host
        .default_input_device()
        .and_then(|device| device.name().ok())
        .unwrap_or_default();

    let devices = host.input_devices()?;
    let items = devices
        .enumerate()
        .filter_map(
            |(idx, device)| match describe_device(&device, idx, &default_name) {
                Ok(descriptor) => Some(descriptor),
                Err(err) => {
                    warn!(error = %err, "failed to describe device");
                    None
                }
            },
        )
        .collect::<Vec<_>>();

    if items.is_empty() {
        Err(AudioError::DeviceUnavailable)
    } else {
        Ok(items)
    }
}

fn describe_device(
    device: &Device,
    index: usize,
    default_name: &str,
) -> Result<AudioDeviceDescriptor, AudioError> {
    let name = device
        .name()
        .map_err(|err| AudioError::HostUnavailable(format!("{err}")))?;
    let id = format!("input::{index}");
    Ok(AudioDeviceDescriptor {
        id,
        label: name.clone(),
        channels: device
            .default_input_config()
            .map(|cfg| cfg.channels())
            .unwrap_or(1),
        is_default: name == default_name,
        is_loopback: name.to_lowercase().contains("loopback"),
    })
}

pub fn start_capture(
    options: CaptureOptions,
    tx: Sender<AudioChunk>,
) -> Result<CaptureSession, AudioError> {
    let host = cpal::default_host();
    let (device, descriptor) = select_device(&host, options.preferred_device_id.clone())?;
    let config = device
        .default_input_config()
        .map_err(|err| AudioError::HostUnavailable(err.to_string()))?;
    let actual_sample_rate = config.sample_rate().0;
    let _actual_channels = config.channels();
    let stream_config: cpal::StreamConfig = config.clone().into();
    let sequence = Arc::new(AtomicU64::new(1));
    let buffer = Arc::new(Mutex::new(Vec::<i16>::with_capacity(4096)));
    let chunk_samples = chunk_size(
        options.chunk_millis,
        options.target_sample_rate,
        options.target_channels,
    );

    let tx_clone = tx.clone();
    let buffer_clone = buffer.clone();
    let device_id = descriptor.id.clone();
    let device_label = descriptor.label.clone();
    let target_rate = options.target_sample_rate;
    let target_channels = options.target_channels;
    let silence_threshold = options.silence_threshold;

    let stream = match config.sample_format() {
        SampleFormat::I16 => build_stream::<i16>(
            &device,
            stream_config,
            tx_clone,
            buffer_clone,
            options.clone(),
            actual_sample_rate,
            chunk_samples,
            sequence.clone(),
            device_id.clone(),
            device_label.clone(),
            silence_threshold,
            convert_from_i16,
        )?,
        SampleFormat::U16 => build_stream::<u16>(
            &device,
            stream_config,
            tx_clone,
            buffer_clone,
            options.clone(),
            actual_sample_rate,
            chunk_samples,
            sequence.clone(),
            device_id.clone(),
            device_label.clone(),
            silence_threshold,
            convert_from_u16,
        )?,
        SampleFormat::F32 => build_stream::<f32>(
            &device,
            stream_config,
            tx_clone,
            buffer_clone,
            options.clone(),
            actual_sample_rate,
            chunk_samples,
            sequence.clone(),
            device_id.clone(),
            device_label.clone(),
            silence_threshold,
            convert_from_f32,
        )?,
        other => {
            return Err(AudioError::StreamError(format!(
                "unsupported sample format {other:?}"
            )));
        }
    };

    stream.play()?;
    debug!(
      device = %device_label,
      actual_rate = actual_sample_rate,
      target_rate = target_rate,
      target_channels = target_channels,
      "audio capture started"
    );

    Ok(CaptureSession {
        handle: AudioCaptureHandle {
            stream: Some(stream),
        },
        descriptor,
        target_sample_rate: target_rate,
        target_channels: target_channels,
        chunk_millis: options.chunk_millis,
    })
}

fn chunk_size(chunk_millis: u32, sample_rate: u32, channels: u16) -> usize {
    let frames_per_ms = sample_rate as f32 / 1000.0;
    let total_frames = (frames_per_ms * chunk_millis as f32).round() as usize;
    total_frames.max(1) * channels as usize
}

fn select_device(
    host: &cpal::Host,
    preferred: Option<String>,
) -> Result<(Device, AudioDeviceDescriptor), AudioError> {
    let default_name = host
        .default_input_device()
        .as_ref()
        .and_then(|d| d.name().ok())
        .unwrap_or_default();

    let devices: Vec<Device> = host.input_devices()?.collect();
    if devices.is_empty() {
        return Err(AudioError::DeviceUnavailable);
    }

    let descriptors = devices
        .iter()
        .enumerate()
        .map(|(idx, device)| describe_device(device, idx, &default_name))
        .collect::<Result<Vec<_>, _>>()?;

    if let Some(target_id) = preferred {
        if let Some((idx, descriptor)) = descriptors
            .iter()
            .enumerate()
            .find(|(_, descriptor)| descriptor.id == target_id)
        {
            return Ok((devices[idx].clone(), descriptor.clone()));
        }
    }

    if let Some((idx, descriptor)) = descriptors
        .iter()
        .enumerate()
        .find(|(_, descriptor)| descriptor.is_default)
    {
        return Ok((devices[idx].clone(), descriptor.clone()));
    }

    Ok((devices[0].clone(), descriptors[0].clone()))
}

fn build_stream<T>(
    device: &Device,
    config: cpal::StreamConfig,
    tx: Sender<AudioChunk>,
    buffer: Arc<Mutex<Vec<i16>>>,
    options: CaptureOptions,
    actual_sample_rate: u32,
    chunk_samples: usize,
    sequence: Arc<AtomicU64>,
    device_id: String,
    device_label: String,
    silence_threshold: i16,
    converter: fn(&[T]) -> Vec<i16>,
) -> Result<Stream, AudioError>
where
    T: SizedSample + fmt::Debug + 'static,
{
    let channels = config.channels.max(1);
    let mut config = config;
    config.channels = channels;
    let tx_clone = tx.clone();
    let chunk_duration = options.chunk_millis;
    let target_channels = options.target_channels;
    let target_rate = options.target_sample_rate;

    let stream = device.build_input_stream(
        &config,
        move |data: &[T], _| {
            let converted = converter(data);
            let downmixed = downmix(&converted, channels, target_channels);
            let resampled = resample(&downmixed, actual_sample_rate, target_rate, target_channels);
            append_and_emit(
                &tx_clone,
                &buffer,
                chunk_samples,
                &sequence,
                &device_id,
                &device_label,
                target_rate,
                target_channels,
                chunk_duration,
                &resampled,
                silence_threshold,
            );
        },
        move |err| {
            warn!(error = %err, "audio stream error");
        },
        None,
    )?;

    Ok(stream)
}

fn convert_from_i16(data: &[i16]) -> Vec<i16> {
    data.to_vec()
}

fn convert_from_u16(data: &[u16]) -> Vec<i16> {
    data.iter()
        .map(|sample| {
            let centered = *sample as i32 - (i16::MAX as i32 + 1);
            centered.clamp(i16::MIN as i32, i16::MAX as i32) as i16
        })
        .collect()
}

fn convert_from_f32(data: &[f32]) -> Vec<i16> {
    data.iter()
        .map(|sample| {
            let clamped = sample.clamp(-1.0, 1.0);
            (clamped * i16::MAX as f32).round() as i16
        })
        .collect()
}

fn downmix(buffer: &[i16], in_channels: u16, out_channels: u16) -> Vec<i16> {
    if in_channels == out_channels {
        return buffer.to_vec();
    }

    if out_channels == 1 {
        buffer
            .chunks(in_channels as usize)
            .map(|frame| {
                let sum: i32 = frame.iter().map(|sample| *sample as i32).sum();
                let avg = sum / frame.len() as i32;
                avg.clamp(i16::MIN as i32, i16::MAX as i32) as i16
            })
            .collect()
    } else {
        buffer
            .chunks(in_channels as usize)
            .flat_map(|frame| {
                let sample = frame.first().copied().unwrap_or_default();
                std::iter::repeat(sample)
                    .take(out_channels as usize)
                    .collect::<Vec<_>>()
            })
            .collect()
    }
}

fn resample(buffer: &[i16], in_rate: u32, out_rate: u32, channels: u16) -> Vec<i16> {
    if in_rate == out_rate || buffer.is_empty() {
        return buffer.to_vec();
    }

    let ratio = out_rate as f32 / in_rate as f32;
    let in_frames = buffer.len() / channels as usize;
    if in_frames < 2 {
        return buffer.to_vec();
    }

    let out_frames = (in_frames as f32 * ratio).round().max(1.0) as usize;
    let mut output = Vec::with_capacity(out_frames * channels as usize);

    for out_idx in 0..out_frames {
        let src_pos = out_idx as f32 / ratio;
        let src_index = src_pos.floor() as usize;
        let alpha = src_pos - src_index as f32;
        let next_index = (src_index + 1).min(in_frames - 1);
        for ch in 0..channels as usize {
            let a = buffer[src_index * channels as usize + ch] as f32;
            let b = buffer[next_index * channels as usize + ch] as f32;
            let interpolated = a + (b - a) * alpha;
            output.push(interpolated.round().clamp(i16::MIN as f32, i16::MAX as f32) as i16);
        }
    }

    output
}

fn append_and_emit(
    tx: &Sender<AudioChunk>,
    buffer: &Arc<Mutex<Vec<i16>>>,
    chunk_samples: usize,
    sequence: &Arc<AtomicU64>,
    device_id: &str,
    device_label: &str,
    sample_rate: u32,
    channels: u16,
    chunk_millis: u32,
    incoming: &[i16],
    silence_threshold: i16,
) {
    if incoming.is_empty() {
        return;
    }

    let mut guard = buffer.lock();
    guard.extend_from_slice(incoming);

    while guard.len() >= chunk_samples {
        let chunk: Vec<i16> = guard.drain(..chunk_samples).collect();
        let is_muted = chunk.iter().all(|sample| sample.abs() <= silence_threshold);
        let timestamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or(Duration::from_millis(0))
            .as_millis() as u64;

        let payload = AudioChunk {
            sequence: sequence.fetch_add(1, Ordering::SeqCst),
            device_id: device_id.to_string(),
            device_label: device_label.to_string(),
            sample_rate,
            channels,
            payload: chunk,
            muted: is_muted,
            captured_at_ms: timestamp,
            chunk_millis,
        };

        if tx.try_send(payload).is_err() {
            break;
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_downmix() {
        let buffer = vec![32767, 0, -32768, 0];
        let result = downmix(&buffer, 2, 1);
        assert_eq!(result.len(), 2);
        assert_eq!(result[0], 16383);
        assert_eq!(result[1], -16384);
    }

    #[test]
    fn test_resample_linear() {
        let buffer = vec![0, 0, 10, 10, 20, 20, 30, 30];
        let output = resample(&buffer, 4, 8, 2);
        assert!(output.len() >= buffer.len());
    }

    #[test]
    fn test_chunk_size() {
        assert_eq!(chunk_size(20, 16000, 1), 320);
        assert_eq!(chunk_size(10, 8000, 2), 160);
    }
}
