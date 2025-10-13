from typing import List, Callable, Any
from llm.base.llm_base import LLMBase


def register_answer_interview_question_function(llm_client: LLMBase, handler: Callable[[Any], Any]):
    """
    注册回答面试问题的函数
    """
    # 注册函数
    llm_client.register_function(
        name="answer_interview_question",
        description="这个函数用于判断并输出面试问题的答案给用户。用户转述了面试官的话，请你分析之后，传递是否是面试问题，如果是，则给出答案,否则，下联两个参数传递空字符串。",
        parameters={
            "type": "object",
            "properties": {
                "is_interview_question": {
                    "type": "boolean",
                    "description": "是否是面试问题",
                },
                "simplified_answer": {
                    "type": "string",
                    "description": "面试问题的简略答案",
                },
                "detailed_answer": {
                    "type": "string",
                    "description": "面试问题的详细答案",
                }
            }
        },
        handler=handler
    )
    pass

def answer_interview_question(is_interview_question: bool, simplified_answer: str, detailed_answer: str):
    return is_interview_question, simplified_answer, detailed_answer
