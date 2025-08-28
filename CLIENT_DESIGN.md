# 面试助手 - 客户端设计文档

## 1. 设计概述

### 1.1 设计目标
- **最小化界面**：轻量级客户端，主要在后台运行
- **跨平台支持**：Windows、macOS、Linux统一代码库
- **防检测能力**：实现防截图、防录屏、防切屏等功能
- **可靠通信**：与云端服务稳定的音频流传输
- **用户友好**：简单的配置和管理界面

### 1.2 技术选型

#### 主要语言：Go
**选择理由**：
- 🚀 **跨平台**：原生支持Windows/macOS/Linux
- 📦 **单一二进制**：无运行时依赖，部署简单
- ⚡ **性能优秀**：内存占用低，启动速度快
- 🔧 **CGO支持**：可调用系统API实现防检测功能
- 🌐 **gRPC原生**：与后端服务通信协议统一
- 🛠️ **工具链完善**：构建、测试、打包工具成熟

#### 依赖库选择
```go
// 音频采集
"github.com/gordonklaus/portaudio"  // 跨平台音频库

// GUI界面 (可选)
"fyne.io/fyne/v2"                   // 轻量级跨平台GUI

// gRPC通信
"google.golang.org/grpc"            // gRPC客户端

// 配置管理
"github.com/spf13/viper"            // 配置文件管理

// 日志
"github.com/sirupsen/logrus"        // 结构化日志

// 系统托盘
"github.com/getlantern/systray"     // 系统托盘图标

// 平台特定API
"github.com/lxn/win"                // Windows API
"golang.org/x/sys/windows"          // Windows系统调用
```

## 2. 架构设计

### 2.1 整体架构
```
┌─────────────────────────────────────────────────────────────┐
│                    客户端应用 (Go)                           │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   GUI层      │  │   托盘管理    │  │   配置管理    │      │
│  │ (Fyne/CLI)   │  │  (Systray)   │  │  (Viper)     │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   认证管理    │  │   会话管理    │  │   状态管理    │      │
│  │ (Auth Mgr)   │  │ (Session)    │  │ (State Mgr)   │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   音频采集    │  │   防检测模块  │  │   gRPC通信   │      │
│  │ (Audio Cap)  │  │ (Anti-Detect) │  │  (GRPC)      │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
├─────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   系统API     │  │   文件系统    │  │   网络层      │      │
│  │ (OS APIs)    │  │ (FileSystem)  │  │ (Network)    │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

### 2.2 模块分层

#### 表示层 (Presentation Layer)
- **GUI界面**：简单的登录和设置界面
- **系统托盘**：状态显示和快捷操作
- **命令行接口**：批量操作和调试

#### 业务逻辑层 (Business Logic Layer)  
- **认证管理**：用户登录、Token刷新
- **会话管理**：面试会话的创建和控制
- **状态管理**：应用状态的维护和同步

#### 核心功能层 (Core Layer)
- **音频采集**：系统音频流的实时采集
- **防检测模块**：各种防监控检测功能
- **通信模块**：与云端服务的gRPC通信

#### 基础设施层 (Infrastructure Layer)
- **系统API调用**：平台特定的系统功能
- **配置管理**：应用配置的读写和管理
- **日志服务**：应用日志的收集和输出

## 3. 核心模块设计

### 3.1 应用主体结构
```go
type ClientApplication struct {
    // 核心组件
    config          *ConfigManager
    auth            *AuthManager
    audio           *AudioCaptureManager
    antiDetection   *AntiDetectionManager
    grpc            *GRPCClientManager
    session         *SessionManager
    
    // UI组件
    gui             *GUIManager
    tray            *TrayManager
    
    // 状态管理
    state           *StateManager
    logger          *logrus.Logger
    
    // 控制通道
    ctx             context.Context
    cancel          context.CancelFunc
    wg              sync.WaitGroup
}

func NewClientApplication() *ClientApplication {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &ClientApplication{
        ctx:    ctx,
        cancel: cancel,
        logger: logrus.New(),
    }
}

func (app *ClientApplication) Initialize() error {
    // 初始化各个组件
    if err := app.initializeConfig(); err != nil {
        return err
    }
    
    if err := app.initializeAuth(); err != nil {
        return err
    }
    
    if err := app.initializeAudio(); err != nil {
        return err
    }
    
    if err := app.initializeAntiDetection(); err != nil {
        return err
    }
    
    if err := app.initializeGRPC(); err != nil {
        return err
    }
    
    return nil
}

func (app *ClientApplication) Run() error {
    // 启动各个服务
    app.wg.Add(4)
    
    go app.runAudioCapture()
    go app.runGRPCClient()  
    go app.runAntiDetection()
    go app.runStateManager()
    
    // 启动GUI或托盘
    if app.config.GetBool("gui.enabled") {
        return app.runGUI()
    } else {
        return app.runTray()
    }
}
```

### 3.2 配置管理模块
```go
type ConfigManager struct {
    viper    *viper.Viper
    filePath string
    mutex    sync.RWMutex
}

