import soundcard as sc
import numpy as np
import keyboard
import time
import threading  # 添加threading模块导入

class AudioRecorder:
    def __init__(self, recognizer):
        # 基本配置
        self.samplerate = 16000
        self.channels = 1
        self.buffer_size = 3200
        self.is_recording = False
        self.recognizer = recognizer
        self.recording_thread = None  # 添加线程对象属性
        # 添加错误处理
        try:
            self.loopback = sc.get_microphone(
                id=str(sc.default_speaker().name), 
                include_loopback=True
            )
        except Exception as e:
            print(f"初始化录音设备失败: {str(e)}")
            raise
    
    def start_recording(self, duration=None):
        """开始捕获系统音频（非阻塞方式）
        
        Args:
            duration: 捕获时长（秒）
        """
        print("开始捕获系统音频...")
        print("按 'F1' 键停止")
        
        self.is_recording = True
        
        # 创建新线程来运行录音循环
        self.recording_thread = threading.Thread(
            target=self._recording_worker,
            args=(duration,),
            daemon=True  # 设为守护线程，这样主程序退出时它会自动终止
        )
        self.recording_thread.start()
    
    def _recording_worker(self, duration=None):
        """实际执行录音的工作线程函数"""
        try:
            with self.loopback.recorder(samplerate=self.samplerate, channels=self.channels) as mic:
                start_time = time.time()
                while self.is_recording:
                    try:
                        data = mic.record(numframes=self.buffer_size)
                        self.send_audio(data)
                    except Exception as e:
                        print(f"音频捕获过程中出错: {str(e)}")
                        break
                        
                    if keyboard.is_pressed('F1'):
                        self.is_recording = False
                        break
                        
                    if duration and (time.time() - start_time) >= duration:
                        self.is_recording = False
                        break
        except Exception as e:
            print(f"录音线程发生错误: {str(e)}")
            self.is_recording = False
    
    def stop_recording(self):
        """停止捕获"""
        self.is_recording = False
        # 可以选择等待线程结束
        if self.recording_thread and self.recording_thread.is_alive():
            self.recording_thread.join(timeout=1.0)  # 等待最多1秒

    def send_audio(self, data):
        """处理音频数据
        
        Args:
            data: 音频数据
        """
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
        self.recognizer.process_audio(pcm_data)