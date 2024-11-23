from sound.SoundCapture import AudioRecorder

if __name__ == "__main__":
    try:
        # 创建录音实例，使用更大的模型来提高准确性
        print("初始化录音系统...")
        print("注意：首次运行时需要下载模型，可能需要一些时间")
        audio_recorder = AudioRecorder(model_size="medium", buffer_duration=10)
        
        # 开始录音并实时转录
        print("\n=== 使用说明 ===")
        print("1. 系统将录制您的系统声音")
        print("2. 每10秒会进行一次转写")
        print("3. 按 'q' 键停止录音")
        print("===============\n")
        
        audio_recorder.start_recording(real_time_transcribe=True)
        
        # 保存录音（可选）
        save = input("\n是否保存录音？(y/n): ").lower().strip()
        if save == 'y':
            audio_recorder.save_recording()
            
    except Exception as e:
        print(f"错误: {str(e)}")