import os
import time
import json
from aliyunsdkcore.client import AcsClient
from aliyunsdkcore.request import CommonRequest

# 创建AcsClient实例
client = AcsClient(
   os.getenv('ALIYUN_AK_ID'),
   os.getenv('ALIYUN_AK_SECRET'),
   os.getenv('ALIYUN_REGION_ID')
);

# 创建request，并设置参数。
request = CommonRequest()
request.set_method('POST')
request.set_domain('nls-meta.cn-shanghai.aliyuncs.com')
request.set_version('2019-02-28')
request.set_action_name('CreateToken')

def get_token():
    # 检查是否已有token及其过期时间
    token_info = load_token_info()
    if token_info and not is_token_expiring(token_info['expireTime']):
        return token_info['token']
    
    # 创建新的token
    try:
        response = client.do_action_with_exception(request)
        jss = json.loads(response)
        if 'Token' in jss and 'Id' in jss['Token']:
            token = jss['Token']['Id']
            expireTime = jss['Token']['ExpireTime']
            save_token_info(token, expireTime)
            return token
    except Exception as e:
        print(e)
        return None

def is_token_expiring(expire_time):
    # 检查token是否快过期
    current_time = int(time.time())
    return (expire_time - current_time) < 300  # 5分钟内过期

def load_token_info():
    # 从文件或数据库加载token信息
    # 示例：从文件加载
    try:
        with open('token_info.json', 'r') as f:
            return json.load(f)
    except FileNotFoundError:
        return None

def save_token_info(token, expire_time):
    # 保存token信息到文件或数据库
    # 示例：保存到文件
    with open('token_info.json', 'w') as f:
        json.dump({'token': token, 'expireTime': expire_time}, f)