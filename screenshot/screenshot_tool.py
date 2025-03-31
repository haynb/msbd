import tkinter as tk
from tkinter import ttk
from PIL import ImageGrab, Image
import os
import datetime
from threading import Thread, Event
import time
import win32api
import win32con
import mss
import mss.tools

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
        # 获取多显示器信息
        self.monitor_info = self.get_monitor_info()
        # 初始化mss
        self.sct = mss.mss()
        
    def get_monitor_info(self):
        """获取所有显示器的位置和尺寸信息"""
        monitors = []
        # 获取主显示器尺寸
        primary_monitor = {
            'left': 0,
            'top': 0,
            'width': win32api.GetSystemMetrics(win32con.SM_CXSCREEN),
            'height': win32api.GetSystemMetrics(win32con.SM_CYSCREEN),
            'right': win32api.GetSystemMetrics(win32con.SM_CXSCREEN),
            'bottom': win32api.GetSystemMetrics(win32con.SM_CYSCREEN),
            'is_primary': True
        }
        monitors.append(primary_monitor)
        
        # 通过枚举显示器获取所有显示器信息
        def callback(hMonitor, hdcMonitor, lprcMonitor, dwData):
            rect = lprcMonitor.contents
            monitor = {
                'left': rect.left,
                'top': rect.top,
                'width': rect.right - rect.left,
                'height': rect.bottom - rect.top,
                'right': rect.right,
                'bottom': rect.bottom,
                'is_primary': False
            }
            # 如果位置是(0,0)，则是主显示器
            if rect.left == 0 and rect.top == 0:
                monitor['is_primary'] = True
                # 更新主显示器信息
                monitors[0] = monitor
            else:
                monitors.append(monitor)
            return True
        
        import ctypes
        from ctypes import wintypes
        # 定义MonitorEnumProc回调函数类型
        MonitorEnumProc = ctypes.WINFUNCTYPE(ctypes.c_bool, ctypes.c_ulong, ctypes.c_ulong, 
                                           ctypes.POINTER(wintypes.RECT), ctypes.c_double)
        # 枚举显示器
        ctypes.windll.user32.EnumDisplayMonitors(None, None, MonitorEnumProc(callback), 0)
        
        # 计算所有显示器的全局边界
        if monitors:
            min_left = min(m['left'] for m in monitors)
            min_top = min(m['top'] for m in monitors)
            max_right = max(m['right'] for m in monitors)
            max_bottom = max(m['bottom'] for m in monitors)
            
            # 添加全局显示区域信息
            monitors.append({
                'left': min_left,
                'top': min_top,
                'width': max_right - min_left,
                'height': max_bottom - min_top,
                'right': max_right,
                'bottom': max_bottom,
                'is_primary': False,
                'is_global': True
            })
        
        return monitors
        
    def take_screenshot(self):
        """启动截图过程"""
        # 保存窗口位置以便稍后恢复
        self.root_geometry = self.root.geometry()
        
        # 临时最小化主窗口
        self.root.iconify()
        
        # 显示当前监视器信息
        print("可用显示器:")
        for i, monitor in enumerate(self.monitor_info):
            if i < len(self.monitor_info) - 1:  # 排除全局区域
                print(f"显示器 {i}: {monitor['width']}x{monitor['height']} @ ({monitor['left']},{monitor['top']})")
        
        # 延迟一下，确保窗口缩小
        time.sleep(0.2)
        
        # 创建全屏透明窗口进行截图
        self.create_screenshot_window()
    
    def create_screenshot_window(self):
        """创建截图窗口"""
        try:
            # 使用系统API获取虚拟屏幕信息，确保覆盖所有显示器
            x_virtual = win32api.GetSystemMetrics(win32con.SM_XVIRTUALSCREEN)
            y_virtual = win32api.GetSystemMetrics(win32con.SM_YVIRTUALSCREEN)
            width_virtual = win32api.GetSystemMetrics(win32con.SM_CXVIRTUALSCREEN)
            height_virtual = win32api.GetSystemMetrics(win32con.SM_CYVIRTUALSCREEN)
            
            # 获取全局显示区域信息（备用方案）
            global_area = self.monitor_info[-1]
            primary_area = self.monitor_info[0]
            
            # 创建全屏透明窗口
            self.screenshot_window = tk.Toplevel()
            
            # 设置窗口样式
            self.screenshot_window.attributes("-alpha", 0.2)  # 调整透明度
            self.screenshot_window.attributes("-topmost", True)
            self.screenshot_window.overrideredirect(True)  # 无边框窗口
            
            # 使用系统API获取的虚拟屏幕尺寸和位置
            width = width_virtual
            height = height_virtual
            left = x_virtual
            top = y_virtual
            
            # 确保尺寸足够大
            if width < global_area['width']:
                width = global_area['width']
            if height < global_area['height']:
                height = global_area['height']
            
            # 设置窗口位置和大小覆盖所有屏幕
            geometry = f"{width}x{height}+{left}+{top}"
            print(f"窗口几何参数: {geometry}")
            self.screenshot_window.geometry(geometry)
            
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
            
            # 设置背景为轻微可见
            self.canvas.configure(bg="#F0F0F0", highlightthickness=0)
            
            # 显示提示文本 - 确保在可见区域显示
            self.canvas.create_text(
                width // 2,
                height // 2,
                text="按住鼠标左键并拖动选择区域，松开完成截图\n按ESC取消",
                fill="black",
                font=("微软雅黑", 16, "bold"),
                tags="help_text"
            )
            
            # 确保窗口显示并更新
            self.screenshot_window.update_idletasks()
            self.screenshot_window.update()
            
            # 打印窗口实际大小
            actual_width = self.screenshot_window.winfo_width()
            actual_height = self.screenshot_window.winfo_height()
            actual_x = self.screenshot_window.winfo_x()
            actual_y = self.screenshot_window.winfo_y()
            print(f"窗口实际尺寸: {actual_width}x{actual_height}+{actual_x}+{actual_y}")
            
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
            
            # 显示鼠标位置提示
            self.cursor_text_id = self.canvas.create_text(
                self.start_x + 15, self.start_y - 15,
                text=f"起点: ({self.start_x}, {self.start_y})",
                fill="black", 
                font=("微软雅黑", 9),
                tags="cursor_text"
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
                self.canvas.delete("cursor_text")
                
                width = abs(self.current_x - self.start_x)
                height = abs(self.current_y - self.start_y)
                
                # 显示鼠标位置和尺寸
                text = f"尺寸: {width} x {height} | 当前位置: ({self.current_x}, {self.current_y})"
                self.cursor_text_id = self.canvas.create_text(
                    self.current_x + 15, self.current_y - 15,
                    text=text,
                    fill="black", 
                    font=("微软雅黑", 9),
                    tags="cursor_text"
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
                    
                    # 获取窗口位置
                    # 注意：在tkinter中，winfo_x和winfo_y返回的是窗口相对于屏幕的位置
                    window_x = self.screenshot_window.winfo_x()
                    window_y = self.screenshot_window.winfo_y()
                    
                    # 计算实际屏幕坐标
                    screen_x1 = x1 + window_x
                    screen_y1 = y1 + window_y
                    screen_x2 = x2 + window_x
                    screen_y2 = y2 + window_y
                    
                    # 打印调试信息
                    print(f"选择区域: ({x1}, {y1}) -> ({x2}, {y2})")
                    print(f"窗口位置: ({window_x}, {window_y})")
                    print(f"实际坐标: ({screen_x1}, {screen_y1}) -> ({screen_x2}, {screen_y2})")
                    
                    # 延迟一下以确保窗口真正隐藏
                    self.root.after(200, lambda: self.capture_area(screen_x1, screen_y1, screen_x2, screen_y2))
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
                try:
                    # 确保坐标在有效范围内
                    screens = self.sct.monitors
                    print(f"可用屏幕: {screens}")
                    
                    # 调整为整数坐标
                    left = int(x1)
                    top = int(y1)
                    width = int(x2 - x1)
                    height = int(y2 - y1)
                    
                    # 创建截图区域
                    monitor = {
                        "left": left,
                        "top": top,
                        "width": width,
                        "height": height
                    }
                    
                    print(f"截图区域: {monitor}")
                    
                    # 捕获屏幕
                    screenshot = self.sct.grab(monitor)
                    
                    # 转换为PIL Image并保存
                    img = Image.frombytes('RGB', screenshot.size, screenshot.rgb)
                    
                    # 检查图像是否有效
                    if img.size[0] == 0 or img.size[1] == 0:
                        print(f"错误: 截图尺寸无效 {img.size}")
                        return
                        
                    print(f"图像尺寸: {img.size}")
                    img.save(self.screenshot_path)
                    print(f"截图已保存到: {self.screenshot_path}")
                    
                    # 调用回调函数
                    if self.callback:
                        self.callback(self.screenshot_path)
                except Exception as e:
                    print(f"截图错误: {str(e)}")
                    # 尝试使用PIL的ImageGrab作为备选方案
                    try:
                        print("尝试使用备选方案截图...")
                        screenshot = ImageGrab.grab(bbox=(x1, y1, x2, y2))
                        screenshot.save(self.screenshot_path)
                        
                        # 调用回调函数
                        if self.callback:
                            self.callback(self.screenshot_path)
                    except Exception as e2:
                        print(f"备选截图也失败: {str(e2)}")
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