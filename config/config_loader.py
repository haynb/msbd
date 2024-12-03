import yaml
import os
from pathlib import Path

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
        config_path = Path(__file__).parent / 'settings.yaml'
        with open(config_path, 'r', encoding='utf-8') as f:
            self._config = yaml.safe_load(f)
    
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