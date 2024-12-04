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
        """维护对话历史，保持在最大轮数限制内"""
        # 计算当前用户-助手对话轮数
        conversation_messages = [msg for msg in self.messages 
                              if msg.role in ("user", "assistant")]
        num_turns = len(conversation_messages) // 2
        
        # 如果超过最大轮数，删除最早的一轮对话
        while num_turns > self.MAX_CONVERSATION_TURNS:
            # 找到最早的用户消息和助手消息的索引
            user_indices = [i for i, msg in enumerate(self.messages) 
                          if msg.role == "user"]
            assistant_indices = [i for i, msg in enumerate(self.messages) 
                               if msg.role == "assistant"]
            
            if user_indices and assistant_indices:
                # 删除最早的一轮对话
                first_user_idx = user_indices[0]
                first_assistant_idx = assistant_indices[0]
                indices_to_remove = sorted([first_user_idx, first_assistant_idx], 
                                        reverse=True)
                for idx in indices_to_remove:
                    self.messages.pop(idx)
                
            num_turns -= 1

    def handle_function_call_response(self, response: Any) -> Optional[Tuple[str, Dict[str, Any]]]:
        """处理包含函数调用的响应"""
        if not hasattr(response.choices[0].message, 'function_call'):
            return None
            
        function_call = response.choices[0].message.function_call
        return (
            function_call.name,
            json.loads(function_call.arguments)
        )
