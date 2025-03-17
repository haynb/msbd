import tkinter as tk
from tkinter import ttk, scrolledtext, messagebox
import threading
import queue
import time
import os
import sys
import ctypes
from ctypes import windll, wintypes
import win32gui
import win32con

class InterviewAssistantUI:
    def __init__(self, root):
        self.root = root
        self.root.title("面试助手")
        self.root.geometry("900x700")
        self.root.minsize(800, 600)
        
        # 设置主题和样式
        self.setup_theme()
        
        # 创建消息队列用于线程间通信
        self.message_queue = queue.Queue()
        
        # 创建UI组件
        self.create_widgets()
        
        # 启动消息处理
        self.process_messages()
        
        # 回调函数
        self.on_sentence_end_callback = None
        self.start_recording_callback = None
        self.stop_recording_callback = None
        
        # 状态变量
        self.is_recording = False
    
    def setup_theme(self):
        # 设置样式
        self.style = ttk.Style()
        
        # 配置颜色
        bg_color = "#f5f5f5"
        accent_color = "#4a6baf"
        text_color = "#333333"
        
        # 设置窗口背景色
        self.root.configure(bg=bg_color)
        
        # 配置各种样式
        self.style.configure("TFrame", background=bg_color)
        self.style.configure("TButton", font=("微软雅黑", 10), background=accent_color)
        self.style.configure("TLabel", font=("微软雅黑", 10), background=bg_color, foreground=text_color)
        self.style.configure("TLabelframe", background=bg_color, foreground=text_color)
        self.style.configure("TLabelframe.Label", font=("微软雅黑", 10, "bold"), background=bg_color, foreground=text_color)
        self.style.configure("TRadiobutton", background=bg_color, foreground=text_color)
        
        # 创建自定义按钮样式
        self.style.configure("Record.TButton", font=("微软雅黑", 10, "bold"), background="#28a745")
        self.style.configure("Stop.TButton", font=("微软雅黑", 10, "bold"), background="#dc3545")
        self.style.configure("Clear.TButton", font=("微软雅黑", 10), background="#6c757d")
        
    def create_widgets(self):
        # 创建主框架
        main_frame = ttk.Frame(self.root, padding="10")
        main_frame.pack(fill=tk.BOTH, expand=True)
        
        # 顶部区域 - 标题和说明
        header_frame = ttk.Frame(main_frame)
        header_frame.pack(fill=tk.X, pady=(0, 10))
        
        # 标题
        title_label = ttk.Label(header_frame, text="面试助手", font=("微软雅黑", 16, "bold"))
        title_label.pack(pady=(0, 5))
        
        # 说明
        description_label = ttk.Label(header_frame, text="帮助您回答面试问题的AI助手", font=("微软雅黑", 10))
        description_label.pack(pady=(0, 10))
        
        # 设置区域 - 面试类型选择和模型选择
        settings_frame = ttk.LabelFrame(main_frame, text="设置")
        settings_frame.pack(fill=tk.X, pady=(0, 10), padx=5)
        
        # 设置内部框架
        settings_inner_frame = ttk.Frame(settings_frame, padding=10)
        settings_inner_frame.pack(fill=tk.X)
        
        # 面试类型输入
        type_frame = ttk.Frame(settings_inner_frame)
        type_frame.pack(fill=tk.X, pady=(0, 10))
        
        ttk.Label(type_frame, text="面试类型:", width=10).pack(side=tk.LEFT, padx=(0, 5))
        self.interview_type_var = tk.StringVar()
        self.interview_type_entry = ttk.Entry(type_frame, textvariable=self.interview_type_var, width=50)
        self.interview_type_entry.pack(side=tk.LEFT, fill=tk.X, expand=True)
        self.interview_type_entry.insert(0, "例如：Java后端开发工程师")
        
        # 模型选择
        model_frame = ttk.Frame(settings_inner_frame)
        model_frame.pack(fill=tk.X)
        
        ttk.Label(model_frame, text="选择模型:", width=10).pack(side=tk.LEFT, padx=(0, 5))
        self.model_var = tk.StringVar(value="openai")
        ttk.Radiobutton(model_frame, text="OpenAI", variable=self.model_var, value="openai").pack(side=tk.LEFT, padx=(0, 10))
        ttk.Radiobutton(model_frame, text="Deepseek", variable=self.model_var, value="deepseek").pack(side=tk.LEFT)
        
        # 中间区域 - 识别结果和AI回答
        middle_frame = ttk.Frame(main_frame)
        middle_frame.pack(fill=tk.BOTH, expand=True, pady=(0, 10))
        
        # 创建左右分隔区域
        paned_window = ttk.PanedWindow(middle_frame, orient=tk.HORIZONTAL)
        paned_window.pack(fill=tk.BOTH, expand=True)
        
        # 左侧 - 识别结果
        left_frame = ttk.LabelFrame(paned_window, text="语音识别结果")
        paned_window.add(left_frame, weight=1)
        
        self.recognition_text = scrolledtext.ScrolledText(left_frame, wrap=tk.WORD, height=10, font=("微软雅黑", 10))
        self.recognition_text.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)
        
        # 右侧 - AI回答
        right_frame = ttk.LabelFrame(paned_window, text="AI回答")
        paned_window.add(right_frame, weight=1)
        
        self.ai_response_text = scrolledtext.ScrolledText(right_frame, wrap=tk.WORD, height=10, font=("微软雅黑", 10))
        self.ai_response_text.pack(fill=tk.BOTH, expand=True, padx=5, pady=5)
        
        # 底部区域 - 控制按钮
        bottom_frame = ttk.Frame(main_frame)
        bottom_frame.pack(fill=tk.X)
        
        # 状态标签
        self.status_var = tk.StringVar(value="准备开始")
        status_frame = ttk.Frame(bottom_frame)
        status_frame.pack(side=tk.LEFT, fill=tk.Y)
        
        status_label = ttk.Label(status_frame, text="状态:")
        status_label.pack(side=tk.LEFT)
        
        self.status_display = ttk.Label(status_frame, textvariable=self.status_var, font=("微软雅黑", 10, "bold"))
        self.status_display.pack(side=tk.LEFT, padx=5)
        
        # 按钮区域
        button_frame = ttk.Frame(bottom_frame)
        button_frame.pack(side=tk.RIGHT)
        
        # 清除按钮
        clear_button = ttk.Button(button_frame, text="清除记录", command=self.clear_text, style="Clear.TButton")
        clear_button.pack(side=tk.RIGHT, padx=5)
        
        # 开始/停止按钮
        self.start_button = ttk.Button(button_frame, text="开始录音", command=self.toggle_recording, style="Record.TButton")
        self.start_button.pack(side=tk.RIGHT, padx=5)
    
    def toggle_recording(self):
        if not self.is_recording:
            # 检查面试类型是否已填写
            if not self.interview_type_var.get().strip() or self.interview_type_var.get() == "例如：Java后端开发工程师":
                messagebox.showerror("错误", "请输入面试类型")
                return
                
            # 开始录音
            self.is_recording = True
            self.start_button.config(text="停止录音", style="Stop.TButton")
            self.status_var.set("正在录音...")
            self.interview_type_entry.config(state="disabled")
            
            # 调用回调函数
            if self.start_recording_callback:
                threading.Thread(target=self.start_recording_callback, 
                                args=(self.interview_type_var.get(), self.model_var.get())).start()
        else:
            # 停止录音
            self.is_recording = False
            self.start_button.config(text="开始录音", style="Record.TButton")
            self.status_var.set("已停止录音")
            self.interview_type_entry.config(state="normal")
            
            # 调用回调函数
            if self.stop_recording_callback:
                self.stop_recording_callback()
    
    def clear_text(self):
        self.recognition_text.delete(1.0, tk.END)
        self.ai_response_text.delete(1.0, tk.END)
    
    def set_callbacks(self, start_recording_callback, stop_recording_callback, on_sentence_end_callback):
        self.start_recording_callback = start_recording_callback
        self.stop_recording_callback = stop_recording_callback
        self.on_sentence_end_callback = on_sentence_end_callback
    
    def add_recognition_text(self, text):
        text = self.add_separator(text)
        self.recognition_text.insert(tk.END, text + "\n")
        self.recognition_text.see(tk.END)
    
    def add_ai_response(self, response_type, text):
        text = self.add_separator(text)
        if response_type == "result":
            self.ai_response_text.insert(tk.END, text + "\n\n")
        elif response_type == "not_interview":
            self.ai_response_text.insert(tk.END, "这不是面试问题\n\n")
        else:
            self.ai_response_text.insert(tk.END, text + "\n")
        self.ai_response_text.see(tk.END)
    
    def show_error(self, error_message):
        messagebox.showerror("错误", error_message)
        self.status_var.set("发生错误")
    
    def add_to_message_queue(self, message_type, message):
        self.message_queue.put((message_type, message))
    
    def process_messages(self):
        try:
            while not self.message_queue.empty():
                message_type, message = self.message_queue.get_nowait()
                
                if message_type == "recognition":
                    self.add_recognition_text(message)
                elif message_type == "ai_result":
                    self.add_ai_response("result", message)
                elif message_type == "not_interview":
                    self.add_ai_response("not_interview", "")
                elif message_type == "error":
                    self.show_error(message)
                elif message_type == "status":
                    self.status_var.set(message)
        except Exception as e:
            print(f"处理消息错误: {str(e)}")
        
        # 每100毫秒检查一次消息队列
        self.root.after(100, self.process_messages)

    def add_separator(self,text):
        text = "==============================\n" + text + "\n==============================\n"
        return text

