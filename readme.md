# 八股文面试宝典

准备跳槽的时候背八股文背的人都麻了，于是就想着做一个工具，一劳永逸……

- 默认使用阿里云语音识别模块，自动将面试官的话转为文字，自动分句；
- 支持上下文；
- 自动判断转义的句子是不是面试官的专业问题；
- 自动发送给ai，并获取回答；
- 回答分为简略回答与详细解释；

## 1、获取智能语音交互api

阿里云： https://ai.aliyun.com/nls

相关文档：https://help.aliyun.com/zh/isi/getting-started/start-here?spm=5176.12157770.J_5524031460.6.46a86141MDPf2D

本项目需要的阿里云相关参数有：

1. access_key_id
2. access_key_secret
3. app_key
4. region_id

相关文档中都有，这里不再赘述。

<span style="color:red;">新用户可以免费试用！！！</span>

## 2、获取openai或者deepseek的apiKey

这里自行寻找api

deepseek：https://www.deepseek.com/

## 3、运行程序

这里会自动创建配置文件，关闭程序后使用上述参数填充补全配置文件即可(在config/config.json中)。





## 重大更新！！！！！

新增gui界面：

![0](https://github.com/haynb/msbd/blob/d20c81f8a697b966549af22a21c205b9e19a6226/imgs/0.jpg)

新增防屏幕分享、防录屏、防截图、防切屏检测功能！！！！

妈妈再也不用担心我作弊被发现啦！！

新增截图解题功能！！！力扣直接秒，ai帮你答：
![1](https://github.com/haynb/msbd/blob/d20c81f8a697b966549af22a21c205b9e19a6226/imgs/1.jpg)
