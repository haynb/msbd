from sound.SoundCapture import AudioRecorder

if __name__ == "__main__":
    try:
        # 录制音频并实时转录
        audio_recorder = AudioRecorder()
        audio_recorder.start_recording(real_time_transcribe=True)
        # 保存录音
        audio_recorder.save_recording()
    except Exception as e:
        print(f"录音出错: {str(e)}")