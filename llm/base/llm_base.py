from abc import ABC, abstractmethod
from typing import List, Dict, Any, Optional, Tuple, Callable
from dataclasses import dataclass
import os
class LLMBase(ABC):
    """LLM基类"""
    
    def __init__(self, model: str, api_key: str, interview_type: str):
        self.model = model
        self.api_key = api_key
        self.interview_type = interview_type
        if not self.api_key:
            raise ValueError("API key must be provided")

    @abstractmethod
    def chat_completion(self, messages,stream=False, **kwargs):
        """执行对话补全"""
        pass

    @abstractmethod
    def register_function(self, name: str, description: str, parameters: Dict[str, Any], handler: Callable[[Any], Any]):
        """注册一个可以被AI调用的函数"""
        pass

    @abstractmethod
    def on_function_call(self, function_call: dict):
        """让AI处理函数调用"""
        pass

    @abstractmethod
    def use_function(self, name: str, parameters: dict):
        """执行指定已注册的函数"""
        pass

    @abstractmethod
    def analyze_image(self, image_path: str, prompt: str = None):
        """分析图像内容并返回结果
        
        Args:
            image_path: 图像文件路径
            prompt: 可选的提示文本，用于引导AI分析图像
            
        Returns:
            分析结果
        """
        pass


@dataclass
class ChatMessage:
    """表示一条聊天消息"""
    role: str
    content: Optional[str] = None
    function_call: Optional[Dict[str, Any]] = None
    name: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        """转换为OpenAI API所需的格式"""
        message = {"role": self.role}
        if self.content is not None:
            message["content"] = self.content
        if self.function_call is not None:
            message["function_call"] = self.function_call
        if self.name is not None:
            message["name"] = self.name
        return message



class ChatManagerBase(ABC):
    """管理聊天上下文和消息历史"""
    @abstractmethod
    def __init__(self,max_messages: int = 10,system_message: str = None,interview_type: str = None):
        self.messages: List[ChatMessage] = []
        self.max_messages = max_messages
        if system_message:
            self.set_system_message(system_message,interview_type)
        pass

    @abstractmethod
    def set_system_message(self, content: str,interview_type: str = None) -> None:
        """设置系统消息，确保只有一个系统消息且在最前面"""
        pass
    
    @abstractmethod
    def add_user_message(self, content: str) -> None:
        """添加用户消息"""
        pass
    
    @abstractmethod
    def add_assistant_message(self, content: str) -> None:
        """添加助手消息"""
        pass
    
    @abstractmethod
    def add_function_call(self, name: str, arguments: Dict[str, Any]) -> None:
        """添加函数调用消息"""
        pass
    
    @abstractmethod
    def add_function_result(self, name: str, result: Any) -> None:
        """添加函数返回结果消息"""
        pass

    @abstractmethod
    def _maintain_conversation_history(self) -> None:
        """维护聊天历史，确保不超过最大消息数量"""
        pass

    @abstractmethod
    def get_messages(self) -> List[Dict[str, Any]]:
        """获取所有消息"""
        pass

    @abstractmethod
    def clear_history(self, keep_system_message: bool = True) -> None:
        """清空聊天历史"""
        pass

