from abc import ABC, abstractmethod

class SpeechRecognizer(ABC):
    """语音识别基类"""
    
    def __init__(self, do_on_sentence_end=None, do_on_result_chg=None):
        """
        初始化语音识别器
        Args:
            do_on_sentence_end: 句子结束时的回调函数
        """
        self.is_running = False
        self.do_on_sentence_end = do_on_sentence_end
        self.do_on_result_chg = do_on_result_chg
    @abstractmethod
    def start_recognition(self):
        """开始语音识别"""
        pass

    @abstractmethod
    def stop_recognition(self):
        """停止语音识别"""
        pass

    @abstractmethod
    def process_audio(self, audio_data):
        """
        处理音频数据
        Args:
            audio_data: 音频数据
        """
        pass

    @abstractmethod
    def on_sentence_end(self, result, *args):
        """句子结束回调"""
        pass

    @abstractmethod
    def on_result_chg(self, message, *args):
        """结果变化回调"""
        pass

class SpeechRecognizerFactory:
    """语音识别工厂类"""
    
    @staticmethod
    def create_recognizer(provider: str, do_on_sentence_end=None):
        """
        创建语音识别器实例
        Args:
            provider: 提供商名称 ('aliyun', 'other_provider'等)
            do_on_sentence_end: 句子结束时的回调函数
        Returns:
            SpeechRecognizer: 语音识别器实例
        """
        if provider.lower() == 'aliyun':
            from .ali.speech_recognition import AliyunSpeechRecognizer
            return AliyunSpeechRecognizer(do_on_sentence_end)
        # 在这里添加其他提供商的支持
        else:
            raise ValueError(f"不支持的语音识别提供商: {provider}")
