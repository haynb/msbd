from typing import List
from llm.openai.client import LLMClient


def register_extract_keywords_function(llm_client: LLMClient):
    """
    测试函数
    处理语音识别结果
    """    
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
    pass

# 定义函数处理器
def extract_keywords(text: List[str]):
    # 这里可以添加文本处理逻辑
    print(f"提取关键词: {text}")
    # # 调用LLM处理结果
    # response = llm_client.chat_completion(
    #     result,
    #     auto_function_call=True,
    #     # return_function_call=True,
    #     stream=True,
    #     function_handlers={"extract_keywords": extract_keywords}
    # )
    pass


def register_answer_interview_question_function(llm_client: LLMClient):
    """
    注册回答面试问题的函数
    """
    # 注册函数
    llm_client.register_function(
        name="answer_interview_question",
        description="这个函数用于判断并输出面试问题的答案给用户。用户转述了面试官的话，请你分析之后，传递是否是面试问题，如果是，则给出答案,否则，传递空字符串。",
        parameters={
            "type": "object",
            "properties": {
                "is_interview_question": {
                    "type": "boolean",
                    "description": "是否是面试问题",
                },
                "answer": {
                    "type": "string",
                    "description": "面试问题的答案",
                }
            }
        }
    )
    pass

def answer_interview_question(is_interview_question: bool, answer: str):
    if is_interview_question:
        print(f"面试问题答案: {answer}")
    else:
        print("这不是面试问题")
    
    return is_interview_question, answer



