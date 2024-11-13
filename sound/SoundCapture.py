import soundcard as sc
import soundfile as sf
import numpy as np
import keyboard
import time

class AudioRecorder:
    def __init__(self):
        # 基本配置
        self.samplerate = 44100
        self.channels = 2
        self.buffer_size = 2048
        self.audio_data = []
        self.is_recording = False
        self.loopback = sc.get_microphone(
            id=str(sc.default_speaker().name), 
            include_loopback=True
        )
        self.transcriber = None  # 懒加载转写器
    
    def start_recording(self, duration=None):
        """开始录音"""
        print("开始录音...")
        print("按 'q' 键停止录音")
        
        self.is_recording = True
        self.audio_data = []
        
        with self.loopback.recorder(samplerate=self.samplerate) as mic:
            start_time = time.time()
            while self.is_recording:
                data = mic.record(numframes=self.buffer_size)
                self.audio_data.append(data)
                
                time.sleep(0.001)
                
                if keyboard.is_pressed('q'):
                    break
                    
                if duration and (time.time() - start_time) >= duration:
                    break
    
    def stop_recording(self):
        """停止录音"""
        self.is_recording = False
    
    def transcribe_audio(self, audio_path=None, model_size="base"):
        """将录音转换为文字
        
        Args:
            audio_path: 音频文件路径（如果为None，则使用最近保存的录音）
            model_size: Whisper模型大小
            
        Returns:
            str: 转换后的文字
        """
        if self.transcriber is None:
            from ai.whisper import WhisperTranscriber
            self.transcriber = WhisperTranscriber(model_size)
            
        if audio_path is None:
            if not hasattr(self, 'last_saved_file'):
                raise ValueError("没有可用的音频文件，请先保存录音")
            audio_path = self.last_saved_file
            
        return self.transcriber.transcribe(audio_path)
    
    def save_recording(self, custom_filename=None):
        """保存录音"""
        if not self.audio_data:
            print("没有可保存的录音数据")
            return
            
        audio = np.concatenate(self.audio_data)
        filename = custom_filename or f"system_audio_{int(time.time())}.wav"
        sf.write(filename, audio, self.samplerate)
        self.last_saved_file = filename  # 保存最后保存的文件名
        print(f"录音已保存为: {filename}")