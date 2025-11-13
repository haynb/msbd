use std::{collections::HashMap, pin::Pin, time::Duration};

use audio::AudioChunk;
use tokio::{select, sync::mpsc, task::JoinHandle, time::sleep};
use tokio_stream::wrappers::ReceiverStream;
use tonic::{metadata::MetadataValue, transport::Endpoint, Request};
use tracing::{debug, error, info, warn};

use crate::proto::realtime::v1::{
    stream_audio_request::Payload, streaming_service_client::StreamingServiceClient,
    AudioChunk as ProtoAudioChunk, ClientSignal, StreamAck, StreamAudioRequest,
    StreamAudioResponse, StreamInit,
};

/// Configuration for establishing the realtime streaming connection.
#[derive(Clone, Debug)]
pub struct StreamConfig {
    pub endpoint: String,
    pub session_id: String,
    pub provider: String,
    pub token: String,
    pub sample_rate: u32,
    pub format: String,
    pub chunk_millis: u32,
    pub insecure: bool,
    pub metadata: HashMap<String, String>,
    pub max_retries: usize,
    pub buffer_size: usize,
    pub base_backoff_ms: u64,
    pub max_backoff_ms: u64,
}

impl Default for StreamConfig {
    fn default() -> Self {
        Self {
            endpoint: "http://127.0.0.1:9084".to_string(),
            session_id: String::new(),
            provider: "aliyun".to_string(),
            token: String::new(),
            sample_rate: 16_000,
            format: "pcm16".to_string(),
            chunk_millis: 20,
            insecure: true,
            metadata: HashMap::new(),
            max_retries: 3,
            buffer_size: 512,
            base_backoff_ms: 100,
            max_backoff_ms: 5_000,
        }
    }
}

#[derive(thiserror::Error, Debug)]
pub enum StreamError {
    #[error("grpc connection failed: {0}")]
    Connect(String),
    #[error("grpc send failed: {0}")]
    Send(String),
    #[error("grpc recv failed: {0}")]
    Recv(String),
    #[error("channel closed")]
    ChannelClosed,
}

#[derive(Clone)]
pub struct StreamSender {
    tx: mpsc::Sender<StreamCommand>,
}

impl StreamSender {
    pub fn enqueue_chunk(&self, chunk: AudioChunk) -> bool {
        self.tx.try_send(StreamCommand::Chunk(chunk)).is_ok()
    }

    pub async fn signal(&self, reason: &str) -> Result<(), StreamError> {
        self.tx
            .send(StreamCommand::Signal {
                reason: reason.to_string(),
            })
            .await
            .map_err(|_| StreamError::ChannelClosed)
    }

    pub async fn shutdown(&self, reason: &str) -> Result<(), StreamError> {
        self.tx
            .send(StreamCommand::Shutdown {
                reason: reason.to_string(),
            })
            .await
            .map_err(|_| StreamError::ChannelClosed)
    }
}

enum StreamCommand {
    Chunk(AudioChunk),
    Signal { reason: String },
    Shutdown { reason: String },
}

/// Spawn the streaming task and return a sender plus join handle.
pub fn spawn_stream(config: StreamConfig) -> (StreamSender, JoinHandle<Result<(), StreamError>>) {
    let (tx, rx) = mpsc::channel(config.buffer_size.max(128));
    let sender = StreamSender { tx: tx.clone() };
    let handle = tokio::spawn(async move { run_stream(rx, config).await });
    (sender, handle)
}

async fn run_stream(
    mut command_rx: mpsc::Receiver<StreamCommand>,
    config: StreamConfig,
) -> Result<(), StreamError> {
    let mut attempts = 0usize;
    let mut backoff = Duration::from_millis(config.base_backoff_ms.max(50));
    loop {
        match connect_stream(&config).await {
            Ok((mut request_tx, mut response_stream)) => {
                info!(session = %config.session_id, "stream connected");
                let init = StreamAudioRequest {
                    payload: Some(Payload::Init(StreamInit {
                        session_id: config.session_id.clone(),
                        provider: config.provider.clone(),
                        sample_rate: config.sample_rate as i32,
                        format: config.format.clone(),
                        metadata: config.metadata.clone(),
                    })),
                };
                request_tx
                    .send(init)
                    .await
                    .map_err(|err| StreamError::Send(err.to_string()))?;

                let mut response_fut = Box::pin(async move {
                    while let Some(message) = response_stream
                        .message()
                        .await
                        .map_err(|err| StreamError::Recv(err.to_string()))?
                    {
                        handle_response(message);
                    }
                    Ok::<(), StreamError>(())
                });

                let result =
                    pump_commands(&mut command_rx, &mut request_tx, &mut response_fut).await;
                match result {
                    Ok(()) => return Ok(()),
                    Err(StreamError::ChannelClosed) => return Ok(()),
                    Err(err) => {
                        error!(error = %err, "stream error");
                        return Err(err);
                    }
                }
            }
            Err(err) => {
                attempts += 1;
                if attempts >= config.max_retries.max(1) {
                    return Err(err);
                }
                warn!(attempt = attempts, error = %err, "stream connect failed; backing off");
                sleep(backoff).await;
                backoff = (backoff * 2).min(Duration::from_millis(config.max_backoff_ms));
            }
        }
    }
}

