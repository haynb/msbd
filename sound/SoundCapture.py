import soundcard as sc
import soundfile as sf
import numpy as np
import keyboard
import time

class AudioRecorder:
    def __init__(self, model_size="medium", buffer_duration=10):
        # 基本配置
        self.samplerate = 44100
        self.channels = 2
        self.buffer_size = 4096
        self.audio_data = []
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
    
    def start_recording(self, duration=None):
        """开始录音
        
        Args:
            duration: 录音时长（秒）
        """
        print("开始录音...")
        print("按 'q' 键停止录音")
        
        self.is_recording = True
        self.audio_data = []
        
        with self.loopback.recorder(samplerate=self.samplerate, channels=self.channels) as mic:
            start_time = time.time()
            while self.is_recording:
                try:
                    data = mic.record(numframes=self.buffer_size)
                    self.audio_data.append(data)
                    time.sleep(0.0005)
                except Exception as e:
                    print(f"录音过程中出错: {str(e)}")
                    break
                    
                if keyboard.is_pressed('q'):
                    break
                    
                if duration and (time.time() - start_time) >= duration:
                    break
    
    def stop_recording(self):
        """停止录音"""
        self.is_recording = False
    
    def save_recording(self, custom_filename=None):
        """保存录音"""
        if not self.audio_data:
            print("没有可保存的录音数据")
            return
            
        audio = np.concatenate(self.audio_data)
        filename = custom_filename or f"system_audio_{int(time.time())}.wav"
        sf.write(filename, audio, self.samplerate)
        self.last_saved_file = filename
        print(f"录音已保存为: {filename}")