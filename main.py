from sound_capture.sound_capture import AudioRecorder
from config.config_loader import ConfigLoader
from speech_recognition.factory.speech_recognizer_factory import SpeechRecognizerFactory
import numpy as np
from llm.openai.client import LLMClient
import llm.openai.functions as llm_functions
import os
import time



# 创建结果处理函数
def on_sentence_end(result):
    # os.system('cls' if os.name == 'nt' else 'clear')
    print("==================================")
    print( result)
    # 记录当前时间
    start_time = time.time()
    # 调用LLM处理结果
    response = llm_client.chat_completion(
        result,
        auto_function_call=True,
        # return_function_call=True,
        stream=True,
        function_handlers={"answer_interview_question": llm_functions.answer_interview_question}
    )
    # 记录结束时间
    end_time = time.time()
    print(f"ai调用时间: {end_time - start_time}秒")
    if response[0]:
        # os.system('cls' if os.name == 'nt' else 'clear')
        print("简略答案: " + str(response[1]))
        print("-----------")
        print("详细答案: " + str(response[2]))
    print("==================================")
    pass


if __name__ == "__main__":
    # 加载配置
    config = ConfigLoader()
    try:
        print("请输入这是什么类型什么专业的面试")
        interview_type = input()
        # 初始化LLM客户端
        print("初始化LLM客户端")
        llm_client = LLMClient(interview_type=interview_type,model=os.getenv("OPENAI_MODEL"))
        # 注册函数
        llm_functions.register_answer_interview_question_function(llm_client)
        
        # 使用工厂类创建语音识别器
        recognizer = SpeechRecognizerFactory.create_recognizer('aliyun', on_sentence_end)
        recognizer.start_recognition()
        
        # 录制音频
        audio_recorder = AudioRecorder(recognizer)
        audio_recorder.start_recording()
    except Exception as e:
        print(f"错误: {str(e)}")
    finally:
        recognizer.stop_recognition()


