import os
from typing import Optional, List, Dict, Any, Union, Callable
from dataclasses import dataclass
import openai
from openai.types.chat import ChatCompletion
from tenacity import retry, stop_after_attempt, wait_exponential


@dataclass
class Function:
    name: str
    description: str
    parameters: Dict[str, Any]


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
    """
    执行chat completion
    Args:
        client: OpenAI客户端
        model: 模型名称
        messages: 消息列表
        temperature: 温度参数
        max_tokens: 最大token数
        stream: 是否流式输出
        functions: 可用的函数列表
        function_call: 函数调用设置
        **kwargs: 其他参数
    Returns:
        ChatCompletion响应
    """
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
    def __init__(
        self,
        api_key: str = None,
        base_url: str = None,
        model: str = "gpt-4",  # 默认使用gpt-4
        temperature: float = 0.7,  # 默认温度0.7
        max_tokens: int = 2000,  # 默认最大token数2000
        timeout: int = 60,  # 默认请求超时时间60秒
        max_retries: int = 3  # 默认最大重试次数3次
    ):
        """
        初始化LLM客户端
        Args:
            api_key: OpenAI API key
            base_url: API基础URL,默认为OpenAI官方
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
        self.functions: List[Function] = []

    def register_function(
        self,
        name: str,
        description: str,
        parameters: Dict[str, Any]
    ) -> None:
        """
        注册一个可以被AI调用的函数
        Args:
            name: 函数名称
            description: 函数描述
            parameters: 函数参数schema (JSON Schema格式)
        """
        self.functions.append(Function(
            name=name,
            description=description,
            parameters=parameters
        ))

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=4, max=10),
        reraise=True
    )
    def chat_completion(
        self,
        messages: List[Dict[str, str]],
        stream: bool = False,
        auto_function_call: bool = True,
        function_handlers: Optional[Dict[str, Callable]] = None,
        return_function_call: bool = False,
        **kwargs
    ) -> Any:
        """
        调用chat completion API，支持函数调用
        Args:
            messages: 消息列表
            stream: 是否使用流式响应
            auto_function_call: 是否自动处理函数调用
            function_handlers: 函数处理器字典 {function_name: handler_function}
            return_function_call: 如果为True，当AI决定调用函数时，直接返回函数调用信息而不执行函数
            **kwargs: 其他参数
        Returns:
            - 如果return_function_call=True且AI决定调用函数：返回(function_name, function_arguments)
            - 如果auto_function_call=True：返回最终的AI回复（可能包含函数调用后的结果）
            - 其他情况：返回原始的API响应
        """
        try:
            # 如果有注册的函数，添加到请求中
            functions = None
            if self.functions:
                functions = [
                    {
                        "name": f.name,
                        "description": f.description,
                        "parameters": f.parameters
                    }
                    for f in self.functions
                ]

            response = perform_chat_completion(
                self.client,
                self.model,
                messages,
                self.temperature,
                self.max_tokens,
                stream,
                functions=functions,
                **kwargs
            )

            # 如果AI决定调用函数
            if not stream and response.choices[0].message.function_call:
                function_call = response.choices[0].message.function_call
                
                # 如果只需要返回函数调用信息
                if return_function_call:
                    import json
                    return (
                        function_call.name,
                        json.loads(function_call.arguments)
                    )
                
                # 如果需要自动执行函数
                if auto_function_call and function_handlers:
                    handler = function_handlers.get(function_call.name)
                    if handler:
                        # 执行函数调用
                        import json
                        args = json.loads(function_call.arguments)
                        result = handler(**args)
                        
                        # 将函数调用结果添加到对话中
                        messages.append({
                            "role": "assistant",
                            "content": None,
                            "function_call": {
                                "name": function_call.name,
                                "arguments": function_call.arguments,
                            },
                        })
                        messages.append({
                            "role": "function",
                            "name": function_call.name,
                            "content": str(result),
                        })
                        
                        # 继续对话
                        return self.chat_completion(
                            messages,
                            stream=stream,
                            auto_function_call=auto_function_call,
                            function_handlers=function_handlers,
                            **kwargs
                        )

            return response
        except Exception as e:
            raise Exception(f"Chat completion failed: {str(e)}")

    def stream_chat(
        self,
        prompt: str,
        system_prompt: Optional[str] = None,
        callback=None
    ):
        """
        流式对话接口
        Args:
            prompt: 用户输入
            system_prompt: 系统提示词
            callback: 用于处理流式响应的回调函数
        """
        messages = []
        if system_prompt:
            messages.append({"role": "system", "content": system_prompt})
        messages.append({"role": "user", "content": prompt})

        response = self.chat_completion(messages, stream=True)
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