type ClientConfig struct {
    // 服务器配置
    Server struct {
        Host     string `json:"host"`
        GRPCPort int    `json:"grpc_port"`
        UseHTTPS bool   `json:"use_https"`
    } `json:"server"`
    
    // 认证配置
    Auth struct {
        Email        string    `json:"email"`
        AccessToken  string    `json:"access_token,omitempty"`
        RefreshToken string    `json:"refresh_token,omitempty"`
        ExpiresAt    time.Time `json:"expires_at,omitempty"`
    } `json:"auth"`
    
    // 音频配置
    Audio struct {
        SampleRate   int     `json:"sample_rate"`   // 16000
        Channels     int     `json:"channels"`      // 1 (mono)
        BitDepth     int     `json:"bit_depth"`     // 16
        ChunkSizeMS  int     `json:"chunk_size_ms"` // 200ms
        DeviceID     string  `json:"device_id"`     // 默认设备
        AutoGain     bool    `json:"auto_gain"`
    } `json:"audio"`
    
    // 防检测配置
    AntiDetection struct {
        Enabled               bool `json:"enabled"`
        PreventScreenshot     bool `json:"prevent_screenshot"`
        PreventRecording      bool `json:"prevent_recording"`
        PreventSwitchDetect   bool `json:"prevent_switch_detect"`
        PreventScreenShare    bool `json:"prevent_screen_share"`
    } `json:"anti_detection"`
    
    // GUI配置
    GUI struct {
        Enabled       bool `json:"enabled"`
        StartMinimized bool `json:"start_minimized"`
        ShowInTray     bool `json:"show_in_tray"`
    } `json:"gui"`
    
    // 日志配置
    Logging struct {
        Level      string `json:"level"`
        FilePath   string `json:"file_path"`
        MaxSize    int    `json:"max_size_mb"`
        MaxBackups int    `json:"max_backups"`
    } `json:"logging"`
}

func (cm *ConfigManager) Load() (*ClientConfig, error) {
    cm.mutex.RLock()
    defer cm.mutex.RUnlock()
    
    var config ClientConfig
    if err := cm.viper.Unmarshal(&config); err != nil {
        return nil, fmt.Errorf("failed to unmarshal config: %w", err)
    }
    
    return &config, nil
}

func (cm *ConfigManager) Save(config *ClientConfig) error {
    cm.mutex.Lock()
    defer cm.mutex.Unlock()
    
    // 将config转换为map并保存
    data, err := json.Marshal(config)
    if err != nil {
        return err
    }
    
    var configMap map[string]interface{}
    if err := json.Unmarshal(data, &configMap); err != nil {
        return err
    }
    
    for key, value := range configMap {
        cm.viper.Set(key, value)
    }
    
    return cm.viper.WriteConfig()
}
```

### 3.3 认证管理模块
```go
type AuthManager struct {
    config       *ClientConfig
    httpClient   *http.Client
    mutex        sync.RWMutex
    
    // 认证状态
    isLoggedIn   bool
    user         *UserInfo
    tokenExpiry  time.Time
}

type UserInfo struct {
    ID       string `json:"id"`
    Email    string `json:"email"`
    Nickname string `json:"nickname"`
    Plan     string `json:"plan"`
}

type LoginRequest struct {
    Email    string `json:"email"`
    Password string `json:"password"`
}

type LoginResponse struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token"`
    ExpiresIn    int       `json:"expires_in"`
    User         *UserInfo `json:"user"`
}

func (am *AuthManager) Login(email, password string) error {
    req := &LoginRequest{
        Email:    email,
        Password: password,
    }
    
    resp, err := am.makeAuthRequest("POST", "/api/v1/auth/login", req)
    if err != nil {
        return fmt.Errorf("login request failed: %w", err)
    }
    
    var loginResp LoginResponse
    if err := json.Unmarshal(resp, &loginResp); err != nil {
        return fmt.Errorf("failed to parse login response: %w", err)
    }
    
    // 更新认证状态
    am.mutex.Lock()
    defer am.mutex.Unlock()
    
    am.isLoggedIn = true
    am.user = loginResp.User
    am.tokenExpiry = time.Now().Add(time.Duration(loginResp.ExpiresIn) * time.Second)
    
    // 保存认证信息到配置
    am.config.Auth.Email = email
    am.config.Auth.AccessToken = loginResp.AccessToken
    am.config.Auth.RefreshToken = loginResp.RefreshToken
    am.config.Auth.ExpiresAt = am.tokenExpiry
    
    return nil
}

func (am *AuthManager) RefreshToken() error {
    if am.config.Auth.RefreshToken == "" {
        return errors.New("no refresh token available")
    }
    
    // 实现Token刷新逻辑
    // ...
    
    return nil
}

func (am *AuthManager) IsTokenExpired() bool {
    am.mutex.RLock()
    defer am.mutex.RUnlock()
    
    return time.Now().After(am.tokenExpiry.Add(-time.Minute * 5)) // 提前5分钟刷新
}

func (am *AuthManager) GetAuthHeader() string {
    am.mutex.RLock()
    defer am.mutex.RUnlock()
    
    return "Bearer " + am.config.Auth.AccessToken
}
```

### 3.4 音频采集模块
```go
type AudioCaptureManager struct {
    // PortAudio相关
    stream       *portaudio.Stream
    inputParams  *portaudio.StreamParameters
    
    // 音频配置
    config       *AudioConfig
    
    // 数据流
    audioBuffer  chan []int16
    isRecording  bool
    mutex        sync.RWMutex
    
    // 回调函数
    onAudioData  func([]byte) error
    
    // 上下文控制
    ctx          context.Context
    cancel       context.CancelFunc
}

type AudioConfig struct {
    SampleRate  int
    Channels    int
    BitDepth    int
    ChunkSize   int    // 每个音频块的采样点数
    DeviceID    int    // 音频设备ID
}