def resource_path(relative_path):
    """ 获取资源的绝对路径，用于处理PyInstaller打包后的资源路径 """
    try:
        # PyInstaller创建临时文件夹，将路径存储在_MEIPASS中
        base_path = sys._MEIPASS
    except Exception:
        base_path = os.path.abspath(".")
    
    return os.path.join(base_path, relative_path)


def Hide_window(root):
    # 确保窗口已完全创建并显示在前台
    root.update_idletasks()
    root.update()
    
    # 强制窗口前置
    root.lift()
    root.focus_force()
    
    # 延迟一小段时间确保窗口真正显示
    time.sleep(0.1)
    
    # 定义常量
    WDA_NONE = 0x00000000
    WDA_EXCLUDEFROMCAPTURE = 0x00000011

    # 获取当前窗口句柄
    hwnd = win32gui.GetForegroundWindow()
    
    # 使用SetWindowDisplayAffinity函数
    result = windll.user32.SetWindowDisplayAffinity(
        hwnd,
        WDA_EXCLUDEFROMCAPTURE
    )

    if result:
        print("已成功应用防截图设置")
    else:
        error_code = ctypes.GetLastError()
        print(f"设置失败，错误码: {error_code}")
        print(f"错误详情: {ctypes.FormatError(error_code)}")
        messagebox.showerror("错误", f"防截图设置失败，错误码: {error_code}\n错误详情: {ctypes.FormatError(error_code)}")


