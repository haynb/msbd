import os
from typing import Optional, List, Dict, Any
import openai
from tenacity import retry, stop_after_attempt, wait_exponential


class LLMClient:
    def __init__(
        self,
        api_key: str = None,
        base_url: str = None,
        model: str = "gpt-4o-mini",  # 默认使用gpt-4o-mini
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

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=4, max=10),
        reraise=True
    )
    def chat_completion(
        self,
        messages: List[Dict[str, str]],
        stream: bool = False,
        **kwargs
    ) -> Any:
        """
        调用chat completion API
        Args:
            messages: 消息列表
            stream: 是否使用流式响应
            **kwargs: 其他参数
        Returns:
            API响应
        """
        try:
            response = self.client.chat.completions.create(
                model=self.model,
                messages=messages,
                temperature=self.temperature,
                max_tokens=self.max_tokens,
                stream=stream,
                **kwargs
            )
            return response
        except Exception as e:
            raise Exception(f"Chat completion failed: {str(e)}")

    # 删除一个空行
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
