import threading
import time
import os
from sound_capture.sound_capture import AudioRecorder
from config.config_loader import ConfigLoader
from speech_recognition.factory.speech_recognizer_factory import SpeechRecognizerFactory
from llm.factory.llm_factory import LLMFactory
import llm.base.functions as functions

class InterviewAssistantController:
    def __init__(self, ui):
        self.ui = ui
        self.audio_recorder = None
        self.recognizer = None
        self.llm_client = None
        self.image_recognition_client = None
        self.config = ConfigLoader()
        
        # 设置UI回调
        self.ui.set_callbacks(
            start_recording_callback=self.start_recording,
            stop_recording_callback=self.stop_recording,
            on_sentence_end_callback=self.on_sentence_end,
            screenshot_callback=self.analyze_screenshot
        )
        
        # 默认系统提示词
        self.DEFAULT_SYSTEM_PROMPT = """
你是一个求职助手，旨在帮助用户检测并回答面试过程中被提问的问题，
用户会把面试官说的话转述给你，里面可能包含了面试官的考题，或者其他内容，
你需要在判断得出这是面试官的考题的时候帮助用户，
你先给出问题的简略答案，然后详细回答并给出问题的解析。
"""
    
    def start_recording(self, interview_type, model_choice):
        try:
            self.ui.add_to_message_queue("status", "正在初始化...")
            
            # 初始化LLM客户端
            if model_choice == "openai":
                self.llm_client = LLMFactory.create_llm_client(
                    "openai",
                    interview_type=interview_type,
                    model=os.getenv("OPENAI_MODEL"),
                    system_message=self.DEFAULT_SYSTEM_PROMPT
                )
            else:
                self.llm_client = LLMFactory.create_llm_client(
                    "deepseek",
                    interview_type=interview_type,
                    model=os.getenv("DEEPSEEK_MODEL"),
                    system_message=self.DEFAULT_SYSTEM_PROMPT
                )
            
            # 初始化图像识别客户端
            self.init_image_recognition_client(model_choice)
            
            # 注册函数
            functions.register_answer_interview_question_function(self.llm_client, functions.answer_interview_question)
            
            # 使用工厂类创建语音识别器
            self.recognizer = SpeechRecognizerFactory.create_recognizer('aliyun', self.on_sentence_end)
            self.recognizer.start_recognition()
            
            # 录制音频
            self.audio_recorder = AudioRecorder(self.recognizer)
            self.audio_recorder.start_recording(30*60) # 30分钟
            
            self.ui.add_to_message_queue("status", "正在监听录音...")
            
        except Exception as e:
            error_message = f"启动错误: {str(e)}"
            print(error_message)
            self.ui.add_to_message_queue("error", error_message)
            self.stop_recording()
    
    def init_image_recognition_client(self, model_choice):
        """初始化图像识别客户端"""
        try:
            # 尝试创建图像识别客户端
            provider = "openai" if model_choice == "openai" else "deepseek"
            self.image_recognition_client = LLMFactory.create_image_recognition_client(provider)
        except Exception as e:
            error_message = f"图像识别初始化错误: {str(e)}"
            print(error_message)
            self.ui.add_to_message_queue("error", error_message)
    
    def analyze_screenshot(self, screenshot_path):
        """分析截图内容"""
        try:
            # 检查是否已初始化图像识别客户端
            if not self.image_recognition_client:
                # 如果没有，尝试使用OpenAI初始化
                self.image_recognition_client = LLMFactory.create_image_recognition_client("openai")
            
            # 调用图像识别客户端分析图像
            prompt = "请详细分析这张图片中的内容，描述图片中的元素并解释图片的含义。如果图片包含代码或文本，请将其提取出来。"
            result = self.image_recognition_client.analyze_image(screenshot_path, prompt)
            
            # 将结果发送到UI
            self.ui.add_to_message_queue("screenshot_result", result)
            
        except Exception as e:
            error_message = f"图像分析错误: {str(e)}"
            print(error_message)
            self.ui.add_to_message_queue("error", error_message)
            self.ui.add_to_message_queue("screenshot_result", f"无法分析图像: {str(e)}")
    
    def stop_recording(self):
        try:
            if self.audio_recorder:
                self.audio_recorder.stop_recording()
                self.audio_recorder = None
            
            if self.recognizer:
                self.recognizer.stop_recognition()
                self.recognizer = None
            
            self.ui.add_to_message_queue("status", "已停止录音")
            
        except Exception as e:
            error_message = f"停止错误: {str(e)}"
            print(error_message)
            self.ui.add_to_message_queue("error", error_message)
    
    def on_sentence_end(self, result):
        try:
            # 将识别结果添加到UI
            self.ui.add_to_message_queue("recognition", result)
            
            # 如果LLM客户端未初始化，则不处理
            if not self.llm_client:
                return
            
            # 调用LLM处理结果
            start_time = time.time()
            response = self.llm_client.on_function_call(message=result)
            end_time = time.time()
            print(f"ai调用时间: {end_time - start_time}秒")
            
            if response[0]:  # 如果有函数名
                try:
                    # 将字符串转换为字典
                    import json
                    parameters = json.loads(response[1])
                    
                    result = self.llm_client.use_function(
                        name=response[0],
                        parameters=parameters
                    )
                    
                    if not result[0]:
                        self.ui.add_to_message_queue("not_interview", "")
                    else:
                        self.ui.add_to_message_queue("ai_result", str(result[1]) + "\n\n\n" + str(result[2]))
                        
                except json.JSONDecodeError as e:
                    error_message = f"参数解析错误: {e}"
                    print(error_message)
                    self.ui.add_to_message_queue("error", error_message)
                except Exception as e:
                    error_message = f"处理错误: {e}"
                    print(error_message)
                    self.ui.add_to_message_queue("error", error_message)
        
        except Exception as e:
            error_message = f"处理识别结果错误: {str(e)}"
            print(error_message)
            self.ui.add_to_message_queue("error", error_message) 