from dataclasses import dataclass
from typing import Dict, Any, List


@dataclass
class Function:
    """表示一个可以被AI调用的函数"""
    name: str
    description: str
    parameters: Dict[str, Any]


class FunctionRegistry:
    """函数注册表，管理所有可以被AI调用的函数"""
    
    def __init__(self):
        self.functions: List[Function] = []
        
    def register(self, name: str, description: str, parameters: Dict[str, Any]) -> None:
        """注册一个新函数"""
        self.functions.append(Function(
            name=name,
            description=description,
            parameters=parameters
        ))
        
    def get_all(self) -> List[Dict[str, Any]]:
        """获取所有注册的函数的OpenAI格式描述"""
        if not self.functions:
            return None
            
        return [
            {
                "name": f.name,
                "description": f.description,
                "parameters": f.parameters
            }
            for f in self.functions
        ]
        
    def clear(self) -> None:
        """清空所有注册的函数"""
        self.functions.clear()
