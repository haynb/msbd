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
            from ..ali.speech_recognition import AliyunSpeechRecognizer
            return AliyunSpeechRecognizer(do_on_sentence_end)
        # 在这里添加其他提供商的支持
        else:
            raise ValueError(f"不支持的语音识别提供商: {provider}") 