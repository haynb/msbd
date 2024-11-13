import whisper
import numpy as np
import torch

class WhisperTranscriber:
    def __init__(self, model_size="tiny"):
        """初始化Whisper转录器
        
        Args:
            model_size: 模型大小 (tiny, base, small, medium, large)
        """
        self.device = "cuda" if torch.cuda.is_available() else "cpu"
        self.model = whisper.load_model(model_size).to(self.device)
        
    def transcribe(self, audio_path=None, audio_data=None):
        """转录音频为文字
        
        Args:
            audio_path: 音频文件路径
            audio_data: 音频数据 numpy array
            
        Returns:
            str: 转录的文字
        """
        try:
            if audio_path:
                result = self.model.transcribe(audio_path, language="zh")
            elif audio_data is not None:
                audio_data = audio_data.astype(np.float32)
                if len(audio_data.shape) > 1:
                    audio_data = np.mean(audio_data, axis=1)
                result = self.model.transcribe(audio_data, language="zh")
            else:
                raise ValueError("必须提供音频文件路径或音频数据")
                
            return result["text"]
        except Exception as e:
            print(f"转录错误: {str(e)}")
            return ""