fn handle_response(message: StreamAudioResponse) {
    match message.payload {
        Some(stream_response) => match stream_response {
            crate::proto::realtime::v1::stream_audio_response::Payload::Ack(StreamAck {
                session_id,
                ..
            }) => {
                debug!(session = %session_id, "received stream ack");
            }
            crate::proto::realtime::v1::stream_audio_response::Payload::Transcript(transcript) => {
                debug!(sequence = transcript.sequence, final = transcript.is_final, "transcript event received");
            }
            crate::proto::realtime::v1::stream_audio_response::Payload::Error(err) => {
                warn!(code = %err.code, message = %err.message, "gateway reported error");
            }
        },
        None => {}
    }
}

async fn pump_commands(
    command_rx: &mut mpsc::Receiver<StreamCommand>,
    request_tx: &mut mpsc::Sender<StreamAudioRequest>,
    response_fut: &mut Pin<Box<impl std::future::Future<Output = Result<(), StreamError>>>>,
) -> Result<(), StreamError> {
    loop {
        select! {
            biased;
            response = response_fut => {
                return response;
            }
            maybe_cmd = command_rx.recv() => {
                match maybe_cmd {
                    Some(StreamCommand::Chunk(chunk)) => {
                        let request = chunk_to_request(&chunk);
                        request_tx
                            .send(request)
                            .await
                            .map_err(|err| StreamError::Send(err.to_string()))?;
                    }
                    Some(StreamCommand::Signal { reason }) => {
                        let request = signal_request(reason);
                        request_tx
                            .send(request)
                            .await
                            .map_err(|err| StreamError::Send(err.to_string()))?;
                    }
                    Some(StreamCommand::Shutdown { reason }) => {
                        let request = signal_request(reason);
                        let _ = request_tx.send(request).await;
                        request_tx.close_channel();
                        return Ok(());
                    }
                    None => {
                        request_tx.close_channel();
                        return Err(StreamError::ChannelClosed);
                    }
                }
            }
        }
    }
}

async fn connect_stream(
    config: &StreamConfig,
) -> Result<
    (
        mpsc::Sender<StreamAudioRequest>,
        tonic::codec::Streaming<StreamAudioResponse>,
    ),
    StreamError,
> {
    let target = if config.endpoint.starts_with("http") {
        config.endpoint.clone()
    } else {
        format!("http://{}", config.endpoint)
    };
    let channel = Endpoint::from_shared(target)
        .map_err(|err| StreamError::Connect(err.to_string()))?
        .timeout(Duration::from_secs(10))
        .connect()
        .await
        .map_err(|err| StreamError::Connect(err.to_string()))?;

    let token = format!("Bearer {}", config.token);
    let token_value = MetadataValue::try_from(token.as_str())
        .map_err(|err| StreamError::Connect(err.to_string()))?;

    let interceptor = move |mut req: Request<()>| {
        req.metadata_mut()
            .insert("authorization", token_value.clone());
        Ok(req)
    };

    let client = StreamingServiceClient::with_interceptor(channel, interceptor);
    let (request_tx, request_rx) = mpsc::channel::<StreamAudioRequest>(config.buffer_size.max(128));
    let stream = ReceiverStream::new(request_rx);
    let response = client
        .stream_audio(Request::new(stream))
        .await
        .map_err(|err| StreamError::Connect(err.to_string()))?;
    Ok((request_tx, response.into_inner()))
}

fn chunk_to_request(chunk: &AudioChunk) -> StreamAudioRequest {
    let mut bytes = Vec::with_capacity(chunk.payload.len() * 2);
    for sample in &chunk.payload {
        bytes.extend_from_slice(&sample.to_le_bytes());
    }
    let proto_chunk = ProtoAudioChunk {
        data: bytes,
        sequence: chunk.sequence as i64,
        is_last: false,
    };
    StreamAudioRequest {
        payload: Some(Payload::Chunk(proto_chunk)),
    }
}

fn signal_request(reason: String) -> StreamAudioRequest {
    StreamAudioRequest {
        payload: Some(Payload::Signal(ClientSignal { reason })),
    }
}