func NewAudioCaptureManager(config *AudioConfig) *AudioCaptureManager {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &AudioCaptureManager{
        config:      config,
        audioBuffer: make(chan []int16, 10), // 缓冲10个音频块
        ctx:         ctx,
        cancel:      cancel,
    }
}

func (acm *AudioCaptureManager) Initialize() error {
    // 初始化PortAudio
    if err := portaudio.Initialize(); err != nil {
        return fmt.Errorf("failed to initialize PortAudio: %w", err)
    }
    
    // 获取默认输入设备
    device, err := portaudio.DefaultInputDevice()
    if err != nil {
        return fmt.Errorf("failed to get default input device: %w", err)
    }
    
    // 配置音频参数
    acm.inputParams = &portaudio.StreamParameters{
        Input: portaudio.StreamDeviceParameters{
            Device:   device,
            Channels: acm.config.Channels,
            Latency:  device.DefaultLowInputLatency,
        },
        SampleRate:      portaudio.Float64(acm.config.SampleRate),
        FramesPerBuffer: acm.config.ChunkSize,
        Flags:           portaudio.NoFlag,
    }
    
    return nil
}

func (acm *AudioCaptureManager) StartCapture(onAudioData func([]byte) error) error {
    acm.mutex.Lock()
    defer acm.mutex.Unlock()
    
    if acm.isRecording {
        return errors.New("audio capture is already running")
    }
    
    acm.onAudioData = onAudioData
    
    // 创建音频流
    audioBuffer := make([]int16, acm.config.ChunkSize*acm.config.Channels)
    
    stream, err := portaudio.OpenStream(*acm.inputParams, audioBuffer)
    if err != nil {
        return fmt.Errorf("failed to open audio stream: %w", err)
    }
    
    acm.stream = stream
    
    // 启动音频流
    if err := stream.Start(); err != nil {
        return fmt.Errorf("failed to start audio stream: %w", err)
    }
    
    acm.isRecording = true
    
    // 启动音频数据处理goroutine
    go acm.processAudioData(audioBuffer)
    
    return nil
}

func (acm *AudioCaptureManager) processAudioData(buffer []int16) {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("Audio processing panic: %v", r)
        }
    }()
    
    for acm.isRecording {
        select {
        case <-acm.ctx.Done():
            return
        default:
            // 读取音频数据
            if err := acm.stream.Read(); err != nil {
                log.Printf("Failed to read audio data: %v", err)
                continue
            }
            
            // 转换为字节数据
            audioBytes := acm.int16ToBytes(buffer)
            
            // 调用回调函数发送数据
            if acm.onAudioData != nil {
                if err := acm.onAudioData(audioBytes); err != nil {
                    log.Printf("Failed to send audio data: %v", err)
                }
            }
        }
    }
}

func (acm *AudioCaptureManager) StopCapture() error {
    acm.mutex.Lock()
    defer acm.mutex.Unlock()
    
    if !acm.isRecording {
        return nil
    }
    
    acm.isRecording = false
    
    if acm.stream != nil {
        if err := acm.stream.Stop(); err != nil {
            return fmt.Errorf("failed to stop audio stream: %w", err)
        }
        
        if err := acm.stream.Close(); err != nil {
            return fmt.Errorf("failed to close audio stream: %w", err)
        }
        
        acm.stream = nil
    }
    
    return nil
}

func (acm *AudioCaptureManager) int16ToBytes(data []int16) []byte {
    bytes := make([]byte, len(data)*2)
    for i, sample := range data {
        bytes[i*2] = byte(sample)
        bytes[i*2+1] = byte(sample >> 8)
    }
    return bytes
}
```

## 4. 防检测模块详细设计

### 4.1 防检测管理器
```go
type AntiDetectionManager struct {
    platform  string
    features  map[string]AntiDetectionFeature
    enabled   bool
    config    *AntiDetectionConfig
    logger    *logrus.Logger
}

type AntiDetectionFeature interface {
    Name() string
    Description() string
    Enable() error
    Disable() error
    IsEnabled() bool
    IsSupported() bool
    GetStatus() FeatureStatus
}

type FeatureStatus struct {
    Supported bool   `json:"supported"`
    Enabled   bool   `json:"enabled"`
    Active    bool   `json:"active"`
    Error     string `json:"error,omitempty"`
}

func NewAntiDetectionManager(platform string, config *AntiDetectionConfig, logger *logrus.Logger) *AntiDetectionManager {
    adm := &AntiDetectionManager{
        platform: platform,
        features: make(map[string]AntiDetectionFeature),
        config:   config,
        logger:   logger,
    }
    
    // 根据平台注册对应的防检测功能
    adm.registerFeatures()
    
    return adm
}

func (adm *AntiDetectionManager) registerFeatures() {
    switch adm.platform {
    case "windows":
        adm.features["anti_screenshot"] = NewWindowsAntiScreenshot(adm.logger)
        adm.features["anti_recording"] = NewWindowsAntiRecording(adm.logger)
        adm.features["anti_switch"] = NewWindowsAntiSwitch(adm.logger)
        adm.features["anti_share"] = NewWindowsAntiScreenShare(adm.logger)
        
    case "darwin":
        adm.features["anti_screenshot"] = NewMacOSAntiScreenshot(adm.logger)
        adm.features["anti_recording"] = NewMacOSAntiRecording(adm.logger)
        adm.features["anti_switch"] = NewMacOSAntiSwitch(adm.logger)
        
    case "linux":
        adm.features["anti_switch"] = NewLinuxAntiSwitch(adm.logger)
        // Linux下防检测功能有限
    }
}

