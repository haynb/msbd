from sound.sound_capture import AudioRecorder
from config.config_loader import ConfigLoader
from ai.ali.speech_recognition import AliyunSpeechRecognizer
import numpy as np

if __name__ == "__main__":
    try:
        # 加载配置
        config = ConfigLoader()
        
        # 初始化语音识别器
        recognizer = AliyunSpeechRecognizer()
        recognizer.start_recognition()
        
        # 录制音频
        audio_recorder = AudioRecorder()
        
        def process_audio_callback(data):
            # 转换为单声道
            mono_data = data.mean(axis=1)
            pcm_data = (mono_data * 32767).astype(np.int16).tobytes()
            recognizer.process_audio(pcm_data)
            
        audio_recorder.start_recording(callback=process_audio_callback)
        
    except Exception as e:
        print(f"错误: {str(e)}")
    finally:
        recognizer.stop_recognition()