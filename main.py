from sound_capture.sound_capture import AudioRecorder
from config.config_loader import ConfigLoader
from speech_recognition.ali.speech_recognition import AliyunSpeechRecognizer
import numpy as np
from llm.openai.client import LLMClient
import llm.openai.functions as llm_functions




def on_sentence_end(result):
    print(f"识别结果: {result}")


if __name__ == "__main__":
    # 加载配置
    config = ConfigLoader()
    # 初始化LLM客户端
    llm_client = LLMClient()
    try:
        # 创建结果处理函数
        def on_sentence_end(result):
            llm_functions.handle_speech_result(result, llm_client)
        # 初始化语音识别器
        recognizer = AliyunSpeechRecognizer(on_sentence_end)
        recognizer.start_recognition()
        # 录制音频
        audio_recorder = AudioRecorder(recognizer)
        audio_recorder.start_recording()
    except Exception as e:
        print(f"错误: {str(e)}")
    finally:
        recognizer.stop_recognition()