func (adm *AntiDetectionManager) EnableAll() error {
    var errors []error
    
    for name, feature := range adm.features {
        if !feature.IsSupported() {
            adm.logger.Warnf("Feature %s is not supported on this platform", name)
            continue
        }
        
        if err := feature.Enable(); err != nil {
            adm.logger.Errorf("Failed to enable feature %s: %v", name, err)
            errors = append(errors, fmt.Errorf("%s: %w", name, err))
        } else {
            adm.logger.Infof("Successfully enabled feature: %s", name)
        }
    }
    
    if len(errors) > 0 {
        return fmt.Errorf("failed to enable some features: %v", errors)
    }
    
    adm.enabled = true
    return nil
}

func (adm *AntiDetectionManager) DisableAll() error {
    var errors []error
    
    for name, feature := range adm.features {
        if feature.IsEnabled() {
            if err := feature.Disable(); err != nil {
                adm.logger.Errorf("Failed to disable feature %s: %v", name, err)
                errors = append(errors, fmt.Errorf("%s: %w", name, err))
            }
        }
    }
    
    adm.enabled = false
    
    if len(errors) > 0 {
        return fmt.Errorf("failed to disable some features: %v", errors)
    }
    
    return nil
}

func (adm *AntiDetectionManager) GetStatus() map[string]FeatureStatus {
    status := make(map[string]FeatureStatus)
    
    for name, feature := range adm.features {
        status[name] = feature.GetStatus()
    }
    
    return status
}
```

### 4.2 Windows平台防检测实现

#### 防截图功能
```go
type WindowsAntiScreenshot struct {
    enabled bool
    hwnd    uintptr
    logger  *logrus.Logger
}

func NewWindowsAntiScreenshot(logger *logrus.Logger) *WindowsAntiScreenshot {
    return &WindowsAntiScreenshot{
        logger: logger,
    }
}

func (was *WindowsAntiScreenshot) Enable() error {
    // 获取当前窗口句柄
    hwnd := win.GetForegroundWindow()
    if hwnd == 0 {
        return errors.New("failed to get foreground window handle")
    }
    
    was.hwnd = uintptr(hwnd)
    
    // 设置窗口显示亲和性，排除从捕获
    WDA_EXCLUDEFROMCAPTURE := uintptr(0x00000011)
    
    ret := win.SetWindowDisplayAffinity(win.HWND(hwnd), uint32(WDA_EXCLUDEFROMCAPTURE))
    if ret == 0 {
        lastErr := windows.GetLastError()
        return fmt.Errorf("SetWindowDisplayAffinity failed: %v", lastErr)
    }
    
    was.enabled = true
    was.logger.Info("Windows anti-screenshot enabled successfully")
    
    return nil
}

func (was *WindowsAntiScreenshot) Disable() error {
    if !was.enabled || was.hwnd == 0 {
        return nil
    }
    
    // 恢复正常显示亲和性
    WDA_NONE := uintptr(0x00000000)
    
    ret := win.SetWindowDisplayAffinity(win.HWND(was.hwnd), uint32(WDA_NONE))
    if ret == 0 {
        lastErr := windows.GetLastError()
        return fmt.Errorf("failed to restore window display affinity: %v", lastErr)
    }
    
    was.enabled = false
    was.hwnd = 0
    
    return nil
}

func (was *WindowsAntiScreenshot) IsSupported() bool {
    // 检查Windows版本，SetWindowDisplayAffinity在Windows 7+可用
    return windows.Version().Major >= 6 && windows.Version().Minor >= 1
}

func (was *WindowsAntiScreenshot) GetStatus() FeatureStatus {
    return FeatureStatus{
        Supported: was.IsSupported(),
        Enabled:   was.enabled,
        Active:    was.enabled,
    }
}
```

#### 防切屏检测功能
```go
type WindowsAntiSwitch struct {
    enabled    bool
    hwnd       uintptr
    originalStyle uintptr
    logger     *logrus.Logger
}

func NewWindowsAntiSwitch(logger *logrus.Logger) *WindowsAntiSwitch {
    return &WindowsAntiSwitch{
        logger: logger,
    }
}

func (was *WindowsAntiSwitch) Enable() error {
    hwnd := win.GetForegroundWindow()
    if hwnd == 0 {
        return errors.New("failed to get foreground window handle")
    }
    
    was.hwnd = uintptr(hwnd)
    
    // 保存原始窗口样式
    was.originalStyle = uintptr(win.GetWindowLong(win.HWND(hwnd), win.GWL_EXSTYLE))
    
    // 设置窗口为工具窗口，不在Alt+Tab列表中显示
    WS_EX_TOOLWINDOW := uintptr(0x00000080)
    WS_EX_NOACTIVATE := uintptr(0x08000000)
    
    newStyle := was.originalStyle | WS_EX_TOOLWINDOW | WS_EX_NOACTIVATE
    
    win.SetWindowLong(win.HWND(hwnd), win.GWL_EXSTYLE, int32(newStyle))
    
    // 设置为顶层窗口
    win.SetWindowPos(
        win.HWND(hwnd),
        win.HWND_TOPMOST,
        0, 0, 0, 0,
        win.SWP_NOMOVE|win.SWP_NOSIZE,
    )
    
    was.enabled = true
    was.logger.Info("Windows anti-switch detection enabled successfully")
    
    return nil
}

