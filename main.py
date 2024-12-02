from sound.SoundCapture import AudioRecorder
from config.config_loader import ConfigLoader

if __name__ == "__main__":
    try:
        # 加载配置
        config = ConfigLoader()
        
        # 录制音频
        audio_recorder = AudioRecorder()
        audio_recorder.start_recording()
        
        # 保存录音
        audio_recorder.save_recording()
        
    except Exception as e:
        print(f"错误: {str(e)}")