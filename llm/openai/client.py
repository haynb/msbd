import os
from typing import Optional, List, Dict, Any, Callable, Union, Tuple
import openai
from openai.types.chat import ChatCompletion
from tenacity import retry, stop_after_attempt, wait_exponential

from .function_registry import FunctionRegistry
from .chat_manager import ChatManager, ChatMessage


def perform_chat_completion(
    client: openai.OpenAI,
    model: str,
    messages: List[Dict[str, str]],
    temperature: float,
    max_tokens: int,
    stream: bool,
    functions: Optional[List[Dict[str, Any]]] = None,
    function_call: Optional[Union[str, Dict[str, str]]] = None,
    **kwargs
) -> ChatCompletion:
    """执行chat completion"""
    return client.chat.completions.create(
        model=model,
        messages=messages,
        temperature=temperature,
        max_tokens=max_tokens,
        stream=stream,
        functions=functions,
        function_call=function_call,
        **kwargs
    )


class LLMClient:
    """OpenAI LLM客户端"""
    
    DEFAULT_SYSTEM_PROMPT = """你是一个AI助手，请帮助用户解决问题。可以多多使用提供给你的函数和工具。"""  # 设置默认的系统提示词
    
    def __init__(
        self,
        api_key: str = None,
        base_url: str = None,
        model: str = "gpt-4",
        temperature: float = 0.7,
        max_tokens: int = 2000,
        timeout: int = 60,
        max_retries: int = 3
    ):
        """
        初始化LLM客户端
        Args:
            api_key: OpenAI API key
            base_url: API基础URL
            model: 使用的模型
            temperature: 温度参数(0-2.0)
            max_tokens: 最大token数
            timeout: 请求超时时间(秒)
            max_retries: 最大重试次数
        """
        self.api_key = api_key or os.getenv("OPENAI_API_KEY")
        if not self.api_key:
            raise ValueError("API key must be provided")

        self.client = openai.OpenAI(
            api_key=self.api_key,
            base_url=base_url,
            timeout=timeout
        )

        self.model = model
        self.temperature = temperature
        self.max_tokens = max_tokens
        self.max_retries = max_retries
        
        # 初始化功能模块
        self.function_registry = FunctionRegistry()
        self.chat_manager = ChatManager(self.function_registry)
        
        # 设置固定的系统提示词
        self.chat_manager.set_system_message(self.DEFAULT_SYSTEM_PROMPT)

    def register_function(
        self,
        name: str,
        description: str,
        parameters: Dict[str, Any]
    ) -> None:
        """注册一个可以被AI调用的函数"""
        self.function_registry.register(name, description, parameters)

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=4, max=10),
        reraise=True
    )
    def chat_completion(
        self,
        messages: Union[str, List[Dict[str, str]]],
        stream: bool = False,
        auto_function_call: bool = True,
        function_handlers: Optional[Dict[str, Callable]] = None,
        return_function_call: bool = False,
        system_prompt: Optional[str] = None,
        continue_after_function_call: bool = False,
        **kwargs
    ) -> Any:
        """
        执行对话补全
        Args:
            messages: 用户消息或消息列表
            stream: 是否使用流式响应（当有函数调用时会自动禁用流式响应）
            auto_function_call: 是否自动处理函数调用
            function_handlers: 函数处理器字典
            return_function_call: 是否只返回函数调用信息
            system_prompt: 系统提示词
            continue_after_function_call: 是否在函数调用后继续对话
            **kwargs: 其他参数
        """
        # 处理输入消息
        if isinstance(messages, str):
            if system_prompt:
                self.chat_manager.set_system_message(system_prompt)
            self.chat_manager.add_user_message(messages)
            messages = self.chat_manager.get_messages()

        # 如果有注册的函数，强制关闭流式响应
        use_stream = stream and not self.function_registry.get_all()
        
        try:
            response = perform_chat_completion(
                self.client,
                self.model,
                messages,
                self.temperature,
                self.max_tokens,
                use_stream,
                functions=self.function_registry.get_all(),
                **kwargs
            )
            # 如果是流式响应，直接返回
            if use_stream:
                return response
            # 处理函数调用
            function_call = self.chat_manager.handle_function_call_response(response)
            if function_call:
                name, args = function_call
                # 如果只需要返回函数调用信息
                if return_function_call:
                    return name, args
                # 如果需要自动执行函数
                if auto_function_call and function_handlers:
                    handler = function_handlers.get(name)
                    if handler:
                        # 执行函数调用
                        result = handler(**args)
                        # 记录函数调用和结果
                        self.chat_manager.add_function_call(name, args)
                        self.chat_manager.add_function_result(name, result)
                        # 根据参数决定是否继续对话
                        if continue_after_function_call:
                            return self.chat_completion(
                                self.chat_manager.get_messages(),
                                stream=stream,
                                auto_function_call=auto_function_call,
                                function_handlers=function_handlers,
                                continue_after_function_call=continue_after_function_call,
                                **kwargs
                            )
                        else:
                            return result if result is not None else {
                                "status": "success",
                                "function": name,
                                "message": "Function executed successfully"
                            }
                    
            return response

        except Exception as e:
            raise Exception(f"Chat completion failed: {str(e)}")

    def stream_chat(
        self,
        prompt: str,
        system_prompt: Optional[str] = None,
        callback: Optional[Callable[[str], None]] = None
    ) -> str:
        """
        流式对话接口
        Args:
            prompt: 用户输入
            system_prompt: 系统提示词
            callback: 用于处理流式响应的回调函数
        """
        response = self.chat_completion(
            prompt,
            stream=True,
            system_prompt=system_prompt
        )
        
        collected_message = []
        for chunk in response:
            delta = chunk.choices[0].delta.content
            if delta:
                collected_message.append(delta)
                if callback:
                    callback(delta)
        return "".join(collected_message)

    def estimate_tokens(self, text: str) -> int:
        """
        估算文本的token数量(粗略估计)
        """
        return len(text) // 4

    def get_remaining_tokens(self, messages: List[Dict[str, str]]) -> int:
        """
        计算剩余可用token数
        """
        used_tokens = sum(self.estimate_tokens(m["content"]) for m in messages)
        return self.max_tokens - used_tokens
