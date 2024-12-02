import time
import threading
import nls
from config.config_loader import ConfigLoader
import os

class AliyunSpeechRecognizer:
    def __init__(self):
        # 获取配置
        config = ConfigLoader().aliyun_config
        self.appkey = config['app_key']
        self.url = "wss://nls-gateway-cn-shanghai.aliyuncs.com/ws/v1"
        
        # 初始化语音识别器
        self.recognizer = None
        self.is_running = False
        
    def on_sentence_begin(self, message, *args):
        """句子开始回调"""
        print(f"开始识别新句子...")
        
    def on_sentence_end(self, message, *args):
        """句子结束回调"""
        result = message.get('payload', {}).get('result', '')
        print(f"识别结果: {result}")
        
    def on_error(self, message, *args):
        """错误回调"""
        print(f"识别错误: {message}")
        
    def on_close(self, *args):
        """连接关闭回调"""
        print("语音识别连接已关闭")
        
    def start_recognition(self):
        """开始语音识别"""
        try:
            self.is_running = True
            self.recognizer = nls.NlsSpeechTranscriber(
                url=self.url,
                token=self.get_token(),
                appkey=self.appkey,
                on_sentence_begin=self.on_sentence_begin,
                on_sentence_end=self.on_sentence_end,
                on_error=self.on_error,
                on_close=self.on_close
            )
            
            self.recognizer.start(
                aformat="pcm",
                enable_intermediate_result=True,
                enable_punctuation_prediction=True,
                enable_inverse_text_normalization=True
            )
            
        except Exception as e:
            print(f"启动语音识别失败: {str(e)}")
            self.is_running = False
            
    def process_audio(self, audio_data):
        """处理音频数据
        
        Args:
            audio_data: 音频数据（需要是PCM格式）
        """
        if not self.is_running or not self.recognizer:
            return
            
        try:
            # 将音频数据分片发送（每片640字节）
            chunks = [audio_data[i:i+640] for i in range(0, len(audio_data), 640)]
            for chunk in chunks:
                if len(chunk) == 640:  # 确保数据块完整
                    self.recognizer.send_audio(bytes(chunk))
                    time.sleep(0.01)  # 控制发送速率
                    
        except Exception as e:
            print(f"处理音频数据失败: {str(e)}")
            
    def stop_recognition(self):
        """停止语音识别"""
        if self.recognizer:
            try:
                self.recognizer.stop()
                self.is_running = False
            except Exception as e:
                print(f"停止语音识别失败: {str(e)}")
                
    def get_token(self):
        """获取访问令牌"""
        from . import tokens
        return tokens.get_token()
    