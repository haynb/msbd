from sound.SoundCapture import AudioRecorder

if __name__ == "__main__":
    try:
        # 录制音频，不设置时长则一直录制直到按q键
        audio_recorder = AudioRecorder()
        audio_recorder.start_recording()
        # 保存录音
        audio_recorder.save_recording()
        # 转换为文字
        text = audio_recorder.transcribe_audio()
        print(f"转换结果: {text}")
    except Exception as e:
        print(f"录音出错: {str(e)}")