# go_wget - Go语言实现的下载工具

一个使用Go语言编写的命令行下载工具，支持并发下载、断点续传等功能。

## 特性

- 支持HTTP/HTTPS协议下载
- 自动检测并支持断点续传
- 智能分块并发下载，提高下载速度
- 实时显示下载进度和速度
- 支持自定义请求头
- 支持指定输出文件名
- 安全的TLS配置

## 安装

确保已安装Go语言环境（要求Go 1.11+），然后执行：

```bash
go install github.com/Lyrichu/go_wget@latest
```

或者从源码编译：

```bash
git clone https://github.com/Lyrichu/go_wget.git
cd go_wget
go build -o go_wget
```

## 使用方法

基本用法：

```bash
go_wget [选项] <URL>
```

### 可用选项

- `-o, --output <filename>`: 指定输出文件名
- `-H, --headers "Key1:Value1,Key2:Value2"`: 设置自定义HTTP请求头
- `-v, --verbose`: 显示详细的下载进度

### 使用示例

1. 简单下载：
```bash
go_wget https://example.com/file.zip
```

2. 指定输出文件名：
```bash
go_wget -o myfile.zip https://example.com/file.zip
```

3. 添加自定义请求头：
```bash
go_wget -H "User-Agent:Custom-UA,Accept:*/*" https://example.com/file.zip
```

4. 显示详细进度：
```bash
go_wget -v https://example.com/file.zip
```

## 注意事项

- 下载过程中如果遇到中断，程序会自动清理临时文件
- 对于不支持断点续传的服务器，会自动切换到普通下载模式
- 建议在下载大文件时使用 `-v` 选项查看下载进度
- 确保目标路径有足够的磁盘空间

## 贡献

欢迎提交Issue和Pull Request！

## 许可证

MIT License