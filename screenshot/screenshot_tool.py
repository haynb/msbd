import tkinter as tk
from tkinter import ttk
from PIL import ImageGrab, Image
import os
import datetime
from threading import Thread, Event
import time

class ScreenshotTool:
    def __init__(self, root, callback=None):
        """
        截图工具类
        
        Args:
            root: tkinter根窗口
            callback: 截图完成后的回调函数，接收截图路径作为参数
        """
        self.root = root
        self.callback = callback
        self.screenshot_window = None
        self.rect_id = None
        self.start_x = 0
        self.start_y = 0
        self.current_x = 0
        self.current_y = 0
        self.is_drawing = False
        self.screenshot_path = None
        
    def take_screenshot(self):
        """启动截图过程"""
        # 创建全屏透明窗口，临时禁用防截图
        if hasattr(self.root, 'attributes') and '-topmost' in self.root.attributes():
            self.topmost_state = self.root.attributes('-topmost')
            self.root.attributes('-topmost', False)
        
        # 保存窗口位置以便稍后恢复
        self.root_geometry = self.root.geometry()
        
        # 临时最小化主窗口
        self.root.iconify()
        
        # 延迟一下，确保窗口缩小
        time.sleep(0.2)
        
        # 创建全屏透明窗口进行截图
        self.create_screenshot_window()
    
    def create_screenshot_window(self):
        """创建截图窗口"""
        try:
            # 创建全屏透明窗口
            self.screenshot_window = tk.Toplevel()
            self.screenshot_window.attributes("-fullscreen", True)
            self.screenshot_window.attributes("-alpha", 0.3)  # 半透明
            self.screenshot_window.attributes("-topmost", True)
            
            # 设置窗口标题
            self.screenshot_window.title("截图")
            
            # 创建画布
            self.canvas = tk.Canvas(self.screenshot_window, cursor="cross")
            self.canvas.pack(fill=tk.BOTH, expand=True)
            
            # 设置鼠标事件
            self.canvas.bind("<ButtonPress-1>", self.on_press)
            self.canvas.bind("<B1-Motion>", self.on_drag)
            self.canvas.bind("<ButtonRelease-1>", self.on_release)
            
            # 设置键盘事件 (ESC键取消)
            self.screenshot_window.bind("<Escape>", self.cancel_screenshot)
            
            # 设置背景为灰色半透明
            self.canvas.configure(bg="#444444")
            
            # 显示提示文本
            self.canvas.create_text(
                self.screenshot_window.winfo_screenwidth() // 2,
                self.screenshot_window.winfo_screenheight() // 2,
                text="按住鼠标左键并拖动选择区域，松开完成截图\n按ESC取消",
                fill="white",
                font=("微软雅黑", 16, "bold"),
                tags="help_text"
            )
            
            # 确保窗口显示
            self.screenshot_window.update()
        except Exception as e:
            print(f"创建截图窗口失败: {e}")
            self.restore_main_window()
    
    def on_press(self, event):
        """鼠标按下事件"""
        try:
            self.is_drawing = True
            self.start_x = event.x
            self.start_y = event.y
            
            # 清除提示文本
            self.canvas.delete("help_text")
            
            # 创建矩形
            self.rect_id = self.canvas.create_rectangle(
                self.start_x, self.start_y, self.start_x, self.start_y,
                outline="red", width=2, tags="rect"
            )
        except Exception as e:
            print(f"鼠标按下事件错误: {e}")
    
    def on_drag(self, event):
        """鼠标拖动事件"""
        try:
            if self.is_drawing and self.rect_id:
                self.current_x = event.x
                self.current_y = event.y
                
                # 更新矩形
                self.canvas.coords(self.rect_id, self.start_x, self.start_y, self.current_x, self.current_y)
                
                # 显示尺寸信息
                self.canvas.delete("size_text")
                width = abs(self.current_x - self.start_x)
                height = abs(self.current_y - self.start_y)
                
                text_x = min(self.start_x, self.current_x) + width // 2
                text_y = min(self.start_y, self.current_y) - 15
                
                self.canvas.create_text(
                    text_x, text_y,
                    text=f"{width} x {height}",
                    fill="white",
                    font=("微软雅黑", 10),
                    tags="size_text"
                )
        except Exception as e:
            print(f"鼠标拖动事件错误: {e}")
    
    def on_release(self, event):
        """鼠标释放事件"""
        try:
            if self.is_drawing and self.rect_id:
                self.current_x = event.x
                self.current_y = event.y
                self.is_drawing = False
                
                # 捕获选择区域的截图
                x1 = min(self.start_x, self.current_x)
                y1 = min(self.start_y, self.current_y)
                x2 = max(self.start_x, self.current_x)
                y2 = max(self.start_y, self.current_y)
                
                # 确保截图区域有效
                if x2 - x1 > 10 and y2 - y1 > 10:
                    # 先隐藏截图窗口
                    if self.screenshot_window:
                        self.screenshot_window.withdraw()
                    
                    # 延迟一下以确保窗口真正隐藏
                    self.root.after(200, lambda: self.capture_area(x1, y1, x2, y2))
                else:
                    # 区域太小，取消截图
                    self.cancel_screenshot()
        except Exception as e:
            print(f"鼠标释放事件错误: {e}")
            self.restore_main_window()
    
    def capture_area(self, x1, y1, x2, y2):
        """捕获指定区域的截图"""
        try:
            # 确保截图目录存在
            if not os.path.exists("screenshot"):
                os.makedirs("screenshot")
            
            # 生成唯一文件名
            timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")
            self.screenshot_path = os.path.join("screenshot", f"screenshot_{timestamp}.png")
            
            # 捕获屏幕
            if x2 - x1 > 0 and y2 - y1 > 0:  # 确保区域有效
                screenshot = ImageGrab.grab(bbox=(x1, y1, x2, y2))
                screenshot.save(self.screenshot_path)
                
                # 调用回调函数
                if self.callback:
                    self.callback(self.screenshot_path)
        except Exception as e:
            print(f"截图错误: {str(e)}")
        finally:
            # 恢复主窗口
            self.restore_main_window()
    
    def cancel_screenshot(self, event=None):
        """取消截图"""
        try:
            if self.screenshot_window:
                self.screenshot_window.destroy()
                self.screenshot_window = None
            
            # 恢复主窗口
            self.restore_main_window()
        except Exception as e:
            print(f"取消截图错误: {str(e)}")
    
    def restore_main_window(self):
        """恢复主窗口"""
        try:
            # 恢复主窗口
            self.root.deiconify()
            
            # 恢复窗口位置
            if hasattr(self, 'root_geometry'):
                self.root.geometry(self.root_geometry)
            
            # 恢复置顶状态
            if hasattr(self, 'topmost_state'):
                self.root.attributes('-topmost', self.topmost_state)
            
            # 关闭截图窗口
            if self.screenshot_window:
                self.screenshot_window.destroy()
                self.screenshot_window = None
        except Exception as e:
            print(f"恢复主窗口错误: {str(e)}") 