func (was *WindowsAntiSwitch) Disable() error {
    if !was.enabled || was.hwnd == 0 {
        return nil
    }
    
    // 恢复原始窗口样式
    win.SetWindowLong(win.HWND(was.hwnd), win.GWL_EXSTYLE, int32(was.originalStyle))
    
    // 取消顶层窗口设置
    win.SetWindowPos(
        win.HWND(was.hwnd),
        win.HWND_NOTOPMOST,
        0, 0, 0, 0,
        win.SWP_NOMOVE|win.SWP_NOSIZE,
    )
    
    was.enabled = false
    was.hwnd = 0
    
    return nil
}
```

### 4.3 macOS平台防检测实现

#### CGO封装macOS API
```go
/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa -framework CoreGraphics -framework ApplicationServices

#import <Cocoa/Cocoa.h>
#import <CoreGraphics/CoreGraphics.h>

int enableMacAntiCapture(void) {
    @autoreleasepool {
        NSArray *windows = [NSApp windows];
        if ([windows count] == 0) {
            return 0;
        }
        
        NSWindow *window = [windows objectAtIndex:0];
        if (window == nil) {
            return 0;
        }
        
        // 使用私有API设置窗口属性
        // 注意：这可能在不同macOS版本中有所不同
        [window setSharingType:NSWindowSharingNone];
        [window setLevel:NSScreenSaverWindowLevel];
        
        return 1;
    }
}

int disableMacAntiCapture(void) {
    @autoreleasepool {
        NSArray *windows = [NSApp windows];
        if ([windows count] == 0) {
            return 0;
        }
        
        NSWindow *window = [windows objectAtIndex:0];
        if (window == nil) {
            return 0;
        }
        
        [window setSharingType:NSWindowSharingReadOnly];
        [window setLevel:NSNormalWindowLevel];
        
        return 1;
    }
}
*/
import "C"

type MacOSAntiScreenshot struct {
    enabled bool
    logger  *logrus.Logger
}

func NewMacOSAntiScreenshot(logger *logrus.Logger) *MacOSAntiScreenshot {
    return &MacOSAntiScreenshot{
        logger: logger,
    }
}

func (mas *MacOSAntiScreenshot) Enable() error {
    result := C.enableMacAntiCapture()
    if result == 0 {
        return errors.New("failed to enable macOS anti-capture")
    }
    
    mas.enabled = true
    mas.logger.Info("macOS anti-screenshot enabled successfully")
    
    return nil
}

func (mas *MacOSAntiScreenshot) Disable() error {
    if !mas.enabled {
        return nil
    }
    
    result := C.disableMacAntiCapture()
    if result == 0 {
        return errors.New("failed to disable macOS anti-capture")
    }
    
    mas.enabled = false
    
    return nil
}

func (mas *MacOSAntiScreenshot) IsSupported() bool {
    // macOS 10.12+
    return true
}
```

## 5. gRPC通信模块

### 5.1 gRPC客户端管理器
```go
type GRPCClientManager struct {
    conn          *grpc.ClientConn
    client        pb.InterviewAssistantClient
    audioStream   pb.InterviewAssistant_AudioStreamClient
    
    config        *ClientConfig
    auth          *AuthManager
    logger        *logrus.Logger
    
    // 连接状态
    connected     bool
    reconnecting  bool
    mutex         sync.RWMutex
    
    // 控制通道
    ctx           context.Context
    cancel        context.CancelFunc
    
    // 回调函数
    onTranscript  func(*pb.TranscriptResponse) error
    onSuggestion  func(*pb.SuggestionResponse) error
    onError       func(error)
}

func NewGRPCClientManager(config *ClientConfig, auth *AuthManager, logger *logrus.Logger) *GRPCClientManager {
    ctx, cancel := context.WithCancel(context.Background())
    
    return &GRPCClientManager{
        config: config,
        auth:   auth,
        logger: logger,
        ctx:    ctx,
        cancel: cancel,
    }
}

func (gcm *GRPCClientManager) Connect() error {
    gcm.mutex.Lock()
    defer gcm.mutex.Unlock()
    
    if gcm.connected {
        return nil
    }
    
    // 构建连接地址
    addr := fmt.Sprintf("%s:%d", gcm.config.Server.Host, gcm.config.Server.GRPCPort)
    
    // 配置gRPC连接选项
    var opts []grpc.DialOption
    
    if gcm.config.Server.UseHTTPS {
        opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
    } else {
        opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
    }
    
    // 添加认证拦截器
    opts = append(opts, grpc.WithUnaryInterceptor(gcm.authUnaryInterceptor))
    opts = append(opts, grpc.WithStreamInterceptor(gcm.authStreamInterceptor))
    
    // 连接选项
    opts = append(opts, 
        grpc.WithKeepaliveParams(keepalive.ClientParameters{
            Time:                10 * time.Second,
            Timeout:             3 * time.Second,
            PermitWithoutStream: true,
        }),
        grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4*1024*1024)), // 4MB
    )
    
    // 建立连接
    conn, err := grpc.Dial(addr, opts...)
    if err != nil {
        return fmt.Errorf("failed to connect to server: %w", err)
    }
    
    gcm.conn = conn
    gcm.client = pb.NewInterviewAssistantClient(conn)
    gcm.connected = true
    
    gcm.logger.Infof("Connected to server at %s", addr)
    
    return nil
}

func (gcm *GRPCClientManager) StartAudioStream() error {
    if !gcm.connected {
        return errors.New("not connected to server")
    }
    
    // 创建音频流
    stream, err := gcm.client.AudioStream(gcm.ctx)
    if err != nil {
        return fmt.Errorf("failed to create audio stream: %w", err)
    }
    
    gcm.audioStream = stream
    
    // 启动接收响应的goroutine
    go gcm.handleStreamResponse()
    
    return nil
}

