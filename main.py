from sound_capture.sound_capture import AudioRecorder
from config.config_loader import ConfigLoader
from speech_recognition.ali.speech_recognition import AliyunSpeechRecognizer
import numpy as np
from llm.openai.client import LLMClient


def process_audio_callback(data):
    # 转换为单声道
    mono_data = data.mean(axis=1) if len(data.shape) > 1 else data

    # 音量归一化
    if mono_data.max() != 0:
        normalized_data = mono_data / np.max(np.abs(mono_data))
    else:
        normalized_data = mono_data

    # 简单的降噪：去除低于阈值的噪音
    noise_threshold = 0.02
    normalized_data[np.abs(normalized_data) < noise_threshold] = 0

    # 转换为PCM格式
    pcm_data = (normalized_data * 32767).astype(np.int16).tobytes()
    recognizer.process_audio(pcm_data)


if __name__ == "__main__":
    try:
        # 加载配置
        config = ConfigLoader()

        # 初始化语音识别器
        recognizer = AliyunSpeechRecognizer()
        recognizer.start_recognition()

        # 录制音频
        audio_recorder = AudioRecorder()

        audio_recorder.start_recording(callback=process_audio_callback)

    except Exception as e:
        print(f"错误: {str(e)}")
    finally:
        recognizer.stop_recognition()

    # 测试LLMClient
    llm_client = LLMClient()
    response = llm_client.chat_completion(
        messages=[{"role": "user", "content": "Hello"}]
    )
    print(response)
