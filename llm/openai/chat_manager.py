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
    
    MAX_CONVERSATION_TURNS = 5  # 最大对话轮数
    
    def __init__(self, function_registry: FunctionRegistry):
        self.messages: List[ChatMessage] = []
        self.function_registry = function_registry
        self.system_message: Optional[ChatMessage] = None
        
    def set_system_message(self, content: str) -> None:
        """设置系统消息"""
        self.system_message = ChatMessage(role="system", content=content)
        
    def add_message(self, message: ChatMessage) -> None:
        """添加一条消息到历史记录"""
        self.messages.append(message)
        
    def add_user_message(self, content: str) -> None:
        """添加用户消息并维护对话轮数"""
        self.add_message(ChatMessage(role="user", content=content))
        self._maintain_conversation_history()
        
    def add_system_message(self, content: str) -> None:
        """添加系统消息"""
        self.add_message(ChatMessage(role="system", content=content))
        
    def add_assistant_message(self, content: str) -> None:
        """添加助手消息并维护对话轮数"""
        self.add_message(ChatMessage(role="assistant", content=content))
        self._maintain_conversation_history()
        
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
        """获取所有消息的OpenAI格式，确保系统消息始终在最前"""
        all_messages = []
        if self.system_message:
            all_messages.append(self.system_message.to_dict())
        all_messages.extend(msg.to_dict() for msg in self.messages)
        return all_messages
        
    def clear(self) -> None:
        """清空消息历史"""
        self.messages.clear()
        
    def _maintain_conversation_history(self) -> None:
        """维护对话历史，当超出最大对话轮数时删除最早的对话"""
        # 计算当前对话轮数（一轮对话从用户消息开始）
        current_turns = 0
        user_message_indices = []
        
        for i, msg in enumerate(self.messages):
            if msg.role == "user":
                current_turns += 1
                user_message_indices.append(i)
        
        # 如果超出最大轮数限制
        while current_turns > self.MAX_CONVERSATION_TURNS and user_message_indices:
            # 获取下一个用户消息的索引（如果存在）
            next_user_index = user_message_indices[1] if len(user_message_indices) > 1 else len(self.messages)
            # 删除从开始到下一个用户消息之前的所有消息
            del self.messages[0:next_user_index]
            # 更新计数和索引列表
            current_turns -= 1
            user_message_indices.pop(0)
            user_message_indices = [i - next_user_index for i in user_message_indices]

    def handle_function_call_response(self, response: Any) -> Optional[Tuple[str, Dict[str, Any]]]:
        """处理包含函数调用的响应"""
        if not hasattr(response.choices[0].message, 'function_call'):
            return None
            
        function_call = response.choices[0].message.function_call
        return (
            function_call.name,
            json.loads(function_call.arguments)
        )
