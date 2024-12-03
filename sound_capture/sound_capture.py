import soundcard as sc
import numpy as np
import keyboard
import time

class AudioRecorder:
    def __init__(self, model_size="medium", buffer_duration=10):
        # 基本配置
        self.samplerate = 16000
        self.channels = 1
        self.buffer_size = 3200
        self.is_recording = False
        # 添加错误处理
        try:
            self.loopback = sc.get_microphone(
                id=str(sc.default_speaker().name), 
                include_loopback=True
            )
        except Exception as e:
            print(f"初始化录音设备失败: {str(e)}")
            raise
    
    def start_recording(self, duration=None, callback=None):
        """开始捕获系统音频
        
        Args:
            duration: 捕获时长（秒）
            callback: 处理音频数据的回调函数
        """
        print("开始捕获系统音频...")
        print("按 'F1' 键停止")
        
        self.is_recording = True
        
        with self.loopback.recorder(samplerate=self.samplerate, channels=self.channels) as mic:
            start_time = time.time()
            while self.is_recording:
                try:
                    data = mic.record(numframes=self.buffer_size)
                    if callback:
                        callback(data)
                    time.sleep(0.0005)
                except Exception as e:
                    print(f"音频捕获过程中出错: {str(e)}")
                    break
                    
                if keyboard.is_pressed('F1'):
                    break
                    
                if duration and (time.time() - start_time) >= duration:
                    break
    
    def stop_recording(self):
        """停止捕获"""
        self.is_recording = False