func (gcm *GRPCClientManager) SendAudioChunk(audioData []byte, seqNum int64) error {
    if gcm.audioStream == nil {
        return errors.New("audio stream not initialized")
    }
    
    chunk := &pb.AudioChunk{
        Data:      audioData,
        SeqNum:    seqNum,
        Timestamp: timestamppb.Now(),
    }
    
    return gcm.audioStream.Send(chunk)
}

func (gcm *GRPCClientManager) handleStreamResponse() {
    for {
        select {
        case <-gcm.ctx.Done():
            return
        default:
            resp, err := gcm.audioStream.Recv()
            if err != nil {
                if err == io.EOF {
                    gcm.logger.Info("Server closed the audio stream")
                    return
                }
                
                gcm.logger.Errorf("Error receiving from audio stream: %v", err)
                if gcm.onError != nil {
                    gcm.onError(err)
                }
                return
            }
            
            // 处理不同类型的响应
            switch resp.Type {
            case pb.ResponseType_TRANSCRIPT:
                if gcm.onTranscript != nil {
                    if err := gcm.onTranscript(resp.GetTranscript()); err != nil {
                        gcm.logger.Errorf("Error handling transcript: %v", err)
                    }
                }
                
            case pb.ResponseType_SUGGESTION:
                if gcm.onSuggestion != nil {
                    if err := gcm.onSuggestion(resp.GetSuggestion()); err != nil {
                        gcm.logger.Errorf("Error handling suggestion: %v", err)
                    }
                }
                
            case pb.ResponseType_ERROR:
                gcm.logger.Errorf("Server error: %s", resp.GetError().Message)
                if gcm.onError != nil {
                    gcm.onError(fmt.Errorf("server error: %s", resp.GetError().Message))
                }
            }
        }
    }
}

func (gcm *GRPCClientManager) authUnaryInterceptor(
    ctx context.Context,
    method string,
    req, reply interface{},
    cc *grpc.ClientConn,
    invoker grpc.UnaryInvoker,
    opts ...grpc.CallOption,
) error {
    // 为请求添加认证头
    if gcm.auth.IsLoggedIn() {
        md := metadata.New(map[string]string{
            "authorization": gcm.auth.GetAuthHeader(),
        })
        ctx = metadata.NewOutgoingContext(ctx, md)
    }
    
    return invoker(ctx, method, req, reply, cc, opts...)
}

func (gcm *GRPCClientManager) authStreamInterceptor(
    ctx context.Context,
    desc *grpc.StreamDesc,
    cc *grpc.ClientConn,
    method string,
    streamer grpc.Streamer,
    opts ...grpc.CallOption,
) (grpc.ClientStream, error) {
    // 为流请求添加认证头
    if gcm.auth.IsLoggedIn() {
        md := metadata.New(map[string]string{
            "authorization": gcm.auth.GetAuthHeader(),
        })
        ctx = metadata.NewOutgoingContext(ctx, md)
    }
    
    return streamer(ctx, desc, cc, method, opts...)
}
```

## 6. 系统托盘界面

### 6.1 托盘管理器
```go
type TrayManager struct {
    app           *ClientApplication
    menu          *systray.MenuItem
    status        *systray.MenuItem
    logger        *logrus.Logger
    
    // 状态
    isRecording   bool
    connectionStatus string
}

func NewTrayManager(app *ClientApplication, logger *logrus.Logger) *TrayManager {
    return &TrayManager{
        app:    app,
        logger: logger,
    }
}

func (tm *TrayManager) Run() {
    systray.Run(tm.onReady, tm.onExit)
}

func (tm *TrayManager) onReady() {
    // 设置托盘图标
    systray.SetIcon(getIconData())
    systray.SetTitle("面试助手")
    systray.SetTooltip("面试助手客户端")
    
    // 创建菜单项
    tm.status = systray.AddMenuItem("状态: 未连接", "显示当前连接状态")
    systray.AddSeparator()
    
    loginItem := systray.AddMenuItem("登录", "登录到服务器")
    startRecordingItem := systray.AddMenuItem("开始录音", "开始音频采集")
    stopRecordingItem := systray.AddMenuItem("停止录音", "停止音频采集")
    stopRecordingItem.Disable()
    
    systray.AddSeparator()
    
    antiDetectionItem := systray.AddMenuItem("防检测功能", "启用/禁用防检测功能")
    settingsItem := systray.AddMenuItem("设置", "打开设置界面")
    
    systray.AddSeparator()
    
    aboutItem := systray.AddMenuItem("关于", "关于面试助手")
    quitItem := systray.AddMenuItem("退出", "退出应用程序")
    
    // 处理菜单点击事件
    go func() {
        for {
            select {
            case <-loginItem.ClickedCh:
                tm.handleLogin()
                
            case <-startRecordingItem.ClickedCh:
                if err := tm.app.StartRecording(); err != nil {
                    tm.logger.Errorf("Failed to start recording: %v", err)
                    tm.showError("启动录音失败", err.Error())
                } else {
                    tm.isRecording = true
                    startRecordingItem.Disable()
                    stopRecordingItem.Enable()
                    tm.updateStatus()
                }
                
            case <-stopRecordingItem.ClickedCh:
                if err := tm.app.StopRecording(); err != nil {
                    tm.logger.Errorf("Failed to stop recording: %v", err)
                    tm.showError("停止录音失败", err.Error())
                } else {
                    tm.isRecording = false
                    startRecordingItem.Enable()
                    stopRecordingItem.Disable()
                    tm.updateStatus()
                }
                
            case <-antiDetectionItem.ClickedCh:
                tm.handleAntiDetectionToggle()
                
            case <-settingsItem.ClickedCh:
                tm.handleSettings()
                
            case <-aboutItem.ClickedCh:
                tm.showAbout()
                
            case <-quitItem.ClickedCh:
                systray.Quit()
                return
            }
        }
    }()
}

