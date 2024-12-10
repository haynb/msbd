from ..base.llm_base import LLMBase

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
            return OpenAIClient(model=model, interview_type=interview_type,system_message=system_message)
        # 在这里添加其他提供商的支持
        else:
            raise ValueError(f"不支持的LLM提供商: {provider}") 