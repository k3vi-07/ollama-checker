# Ollama 服务检测工具

![Go Version](https://img.shields.io/badge/go-1.21+-blue)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

用于批量检测 Ollama 服务可用性的命令行工具，支持高并发检测和详细日志记录。

## 主要功能

- ✅ 并发检测多个 Ollama 节点
- ✅ 支持文件输入和命令行参数双模式
- ✅ 实时进度显示（已处理数/失败数）
- ✅ 详细的文件日志记录（含网络请求跟踪）
- ✅ 严格的服务验证逻辑：
  - HTTP 状态码检查
  - JSON 格式验证
  - 有效模型存在性检查
  - 3秒请求超时 + 30秒总超时

## 安装

```bash
# 从源码编译
go install github.com/k3vi-07/ollama-checker@latest

# 或下载预编译二进制
https://github.com/k3vi-07/ollama-checker
chmod +x ollama-checker
```

## 使用说明

### 基本使用
```bash
# 从文件读取检测地址
./ollama-checker -file urls.txt

# 直接指定检测地址
./ollama-checker http://localhost:11434 http://backup:11434

# 混合模式（文件优先）
./ollama-checker -file nodes.txt http://fallback:11434
```

### 文件格式示例
```text
# urls.txt
http://primary:11434    # 生产环境主节点
http://secondary:11434  # 生产环境备节点
http://test:11434       # 测试环境节点
```

## 输出示例
```shell
✓ http://good-node:11434
已处理: 15/20 | 失败: 3
检测完成
```

## 日志系统
日志文件自动保存在 `logs/` 目录，命名格式为 `YYYYMMDD-HHMMSS.log`

```bash
# 查看实时日志
tail -f logs/20240520-153045.log

# 典型日志内容
[NET] 请求 → http://node:11434/api/tags
[ERROR] 网络错误: http://bad-node:11434 (context deadline exceeded)
[SUCCESS] 有效节点: http://good-node:11434 (模型数: 3)
```

## 技术参数
- 默认并发数：10 worker
- 单请求超时：3秒
- 总运行超时：30秒
- 最大响应体记录：1KB

## 贡献
欢迎提交 Issue 或 PR，请先阅读：
1. 使用 `gofmt` 格式化代码
2. 添加必要的测试用例
3. 更新相关文档

## 许可证
MIT License © 2024 k3vi-07
