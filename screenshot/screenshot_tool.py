import tkinter as tk
from tkinter import ttk
from PIL import ImageGrab, Image
import os
import datetime
from threading import Thread, Event
import time
import win32api
import win32con
import win32gui
import ctypes
import mss
import mss.tools
from ctypes import wintypes
from ctypes import windll

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
        # 是否在使用后删除截图文件
        self.auto_delete = True
        
        # 应用防截图和防切屏属性给主窗口
        # self.apply_anti_capture_properties(self.root)
        
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
        
        # 在最小化前确保防截图和防切屏功能已开启
        self.apply_anti_capture_properties(self.root)
        
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
    
    def create_protected_toplevel(self, title="受保护窗口"):
        """创建一个已应用防截图和防切屏属性的Toplevel窗口"""
        try:
            # 创建但不显示
            window = tk.Toplevel()
            window.withdraw()  # 关键点：先隐藏窗口
            window.title(title)
            
            # 更新窗口以确保创建
            window.update_idletasks()
            
            # 防截图设置 - 直接针对句柄操作
            hwnd = None
            # 尝试通过标题查找窗口
            if title:
                hwnd = win32gui.FindWindow(None, title)
            
            if hwnd:
                # 定义常量
                WDA_EXCLUDEFROMCAPTURE = 0x00000011
                
                # 使用SetWindowDisplayAffinity函数
                result = windll.user32.SetWindowDisplayAffinity(
                    hwnd,
                    WDA_EXCLUDEFROMCAPTURE
                )

                if result:
                    print(f"已对窗口 '{title}' 预先应用防截图设置")
                else:
                    error_code = ctypes.GetLastError()
                    print(f"预先设置防截图失败，错误码: {error_code}")
                
                # 防切屏检测
                WS_EX_TOOLWINDOW = 0x00000080
                WS_EX_NOACTIVATE = 0x08000000
                
                style = win32gui.GetWindowLong(hwnd, win32con.GWL_EXSTYLE)
                win32gui.SetWindowLong(hwnd, win32con.GWL_EXSTYLE, 
                                    style | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE)
                
                win32gui.SetWindowPos(
                    hwnd, 
                    win32con.HWND_TOPMOST, 
                    0, 0, 0, 0, 
                    win32con.SWP_NOMOVE | win32con.SWP_NOSIZE
                )
                
                print(f"已对窗口 '{title}' 预先应用防切屏检测设置")
            
            return window
        except Exception as e:
            print(f"创建受保护窗口失败: {str(e)}")
            return None
    
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
            
            # 使用防护创建函数创建窗口（保持隐藏状态）
            self.screenshot_window = self.create_protected_toplevel("截图")
            if not self.screenshot_window:
                # 如果创建失败，尝试普通创建方式
                print("使用预先防护创建函数失败，尝试常规方法...")
                self.screenshot_window = tk.Toplevel()
                self.screenshot_window.withdraw()  # 依然先隐藏
                self.screenshot_window.title("截图")
            
            # 设置窗口基本属性
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
            
            # 设置窗口位置和大小覆盖所有屏幕（仍保持隐藏）
            geometry = f"{width}x{height}+{left}+{top}"
            print(f"窗口几何参数: {geometry}")
            self.screenshot_window.geometry(geometry)
            
            # 创建画布
            self.canvas = tk.Canvas(self.screenshot_window)
            self.canvas.pack(fill=tk.BOTH, expand=True)
            
            # 设置鼠标事件
            self.canvas.bind("<ButtonPress-1>", self.on_press)
            self.canvas.bind("<B1-Motion>", self.on_drag)
            self.canvas.bind("<ButtonRelease-1>", self.on_release)
            
            # 设置键盘事件 (ESC键取消)
            self.screenshot_window.bind("<Escape>", self.cancel_screenshot)
            
            # 设置背景为轻微可见
            self.canvas.configure(bg="#F0F0F0", highlightthickness=0)
            
            # 显示提示文本 - 增大字体大小
            self.canvas.create_text(
                width // 2,
                height // 2,
                text="按住鼠标左键并拖动选择区域，松开完成截图\n按ESC取消",
                fill="black",
                font=("微软雅黑", 24, "bold"),  # 字体大小从16增加到24
                tags="help_text"
            )
            
            # 再次确保防护属性已应用
            # 使用更强大的方法直接获取窗口句柄
            hwnd = win32gui.FindWindow(None, "截图")
            if not hwnd:
                # 如果找不到，尝试获取当前前台窗口
                self.screenshot_window.update_idletasks()
                hwnd = win32gui.GetForegroundWindow()
            
            if hwnd:
                # 定义常量
                WDA_EXCLUDEFROMCAPTURE = 0x00000011
                
                # 直接应用防截图设置
                result = windll.user32.SetWindowDisplayAffinity(
                    hwnd,
                    WDA_EXCLUDEFROMCAPTURE
                )
                
                if result:
                    print("已为截图窗口强制应用防截图设置")
                else:
                    error_code = ctypes.GetLastError()
                    print(f"强制设置防截图失败，错误码: {error_code}")
            
            # 最后设置透明度并显示窗口
            self.screenshot_window.attributes("-alpha", 0.3)
            self.screenshot_window.attributes("-topmost", True)
            
            # 显示窗口
            self.screenshot_window.deiconify()
            
            # 强制窗口前置
            self.screenshot_window.lift()
            self.screenshot_window.focus_force()
            
            # 确保窗口显示并更新
            self.screenshot_window.update_idletasks()
            self.screenshot_window.update()
            
            # 手动添加防截图属性 - 作为最后的保障
            self.apply_anti_capture_properties(self.screenshot_window)
            
            # 检查防护状态
            self.check_window_protection_status("截图")
            
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
            
            # 创建矩形 - 增加线宽使其更容易看见
            self.rect_id = self.canvas.create_rectangle(
                self.start_x, self.start_y, self.start_x, self.start_y,
                outline="red", width=4, tags="rect"  # 线宽从2增加到4
            )
            
            # 显示鼠标位置提示 - 增大字体
            self.cursor_text_id = self.canvas.create_text(
                self.start_x + 15, self.start_y - 15,
                text=f"起点: ({self.start_x}, {self.start_y})",
                fill="black", 
                font=("微软雅黑", 14),  # 字体大小从9增加到14
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
                
                # 显示鼠标位置和尺寸 - 增大字体
                text = f"尺寸: {width} x {height} | 当前位置: ({self.current_x}, {self.current_y})"
                self.cursor_text_id = self.canvas.create_text(
                    self.current_x + 15, self.current_y - 15,
                    text=text,
                    fill="black", 
                    font=("微软雅黑", 14),  # 字体大小从9增加到14
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
                    # 先隐藏截图窗口和主窗口
                    if self.screenshot_window:
                        self.screenshot_window.withdraw()
                    
                    # 主窗口最小化
                    self.root.withdraw()
                    
                    # 确保窗口完全隐藏
                    self.root.update_idletasks()
                    
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
                    
                    # 短暂延迟确保窗口已完全隐藏
                    time.sleep(0.2)
                    
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
                        # 传递截图路径给回调函数
                        self.callback(self.screenshot_path)
                        
                        # 如果启用自动删除，在回调后删除截图文件
                        if self.auto_delete:
                            # 延迟一段时间后删除文件（确保回调函数有足够时间处理文件）
                            def delayed_delete():
                                try:
                                    time.sleep(3)  # 3秒延迟
                                    if os.path.exists(self.screenshot_path):
                                        os.remove(self.screenshot_path)
                                        print(f"已删除截图文件: {self.screenshot_path}")
                                except Exception as e:
                                    print(f"删除截图文件失败: {str(e)}")
                            
                            # 创建一个新线程来处理文件删除
                            delete_thread = Thread(target=delayed_delete)
                            delete_thread.daemon = True
                            delete_thread.start()
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
                            
                            # 自动删除
                            if self.auto_delete:
                                def delayed_delete():
                                    try:
                                        time.sleep(3)  # 3秒延迟
                                        if os.path.exists(self.screenshot_path):
                                            os.remove(self.screenshot_path)
                                            print(f"已删除截图文件: {self.screenshot_path}")
                                    except Exception as e:
                                        print(f"删除截图文件失败: {str(e)}")
                                
                                delete_thread = Thread(target=delayed_delete)
                                delete_thread.daemon = True
                                delete_thread.start()
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
                
            # 重新应用防截图和防切屏属性给主窗口
            self.root.update_idletasks()
            self.root.update()
            time.sleep(0.1)  # 短暂延迟确保窗口已完全恢复
            self.apply_anti_capture_properties(self.root)
            
            # 检查主窗口防护状态
            self.check_window_protection_status(self.root.title())
        except Exception as e:
            print(f"恢复主窗口错误: {str(e)}")

    def apply_anti_capture_properties(self, window):
        """给窗口应用防截图和防切屏属性"""
        try:
            # 防截图设置
            self.hide_window_from_capture(window)
            
            # 防切屏检测
            self.prevent_switch_detection(window)
        except Exception as e:
            print(f"应用防截图和防切屏属性失败: {str(e)}")
    
    def hide_window_from_capture(self, window):
        """防止窗口被截图捕获"""
        try:
            # 确保窗口已完全创建并显示在前台
            window.update_idletasks()
            window.update()
            
            # 强制窗口前置
            window.lift()
            window.focus_force()
            
            # 延迟一小段时间确保窗口真正显示
            time.sleep(0.1)
            
            # 定义常量
            WDA_NONE = 0x00000000
            WDA_EXCLUDEFROMCAPTURE = 0x00000011

            # 获取窗口句柄 - 先尝试通过标题获取
            hwnd = None
            if hasattr(window, 'title'):
                hwnd = win32gui.FindWindow(None, window.title())
            
            # 如果找不到，使用前台窗口
            if not hwnd:
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
        except Exception as e:
            print(f"设置防截图失败: {str(e)}")
            
    def prevent_switch_detection(self, window):
        """防止窗口被检测到切屏"""
        try:
            # 确保窗口已创建
            window.update_idletasks()
            window.update()
            
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
        except Exception as e:
            print(f"设置防切屏检测失败: {str(e)}")

    def check_window_protection_status(self, window_title=None):
        """检查指定窗口的防截图状态"""
        try:
            # 如果没有提供窗口标题，使用当前前台窗口
            hwnd = None
            if window_title:
                hwnd = win32gui.FindWindow(None, window_title)
            else:
                hwnd = win32gui.GetForegroundWindow()
                window_title = win32gui.GetWindowText(hwnd)
            
            if not hwnd:
                print(f"无法找到窗口: {window_title}")
                return False
            
            # 获取窗口扩展样式
            style = win32gui.GetWindowLong(hwnd, win32con.GWL_EXSTYLE)
            
            # 检查是否有防切屏属性
            has_noactivate = bool(style & 0x08000000)  # WS_EX_NOACTIVATE
            has_toolwindow = bool(style & 0x00000080)  # WS_EX_TOOLWINDOW
            
            # 检查是否有防截图属性
            # 不幸的是，无法直接读取SetWindowDisplayAffinity的状态
            # 我们只能检查窗口样式
            
            print(f"窗口 '{window_title}' 防护状态:")
            print(f"  - 防切屏 (WS_EX_NOACTIVATE): {'启用' if has_noactivate else '未启用'}")
            print(f"  - 工具窗口 (WS_EX_TOOLWINDOW): {'启用' if has_toolwindow else '未启用'}")
            
            return has_noactivate and has_toolwindow
        except Exception as e:
            print(f"检查窗口防护状态失败: {str(e)}")
            return False 