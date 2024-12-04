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
    
    MAX_MESSAGES = 10  # 最大消息数量（不包括系统消息）
    
    def __init__(self, function_registry: FunctionRegistry):
        self.messages: List[ChatMessage] = []
        self.function_registry = function_registry
        
    def set_system_message(self, content: str) -> None:
        """设置系统消息，确保只有一个系统消息且在最前面"""
        if self.messages and self.messages[0].role == "system":
            self.messages[0].content = content
        else:
            self.messages.insert(0, ChatMessage(role="system", content=content))
            
    def add_user_message(self, content: str) -> None:
        """添加用户消息"""
        self.messages.append(ChatMessage(role="user", content=content))
        self._maintain_conversation_history()
        
    def add_assistant_message(self, content: str) -> None:
        """添加助手消息"""
        self.messages.append(ChatMessage(role="assistant", content=content))
        self._maintain_conversation_history()
        
    def add_function_call(self, name: str, arguments: Dict[str, Any]) -> None:
        """添加函数调用消息"""
        self.messages.append(ChatMessage(
            role="assistant",
            function_call={
                "name": name,
                "arguments": json.dumps(arguments)
            }
        ))
        self._maintain_conversation_history()
        
    def add_function_result(self, name: str, result: Any) -> None:
        """添加函数返回结果消息"""
        self.messages.append(ChatMessage(
            role="function",
            name=name,
            content=str(result)
        ))
        self._maintain_conversation_history()
        
    def handle_function_call_response(self, response: Any) -> Optional[Tuple[str, Dict[str, Any]]]:
        """处理包含函数调用的响应"""
        message = response.choices[0].message
        
        # 如果有函数调用
        if message.function_call:
            name = message.function_call.name
            try:
                arguments = json.loads(message.function_call.arguments)
                return name, arguments
            except json.JSONDecodeError:
                return None
        return None
        
    def get_messages(self) -> List[Dict[str, Any]]:
        """获取所有消息的OpenAI格式"""
        return [msg.to_dict() for msg in self.messages]
        
    def _maintain_conversation_history(self) -> None:
        """维护对话历史，当超出最大消息数量时删除最早的两条消息，但保留系统消息"""
        # 计算非系统消息的数量
        non_system_messages = len(self.messages) - 1  # 减去系统消息
        
        # 如果超出限制，删除最早的两条非系统消息
        if non_system_messages > self.MAX_MESSAGES:
            # 删除索引1和2的消息（保留系统消息）
            del self.messages[1:3]
            
    def clear_history(self, keep_system_message: bool = True) -> None:
        """清空聊天历史"""
        if keep_system_message and self.messages and self.messages[0].role == "system":
            system_message = self.messages[0]
            self.messages = [system_message]
        else:
            self.messages.clear()