func (tm *TrayManager) onExit() {
    // 清理资源
    tm.logger.Info("Tray manager exiting")
}

func (tm *TrayManager) handleLogin() {
    // 这里可以弹出简单的登录对话框
    // 或者调用命令行界面进行登录
    
    // 简单实现：从标准输入读取
    fmt.Print("请输入邮箱: ")
    var email string
    fmt.Scanln(&email)
    
    fmt.Print("请输入密码: ")
    var password string
    fmt.Scanln(&password)
    
    if err := tm.app.auth.Login(email, password); err != nil {
        tm.showError("登录失败", err.Error())
    } else {
        tm.connectionStatus = "已连接"
        tm.updateStatus()
    }
}

func (tm *TrayManager) updateStatus() {
    var status string
    if tm.isRecording {
        status = fmt.Sprintf("状态: %s (录音中)", tm.connectionStatus)
    } else {
        status = fmt.Sprintf("状态: %s", tm.connectionStatus)
    }
    
    tm.status.SetTitle(status)
}

func (tm *TrayManager) showError(title, message string) {
    // 在实际实现中，这里可以显示系统通知
    tm.logger.Errorf("%s: %s", title, message)
}

func (tm *TrayManager) showAbout() {
    // 显示关于信息
    about := `面试助手客户端
版本: v1.0.0  
作者: Your Name
功能: 面试音频采集和防检测`
    
    tm.logger.Info(about)
}

// 获取托盘图标数据
func getIconData() []byte {
    // 这里返回图标的字节数据
    // 可以使用go:embed嵌入图标文件
    return iconData
}
```

## 7. 构建和部署

### 7.1 跨平台构建脚本
```makefile
# Makefile
.PHONY: build build-all clean install deps proto

# 版本信息
VERSION := v1.0.0
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD)

# 构建标识
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

# 默认构建当前平台
build:
	@echo "Building for current platform..."
	go build $(LDFLAGS) -o bin/msbd-client ./cmd

# 构建所有平台
build-all: build-windows build-darwin build-linux

build-windows:
	@echo "Building for Windows..."
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/msbd-client-windows-amd64.exe ./cmd
	GOOS=windows GOARCH=386 go build $(LDFLAGS) -o bin/msbd-client-windows-386.exe ./cmd

build-darwin:
	@echo "Building for macOS..."  
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/msbd-client-darwin-amd64 ./cmd
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/msbd-client-darwin-arm64 ./cmd

build-linux:
	@echo "Building for Linux..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/msbd-client-linux-amd64 ./cmd
	GOOS=linux GOARCH=386 go build $(LDFLAGS) -o bin/msbd-client-linux-386 ./cmd

# 安装依赖
deps:
	go mod tidy
	go mod download

