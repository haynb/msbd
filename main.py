from sound_capture.sound_capture import AudioRecorder
from config.config_loader import ConfigLoader
from speech_recognition.ali.speech_recognition import AliyunSpeechRecognizer
import numpy as np
from llm.openai.client import LLMClient
from typing import List


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

def on_sentence_end(result):
    print(f"识别结果: {result}")

def handle_speech_result(result: str, llm_client: LLMClient):
    """处理语音识别结果"""
    print(f"识别结果: {result}")
    
    # 定义函数处理器
    def extract_keywords(text: List[str]):
        # 这里可以添加文本处理逻辑
        print(f"提取关键词: {text}")
    
    # 注册函数
    llm_client.register_function(
        name="extract_keywords",
        description="输入用户的话的关键词作为参数，函数下游会处理关键词并打印结果",
        parameters={
            "type": "object",
            "properties": {
                "text": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    },
                    "description": "关键词",
                }
            },
            "required": ["text"]
        }
    )
    
    # 调用LLM处理结果
    response = llm_client.chat_completion(
        result,
        auto_function_call=True,
        # return_function_call=True,
        function_handlers={"extract_keywords": extract_keywords}
    )
    print(f"LLM响应: {response}")
    return response

if __name__ == "__main__":
    # 加载配置
    config = ConfigLoader()

    # 初始化LLM客户端
    llm_client = LLMClient()

    try:
        # 创建结果处理函数
        def on_sentence_end(result):
            handle_speech_result(result, llm_client)

        # 初始化语音识别器
        recognizer = AliyunSpeechRecognizer(on_sentence_end)
        recognizer.start_recognition()

        # 录制音频
        audio_recorder = AudioRecorder()
        audio_recorder.start_recording(callback=process_audio_callback)

    except Exception as e:
        print(f"错误: {str(e)}")
    finally:
        recognizer.stop_recognition()


