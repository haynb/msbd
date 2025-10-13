import tkinter as tk
from ui.app_ui import create_app, Hide_window
from ui.controller import InterviewAssistantController
import sys
import os

if __name__ == "__main__":
    try:
        # 创建UI应用
        root, app = create_app()
        
        # 创建控制器
        controller = InterviewAssistantController(app)
        # 启动UI主循环
        root.mainloop()
    except Exception as e:
        print(f"程序启动错误: {str(e)}")
        sys.exit(1)