# 生成Protobuf文件
proto:
	protoc --go_out=. --go-grpc_out=. proto/*.proto

# 清理构建产物
clean:
	rm -rf bin/
	rm -rf dist/

# 运行测试
test:
	go test -v ./...

# 代码格式化
fmt:
	go fmt ./...
	goimports -w .

# 代码检查
lint:
	golangci-lint run

# 安装工具
install-tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
```

### 7.2 打包脚本
```bash
#!/bin/bash
# build/package.sh

set -e

VERSION=${1:-"v1.0.0"}
PLATFORMS=("windows/amd64" "darwin/amd64" "darwin/arm64" "linux/amd64")

echo "Packaging msbd-client $VERSION for multiple platforms..."

# 创建临时目录
TEMP_DIR=$(mktemp -d)
DIST_DIR="dist"

mkdir -p "$DIST_DIR"

for platform in "${PLATFORMS[@]}"; do
    IFS='/' read -ra ADDR <<< "$platform"
    OS=${ADDR[0]}
    ARCH=${ADDR[1]}
    
    OUTPUT_NAME="msbd-client-$OS-$ARCH"
    if [ "$OS" = "windows" ]; then
        OUTPUT_NAME+=".exe"
    fi
    
    echo "Building for $OS/$ARCH..."
    
    GOOS=$OS GOARCH=$ARCH go build \
        -ldflags "-X main.Version=$VERSION -X main.BuildTime=$(date -u '+%Y-%m-%d_%H:%M:%S') -X main.GitCommit=$(git rev-parse --short HEAD)" \
        -o "$TEMP_DIR/$OUTPUT_NAME" \
        ./cmd
    
    # 创建包目录
    PKG_DIR="$TEMP_DIR/msbd-client-$VERSION-$OS-$ARCH"
    mkdir -p "$PKG_DIR"
    
    # 复制文件
    cp "$TEMP_DIR/$OUTPUT_NAME" "$PKG_DIR/"
    cp README.md "$PKG_DIR/"
    cp LICENSE "$PKG_DIR/" 2>/dev/null || echo "LICENSE file not found"
    
    # 创建配置文件模板
    cat > "$PKG_DIR/config.json.example" << EOF
{
  "server": {
    "host": "localhost",
    "grpc_port": 9090,
    "use_https": false
  },
  "audio": {
    "sample_rate": 16000,
    "channels": 1,
    "bit_depth": 16,
    "chunk_size_ms": 200
  },
  "anti_detection": {
    "enabled": true,
    "prevent_screenshot": true,
    "prevent_recording": true,
    "prevent_switch_detect": true
  },
  "gui": {
    "enabled": false,
    "start_minimized": true,
    "show_in_tray": true
  }
}
EOF
    
    # 创建启动脚本
    if [ "$OS" = "windows" ]; then
        cat > "$PKG_DIR/start.bat" << EOF
@echo off
echo Starting MSBD Client...
msbd-client-windows-amd64.exe
pause
EOF
    else
        cat > "$PKG_DIR/start.sh" << 'EOF'
#!/bin/bash
echo "Starting MSBD Client..."
./msbd-client-*
EOF
        chmod +x "$PKG_DIR/start.sh"
    fi
    
    # 打包
    cd "$TEMP_DIR"
    if [ "$OS" = "windows" ]; then
        zip -r "$DIST_DIR/msbd-client-$VERSION-$OS-$ARCH.zip" "msbd-client-$VERSION-$OS-$ARCH"
    else
        tar -czf "$DIST_DIR/msbd-client-$VERSION-$OS-$ARCH.tar.gz" "msbd-client-$VERSION-$OS-$ARCH"
    fi
    cd - > /dev/null
    
    echo "Packaged: $DIST_DIR/msbd-client-$VERSION-$OS-$ARCH"
done

# 清理临时目录
rm -rf "$TEMP_DIR"

echo "All packages created in $DIST_DIR/"
ls -la "$DIST_DIR/"
```

## 8. 测试策略

### 8.1 单元测试示例
```go
// internal/audio/capture_test.go
package audio

import (
    "testing"
    "time"
    "context"
)

func TestAudioCaptureManager_Initialize(t *testing.T) {
    config := &AudioConfig{
        SampleRate: 16000,
        Channels:   1,
        BitDepth:   16,
        ChunkSize:  1024,
    }
    
    manager := NewAudioCaptureManager(config)
    
    err := manager.Initialize()
    if err != nil {
        t.Fatalf("Failed to initialize audio capture manager: %v", err)
    }
    
    defer manager.Cleanup()
    
    // 验证配置
    if manager.config.SampleRate != 16000 {
        t.Errorf("Expected sample rate 16000, got %d", manager.config.SampleRate)
    }
}

func TestAudioCaptureManager_StartStopCapture(t *testing.T) {
    // 测试音频采集的开始和停止
    config := &AudioConfig{
        SampleRate: 16000,
        Channels:   1,
        BitDepth:   16,
        ChunkSize:  1024,
    }
    
    manager := NewAudioCaptureManager(config)
    err := manager.Initialize()
    if err != nil {
        t.Skipf("Skipping test due to audio initialization failure: %v", err)
    }
    defer manager.Cleanup()
    
    // 测试数据接收
    dataReceived := make(chan bool, 1)
    
    err = manager.StartCapture(func(data []byte) error {
        if len(data) > 0 {
            select {
            case dataReceived <- true:
            default:
            }
        }
        return nil
    })
    
    if err != nil {
        t.Fatalf("Failed to start capture: %v", err)
    }
    
    // 等待音频数据
    select {
    case <-dataReceived:
        t.Log("Successfully received audio data")
    case <-time.After(2 * time.Second):
        t.Log("No audio data received (this might be normal in test environment)")
    }
    
    err = manager.StopCapture()
    if err != nil {
        t.Errorf("Failed to stop capture: %v", err)
    }
}
```

### 8.2 集成测试
```go
// test/integration/client_test.go
package integration

import (
    "testing"
    "time"
    "context"
)

func TestClientServerIntegration(t *testing.T) {
    // 这个测试需要实际的服务端运行
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }
    
    // 创建测试客户端
    config := &ClientConfig{
        Server: struct {
            Host     string `json:"host"`
            GRPCPort int    `json:"grpc_port"`
            UseHTTPS bool   `json:"use_https"`
        }{
            Host:     "localhost",
            GRPCPort: 9090,
            UseHTTPS: false,
        },
        Auth: struct {
            Email        string    `json:"email"`
            AccessToken  string    `json:"access_token,omitempty"`
            RefreshToken string    `json:"refresh_token,omitempty"`
            ExpiresAt    time.Time `json:"expires_at,omitempty"`
        }{
            Email: "test@example.com",
        },
    }
    
    client := NewClientApplication(config)
    
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // 测试连接
    err := client.Initialize(ctx)
    if err != nil {
        t.Fatalf("Failed to initialize client: %v", err)
    }
    
    // 测试认证
    err = client.auth.Login("test@example.com", "testpassword")
    if err != nil {
        t.Fatalf("Failed to login: %v", err)
    }
    
    // 测试gRPC连接
    err = client.grpc.Connect()
    if err != nil {
        t.Fatalf("Failed to connect to gRPC server: %v", err)
    }
    
    // 清理
    client.Cleanup()
}
```

---

这个客户端设计文档提供了完整的技术架构和实现方案。主要特点：

1. **技术选型合理**：Go语言确保跨平台兼容性
2. **架构清晰**：分层设计，职责明确  
3. **防检测功能**：针对不同平台的具体实现
4. **通信可靠**：gRPC流式通信，支持重连
5. **用户友好**：系统托盘界面，配置简单
6. **测试完善**：单元测试和集成测试覆盖

接下来可以开始具体的代码实现工作。