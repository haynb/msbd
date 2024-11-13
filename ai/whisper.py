import whisper

class WhisperTranscriber:
    def __init__(self, model_size="base"):
        """初始化Whisper模型
        
        Args:
            model_size: 模型大小 ("tiny", "base", "small", "medium", "large")
        """
        self.model = whisper.load_model(model_size)
    
    def transcribe(self, audio_path, language="zh"):
        """将音频文件转换为文字
        
        Args:
            audio_path: 音频文件路径
            language: 音频语言 (默认中文)
            
        Returns:
            str: 转换后的文字
        """
        result = self.model.transcribe(
            audio_path,
            language=language,
            task="transcribe"
        )
        return result["text"]
