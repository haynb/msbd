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

## 2、获取openai或者deepseek的apiKey

这里自行寻找api

deepseek：https://www.deepseek.com/

## 3、运行程序

这里会自动创建配置文件，关闭程序后使用上述参数填充补全配置文件即可。