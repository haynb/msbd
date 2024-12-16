import yaml
import os
from pathlib import Path
import sys

class ConfigLoader:
    _instance = None
    _config = None
    
    def __new__(cls):
        if cls._instance is None:
            cls._instance = super().__new__(cls)
        return cls._instance
    
    def __init__(self):
        if self._config is None:
            self._load_config()
            self._set_environment_variables()
    
    def _load_config(self):
        """加载YAML配置文件"""
        possible_paths = [
            Path(sys.executable).parent / 'settings.yaml',  # exe 目录下的 settings.yaml
            Path(sys.executable).parent / 'config' / 'settings.yaml',  # exe 目录下的 config 目录中的 settings.yaml
            Path.cwd() / 'settings.yaml',                    # 当前工作目录下的 settings.yaml
            Path.home() / '.myapp' / 'settings.yaml',       # 用户主目录下的 .myapp 文件夹中的 settings.yaml
            Path(__file__).parent / 'settings.yaml',         # 当前脚本所在目录下的 settings.yaml
            Path(__file__).parent / 'config' / 'settings.yaml',  # 当前脚本下的 config 目录中的 settings.yaml
    ]

        config_file = None
        for path in possible_paths:
            if path.exists():
                config_file = path
                print(f"已找到配置文件：{path}")
                break
        
        if config_file is None:
            # 如果找不到配置文件，在exe目录下创建默认配置
            default_path = Path(sys.executable).parent / 'settings.yaml'
            print(f"未找到配置文件，将在以下位置创建默认配置：{default_path}")
            print("请自行补充配置文件的内容然后重启本程序")
            self.create_default_config(default_path)
            # 暂停本软件
            input("按任意键继续...")
            exit()

        try:
            with open(config_file, 'r', encoding='utf-8') as f:
                self._config = yaml.safe_load(f)
                print(f"成功加载配置文件")
        except Exception as e:
            print(f"加载配置文件失败: {e}")
            self._config = self._get_default_config()

    def _get_default_config(self):
        """返回默认配置"""
        return {
            'aliyun': {
                'access_key_id': '',
                'access_key_secret': '',
                'region_id': 'cn-shanghai',
                'app_key': ''
            },
            'openai': {
                'api_key': '',
                'base_url': '',
                'model': 'gpt-4o-mini',
                'temperature': 0.7,
                'max_tokens': 2000,
                'timeout': 60
            }
        }
    
    def _set_environment_variables(self):
        """设置环境变量"""
        aliyun_config = self._config.get('aliyun', {})
        # 设置阿里云相关环境变量
        os.environ['ALIYUN_AK_ID'] = aliyun_config.get('access_key_id', '')
        os.environ['ALIYUN_AK_SECRET'] = aliyun_config.get('access_key_secret', '')
        os.environ['ALIYUN_REGION_ID'] = aliyun_config.get('region_id', 'cn-shanghai')
        os.environ['ALIYUN_APP_KEY'] = aliyun_config.get('app_key', '')
        
        # 设置OpenAI相关环境变量
        openai_config = self._config.get('openai', {})
        os.environ['OPENAI_API_KEY'] = openai_config.get('api_key', '')
        os.environ['OPENAI_BASE_URL'] = openai_config.get('base_url', '')
        os.environ['OPENAI_MODEL'] = openai_config.get('model', 'gpt-4o-mini')

        # 设置DeepSeek相关环境变量
        deepseek_config = self._config.get('deepseek', {})
        os.environ['DEEPSEEK_API_KEY'] = deepseek_config.get('api_key', '')
        os.environ['DEEPSEEK_BASE_URL'] = deepseek_config.get('base_url', '')
        os.environ['DEEPSEEK_MODEL'] = deepseek_config.get('model', 'deepseek-chat')
        
    @property
    def aliyun_config(self):
        """获取环境变量中的配置"""
        return {
            'access_key_id': os.getenv('ALIYUN_AK_ID'),
            'access_key_secret': os.getenv('ALIYUN_AK_SECRET'),
            'region_id': os.getenv('ALIYUN_REGION_ID'),
            'app_key': os.getenv('ALIYUN_APP_KEY')
        }
    
    @property
    def openai_config(self):
        """获取OpenAI配置"""
        return {
            'api_key': os.getenv('OPENAI_API_KEY'),
            'base_url': os.getenv('OPENAI_BASE_URL'),
            'model': self._config.get('openai', {}).get('model', 'gpt-4o-mini'),
            'temperature': self._config.get('openai', {}).get('temperature', 0.7),
            'max_tokens': self._config.get('openai', {}).get('max_tokens', 2000),
            'timeout': self._config.get('openai', {}).get('timeout', 60)
        }
    
    def create_default_config(self, path):
        """创建默认配置文件"""
        config_dir = path.parent
        if not config_dir.exists():
            config_dir.mkdir(parents=True)
        
        with open(path, 'w', encoding='utf-8') as f:
            yaml.dump(self._get_default_config(), f, allow_unicode=True)