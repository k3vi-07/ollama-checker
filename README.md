# Ollama 服务健康检测工具

![Go Version](https://img.shields.io/badge/go-1.21+-blue)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

专为Ollama服务设计的分布式健康检测工具，支持智能并发控制和详细诊断报告。

## 核心特性

### 🚀 检测能力
- 多节点并行检测（3-20个动态worker）
- 三级健康验证机制：
  ```mermaid
  graph TD
    A[HTTP状态码] --> B[JSON格式验证]
    B --> C[模型列表提取]
  ```
- 自动重试机制（3次指数退避重试）

### 📊 结果输出
- 实时终端仪表盘
  ```
  ✓ http://node1:11434 (3 models)
  ✗ http://node2:11434 (连接超时)
  已处理: 15/20 | 成功率: 75%
  ```
- CSV报告自动生成
  ```csv
  URL,Model
  http://node1:11434,llama2; codellama
  http://node3:11434,mistral
  ```

### 📁 日志系统
- 双通道日志记录
  - `[MAIN]` 主日志（仅文件）
  - `[NET]` 网络日志（文件+终端）
- 自动日志轮转（按启动时间命名）

## 快速开始

### 安装方式
```bash
# 源码安装
go install github.com/k3vi-07/ollama-checker@latest

# 二进制安装
https://github.com/k3vi-07/ollama-checker
```

### 使用示例
```bash
# 从文件读取节点列表
./ollama-checker -file nodes.txt

# 直接检测指定节点
./ollama-checker http://node1:11434 http://node2:11434

# 混合模式（文件+命令行参数）
./ollama-checker -file prod-nodes.txt http://backup:11434
```

### 输入文件格式
```text
# nodes.txt
http://primary:11434    # 生产主节点
http://secondary:11434  # 生产备援
http://test:11434       # 测试环境
```

## 高级功能

### 结果导出
- 自动生成时间戳命名的CSV文件
- 模型列表分号分隔
- 支持空模型检测（标记为"无模型"）

### 诊断日志
```log
[MAIN] 启动检测器 v2.2 | 节点数: 20
[NET] 请求 → http://node1:11434/api/tags (重试 2/3)
[NET] 非200响应: http://node2:11434 (404)
```

## 技术规格

| 类别         | 参数配置                  |
|--------------|-------------------------|
| 并发策略     | 动态计算 (N/2, 3-20)     |
| 单请求超时   | 5秒                     |
| 总运行时间   | 无限制（直到任务完成）   |
| 重试机制     | 3次 (500ms, 1s, 1.5s)  |
| 内存保护     | 自动回收响应体 (≤1MB)   |

## 贡献指南

1. 提交Issue前请先检查最新日志
2. 代码需通过 `go test -race` 测试
3. 文档更新要求：
   - 同步修改示例代码
   - 更新版本号标记
   - 维护变更日志

## 授权许可
MIT License © 2025 k3vi-07
