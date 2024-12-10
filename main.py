from sound_capture.sound_capture import AudioRecorder
from config.config_loader import ConfigLoader
from speech_recognition.factory.speech_recognizer_factory import SpeechRecognizerFactory
from llm.factory.llm_factory import LLMFactory
import numpy as np
import llm.base.functions as functions
import os
import time

DEFAULT_SYSTEM_PROMPT = """
你是一个求职助手，旨在帮助用户检测并回答面试过程中被提问的问题，
用户会把面试官说的话转述给你，里面可能包含了面试官的考题，或者其他内容，
你需要在判断得出这是面试官的考题的时候帮助用户，
你先给出问题的简略答案，然后详细回答并给出问题的解析。
"""  # 设置默认的系统提示词

# 创建结果处理函数
def on_sentence_end(result):
    print("==================================")
    print(result)
    
    start_time = time.time()
    response = llm_client.on_function_call(
        message=result
    )
    end_time = time.time()
    print(f"ai调用时间: {end_time - start_time}秒")
    
    if response[0]:  # 如果有函数名
        try:
            # 将字符串转换为字典
            import json
            parameters = json.loads(response[1])
            
            result = llm_client.use_function(
                name=response[0],
                parameters=parameters
            )
            
            if not result[0]:
                print("这不是面试问题")
            else:
                print("简略答案: " + str(result[1]))
                print("-----------")
                print("详细答案: " + str(result[2]))
                
        except json.JSONDecodeError as e:
            print(f"参数解析错误: {e}")
        except Exception as e:
            print(f"处理错误: {e}")
            
    print("==================================")


if __name__ == "__main__":
    # 初始化变量为None
    audio_recorder = None
    recognizer = None
    llm_client = None
    
    # 加载配置
    config = ConfigLoader()
    try:
        print("请输入这是什么类型什么专业的面试")
        interview_type = input()
        # 初始化LLM客户端
        print("初始化LLM客户端")
        llm_client = LLMFactory.create_llm_client("openai",interview_type=interview_type,model=os.getenv("OPENAI_MODEL"),system_message=DEFAULT_SYSTEM_PROMPT)
        # 注册函数
        functions.register_answer_interview_question_function(llm_client,functions.answer_interview_question)
        
        # 使用工厂类创建语音识别器
        recognizer = SpeechRecognizerFactory.create_recognizer('aliyun', on_sentence_end)
        recognizer.start_recognition()
        
        # 录制音频
        audio_recorder = AudioRecorder(recognizer)
        audio_recorder.start_recording()
    except Exception as e:
        print(f"错误: {str(e)}")
    finally:
        if audio_recorder:
            audio_recorder.stop_recording()
        if recognizer:
            recognizer.stop_recognition()


