import openai
from ..base.llm_base import LLMBase
from .deepseek_chat_manager import DeepSeekChatManager
from typing import Dict, Any, Callable
from tenacity import retry, stop_after_attempt, wait_fixed
import os
class DeepSeekClient(LLMBase):
    """DeepSeek LLM客户端"""

    def __init__(
        self,
        base_url: str = None,
        model: str = "deepseek-chat",
        temperature: float = 0.7,
        max_tokens: int = 2000,
        timeout: int = 60,
        interview_type: str = None,
        chat_manager: DeepSeekChatManager = None,
        system_message: str = None,
        max_messages: int = 10
    ):
        super().__init__(model=model,api_key=os.getenv("DEEPSEEK_API_KEY"), interview_type=interview_type)
        self.client = openai.OpenAI(
            base_url=base_url,
            timeout=timeout,
            api_key=self.api_key
        )
        self.temperature = temperature
        self.max_tokens = max_tokens
        self.chat_manager = chat_manager or DeepSeekChatManager(system_message=system_message,interview_type=interview_type,max_messages=max_messages)
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
        """注册一个可以被AI调用的函数
        
        Args:
            name: 函数名称
            description: 函数描述
            parameters: 函数参数定义
            handler: 函数处理器
        """
        self.functions.append({
            "type": "function",
            "function": {
                "name": name,
                "description": description,
                "parameters": {
                    "type": "object",
                    "properties": parameters,
                    "required": True
                }
            }
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
            tools=self.functions,
            tool_choice="auto",
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