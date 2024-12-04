from typing import List
from llm.openai.client import LLMClient

def handle_speech_result(result: str, llm_client: LLMClient):
    """
    测试函数
    处理语音识别结果
    """
    print(f"识别结果: {result}")
    
    # 定义函数处理器
    def extract_keywords(text: List[str]):
        # 这里可以添加文本处理逻辑
        print(f"提取关键词: {text}")
    
    # 注册函数
    llm_client.register_function(
        name="extract_keywords",
        description="输入用户的话的关键词作为参数，函数下游会处理关键词并打印结果",
        parameters={
            "type": "object",
            "properties": {
                "text": {
                    "type": "array",
                    "items": {
                        "type": "string"
                    },
                    "description": "关键词",
                }
            },
            "required": ["text"]
        }
    )
    
    # 调用LLM处理结果
    response = llm_client.chat_completion(
        result,
        auto_function_call=True,
        # return_function_call=True,
        function_handlers={"extract_keywords": extract_keywords}
    )
    print(f"LLM响应: {response}")
    return response