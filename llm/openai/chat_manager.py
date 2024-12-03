from typing import List, Dict, Any, Optional, Tuple
import json
from dataclasses import dataclass
from .function_registry import FunctionRegistry


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


class ChatManager:
    """管理聊天上下文和消息历史"""
    
    def __init__(self, function_registry: FunctionRegistry):
        self.messages: List[ChatMessage] = []
        self.function_registry = function_registry
        
    def add_message(self, message: ChatMessage) -> None:
        """添加一条消息到历史记录"""
        self.messages.append(message)
        
    def add_user_message(self, content: str) -> None:
        """添加用户消息"""
        self.add_message(ChatMessage(role="user", content=content))
        
    def add_system_message(self, content: str) -> None:
        """添加系统消息"""
        self.add_message(ChatMessage(role="system", content=content))
        
    def add_assistant_message(self, content: str) -> None:
        """添加助手消息"""
        self.add_message(ChatMessage(role="assistant", content=content))
        
    def add_function_call(self, name: str, arguments: str) -> None:
        """添加函数调用消息"""
        self.add_message(ChatMessage(
            role="assistant",
            content=None,
            function_call={"name": name, "arguments": arguments}
        ))
        
    def add_function_result(self, name: str, result: str) -> None:
        """添加函数调用结果"""
        self.add_message(ChatMessage(
            role="function",
            name=name,
            content=str(result)
        ))
        
    def get_messages(self) -> List[Dict[str, Any]]:
        """获取所有消息的OpenAI格式"""
        return [msg.to_dict() for msg in self.messages]
        
    def clear(self) -> None:
        """清空消息历史"""
        self.messages.clear()

    def handle_function_call_response(self, response: Any) -> Optional[Tuple[str, Dict[str, Any]]]:
        """处理包含函数调用的响应"""
        if not hasattr(response.choices[0].message, 'function_call'):
            return None
            
        function_call = response.choices[0].message.function_call
        return (
            function_call.name,
            json.loads(function_call.arguments)
        )
