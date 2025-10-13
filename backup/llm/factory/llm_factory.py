from ..base.llm_base import LLMBase
import os
class LLMFactory:
    """LLM工厂类"""
    
    @staticmethod
    def create_llm_client(provider: str, model: str, interview_type: str,system_message: str) -> LLMBase:
        """
        创建LLM客户端实例
        Args:
            provider: 提供商名称 ('openai', 'other_provider'等)
            model: 使用的模型
            api_key: API密钥
        Returns:
            LLMBase: LLM客户端实例
        """
        if provider.lower() == 'openai':
            from ..openai.openai_client import OpenAIClient
            return OpenAIClient(model=model, interview_type=interview_type,system_message=system_message,base_url=os.getenv("OPENAI_BASE_URL"))
        elif provider.lower() == 'deepseek':
            from ..deepseek.deepseek_client import DeepSeekClient
            return DeepSeekClient(model=model, interview_type=interview_type,system_message=system_message,base_url=os.getenv("DEEPSEEK_BASE_URL"))
        # 在这里添加其他提供商的支持
        else:
            raise ValueError(f"不支持的LLM提供商: {provider}")
            
    @staticmethod
    def create_image_recognition_client(provider: str = 'openai', model: str = 'gpt-4o') -> LLMBase:
        """
        创建用于图像识别的LLM客户端实例
        
        Args:
            provider: 提供商名称 ('openai', 'deepseek'等)
            model: 使用的模型
            
        Returns:
            LLMBase: 支持图像识别的LLM客户端实例
        """
        if provider.lower() == 'openai':
            from ..openai.openai_client import OpenAIClient
            return OpenAIClient(model=model, interview_type="screenshot", 
                               system_message="你是一个图像识别助手，可以分析截图并提供详细解释。", 
                               base_url=os.getenv("OPENAI_BASE_URL"))
        elif provider.lower() == 'deepseek':
            from ..deepseek.deepseek_client import DeepSeekClient
            return DeepSeekClient(model=model, interview_type="screenshot", 
                                 system_message="你是一个图像识别助手，可以分析截图并提供详细解释。", 
                                 base_url=os.getenv("DEEPSEEK_BASE_URL"))
        else:
            raise ValueError(f"不支持的图像识别LLM提供商: {provider}") 