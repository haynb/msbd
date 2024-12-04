from sound_capture.sound_capture import AudioRecorder
from config.config_loader import ConfigLoader
from speech_recognition.ali.speech_recognition import AliyunSpeechRecognizer
import numpy as np
from llm.openai.client import LLMClient
import llm.openai.functions as llm_functions




# 创建结果处理函数
def on_sentence_end(result):
    print("识别结果: " + result)
    # 调用LLM处理结果
    response = llm_client.chat_completion(
        result,
        auto_function_call=True,
        # return_function_call=True,
        stream=True,
        function_handlers={"answer_interview_question": llm_functions.answer_interview_question}
    )
    print("函数结果: " + str(response))
    pass


if __name__ == "__main__":
    # 加载配置
    config = ConfigLoader()
    try:
        print("请输入这是什么类型什么专业的面试")
        interview_type = input()
        # 初始化LLM客户端
        print("初始化LLM客户端")
        llm_client = LLMClient(interview_type=interview_type)
        # 注册函数
        # llm_functions.register_extract_keywords_function(llm_client)
        llm_functions.register_answer_interview_question_function(llm_client)
        # 初始化语音识别器
        recognizer = AliyunSpeechRecognizer(on_sentence_end)
        recognizer.start_recognition()
        # 录制音频
        audio_recorder = AudioRecorder(recognizer)
        audio_recorder.start_recording()
    except Exception as e:
        print(f"错误: {str(e)}")
    finally:
        recognizer.stop_recognition()


