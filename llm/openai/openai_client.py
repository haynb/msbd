import openai
from ..base.llm_base import LLMBase
from .openai_chat_manager import OpenAIChatManager
from typing import Dict, Any, Callable
from tenacity import retry, stop_after_attempt, wait_fixed
import os
import base64
class OpenAIClient(LLMBase):
    """OpenAI LLM客户端"""

    def __init__(
        self,
        base_url: str = None,
        model: str = "gpt-4o-mini",
        temperature: float = 0.7,
        max_tokens: int = 2000,
        timeout: int = 60,
        interview_type: str = None,
        chat_manager: OpenAIChatManager = None,
        system_message: str = None,
        max_messages: int = 10
    ):
        super().__init__(model=model,api_key=os.getenv("OPENAI_API_KEY"), interview_type=interview_type)
        self.client = openai.OpenAI(
            base_url=base_url,
            timeout=timeout,
            api_key=self.api_key
        )
        self.temperature = temperature
        self.max_tokens = max_tokens
        self.chat_manager = chat_manager or OpenAIChatManager(system_message=system_message,interview_type=interview_type,max_messages=max_messages)
        self.functions = []
        self.function_handlers = {}
        pass
    
    @retry(stop=stop_after_attempt(3), wait=wait_fixed(1))
    def chat_completion(self, messages:str,stream=False, **kwargs):
        """执行对话补全"""
        try:
            self.chat_manager.add_user_message(messages)
            messages = self.chat_manager.get_messages()
            response = self.client.chat.completions.create(
                model=self.model,
                messages=messages,
                stream=stream,
                **kwargs
            )
            if stream:
                return self._handle_stream_response(response)
            else:
                content = response.choices[0].message.content
                self.chat_manager.add_assistant_message(content)
                return content
        except Exception as e:
            print(f"执行对话补全时发生错误: {str(e)}")
            return None
        pass

    def register_function(self, name: str, description: str, parameters: Dict[str, Any], handler: Callable[[Any], Any]):
        """注册一个可以被AI调用的函数"""
        # todo 不同模型的优化，是否可以提取到base中
        self.functions.append({
            "name": name,
            "description": description,
            "parameters": parameters
        })
        self.function_handlers[name] = handler
        pass

    def on_function_call(self,message:str):
        """让AI处理函数调用"""
        self.chat_manager.add_user_message(message)
        messages = self.chat_manager.get_messages()
        response = self.client.chat.completions.create(
            model=self.model,
            messages=messages,
            temperature=self.temperature,
            functions=self.functions,
            function_call="auto",
            stream=False
        )
        # 处理函数调用
        function_call = response.choices[0].message.function_call
        if function_call:
            function_name = function_call.name
            function_arguments = function_call.arguments
            # 添加到消息历史
            self.chat_manager.add_function_call(name=function_name, arguments=function_arguments)
            return function_name, function_arguments
        return None, None
        pass

    def use_function(self, name: str, parameters: dict):
        """执行指定已注册的函数"""
        handler = self.function_handlers.get(name)
        if handler:
            result = handler(**parameters)
            # 添加到消息历史
            self.chat_manager.add_function_result(name=name, result=result)
            return result
        return None
        pass

    def _handle_stream_response(self, response):
        """处理流式响应"""
        collected_content = []
        
        def response_generator():
            nonlocal collected_content
            try:
                for chunk in response:
                    if chunk.choices[0].delta.content is not None:
                        content = chunk.choices[0].delta.content
                        collected_content.append(content)
                        yield content
                
                # 在流式响应结束后，将完整内容添加到消息历史
                full_content = ''.join(collected_content)
                self.chat_manager.add_assistant_message(full_content)
                
            except Exception as e:
                print(f"处理流式响应时发生错误: {str(e)}")
        
        return response_generator()
        pass

    @retry(stop=stop_after_attempt(3), wait=wait_fixed(1))
    def analyze_image(self, image_path: str, prompt: str = None):
        """分析图像内容并返回结果
        
        Args:
            image_path: 图像文件路径
            prompt: 可选的提示文本，用于引导AI分析图像
            
        Returns:
            分析结果字符串
        """
        try:
            # 读取图像文件
            with open(image_path, "rb") as image_file:
                image_data = image_file.read()
                base64_image = base64.b64encode(image_data).decode('utf-8')
            
            # 准备消息内容
            if not prompt:
                prompt = "请详细分析这张图片中的内容。"
            
            # 创建带有图像的消息
            messages = [
                {"role": "user", "content": [
                    {"type": "text", "text": prompt},
                    {"type": "image_url", "image_url": {"url": f"data:image/jpeg;base64,{base64_image}"}}
                ]}
            ]
            
            # 调用API
            response = self.client.chat.completions.create(
                model="gpt-4o",  # 使用支持视觉的模型
                messages=messages,
                max_tokens=1000
            )
            
            return response.choices[0].message.content
        except Exception as e:
            print(f"分析图像时发生错误: {str(e)}")
            return f"图像分析失败: {str(e)}"