def prevent_switch_detection(root):
    """防止窗口被检测到切屏"""
    # 确保窗口已创建
    root.update_idletasks()
    root.update()
    
    # 获取窗口句柄
    hwnd = win32gui.GetForegroundWindow()
    
    # 定义常量
    WS_EX_TOOLWINDOW = 0x00000080
    WS_EX_NOACTIVATE = 0x08000000
    
    # 修改窗口扩展风格
    # 添加WS_EX_TOOLWINDOW使窗口不出现在任务栏和Alt+Tab列表中
    # 添加WS_EX_NOACTIVATE使窗口在点击时不获取焦点
    style = win32gui.GetWindowLong(hwnd, win32con.GWL_EXSTYLE)
    win32gui.SetWindowLong(hwnd, win32con.GWL_EXSTYLE, 
                          style | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE)
    
    # 将窗口设为顶层窗口，确保始终可见
    win32gui.SetWindowPos(
        hwnd, 
        win32con.HWND_TOPMOST, 
        0, 0, 0, 0, 
        win32con.SWP_NOMOVE | win32con.SWP_NOSIZE
    )
    
    print("已应用防切屏检测设置")


def create_app():
    root = tk.Tk()
    
    # 设置应用图标 - 如果有图标文件的话
    try:
        icon_path = resource_path(os.path.join("imgs", "icon.ico"))
        if os.path.exists(icon_path):
            root.iconbitmap(icon_path)
    except Exception as e:
        # 图标加载失败不影响程序运行
        print(f"图标加载失败 (这不影响程序运行): {str(e)}")
    
    app = InterviewAssistantUI(root)
    
    # 设置防截图
    Hide_window(root)
    
    # 设置防切屏检测
    prevent_switch_detection(root)
    
    return root, app
