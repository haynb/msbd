from llm.base.llm_base import ChatManagerBase,ChatMessage
import json
from typing import Dict, Any,List

class DeepSeekChatManager(ChatManagerBase):
    """DeepSeek聊天上下文和消息历史管理"""
    def __init__(self,max_messages: int = 10,system_message: str = None,interview_type: str = None):
        super().__init__(max_messages = max_messages,system_message = system_message,interview_type = interview_type)
        pass
    
    def set_system_message(self, content: str,interview_type: str = None) -> None:
        """设置系统消息，确保只有一个系统消息且在最前面"""
        if self.messages and self.messages[0].role == "system":
            self.messages[0].content = content
        else:
            self.messages.insert(0, ChatMessage(role="system", content=content))
        pass

    def add_user_message(self, content: str) -> None:
        """添加用户消息"""
        self.messages.append(ChatMessage(role="user", content=content))
        self._maintain_conversation_history()
        pass
        
    def add_assistant_message(self, content: str) -> None:
        """添加助手消息"""
        self.messages.append(ChatMessage(role="assistant", content=content))
        self._maintain_conversation_history()
        pass
    
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
        pass
        
    def add_function_result(self, name: str, result: Any) -> None:
        """添加函数返回结果消息"""
        self.messages.append(ChatMessage(
            role="function",
            name=name,
            content=str(result)
        ))
        self._maintain_conversation_history()
        pass
    
    def _maintain_conversation_history(self) -> None:
        """维护对话历史，当超出最大消息数量时删除最早的两条消息，但保留系统消息"""
        # 计算非系统消息的数量
        non_system_messages = len(self.messages) - 1  # 减去系统消息
        
        # 如果超出限制，删除最早的两条非系统消息
        if non_system_messages > self.max_messages:
            # 删除索引1和2的消息（保留系统消息）
            del self.messages[1:3]
        pass
    
    def get_messages(self) -> List[Dict[str, Any]]:
        """获取所有消息的OpenAI格式"""
        return [msg.to_dict() for msg in self.messages]
        pass

    def clear_history(self, keep_system_message: bool = True) -> None:
        """清空聊天历史"""
        if keep_system_message and self.messages and self.messages[0].role == "system":
            system_message = self.messages[0]
            self.messages = [system_message]
        else:
            self.messages.clear()
        pass