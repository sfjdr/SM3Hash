# SM3Hash

SM3 hash desktop工具，纯 Go + WinAPI 实现，单文件源码，无第三方依赖。

## 功能
- 拖放文件或批量加入队列，计算 SM3 摘要，可选显示文件大小/时间戳。
- 可保存结果、复制到剪贴板，带进度条与状态提示。
- 界面可调整尺寸，使用原生控件，支持大文件流式处理。
- 单个 `sm3hash.go` 源文件，可直接构建出 Windows 可执行程序。

## 构建
```powershell
# 在项目根目录
go mod tidy    # 标准库依赖，快速完成
go fmt ./...
go build -o SM3Hash.exe sm3hash.go
```

## 使用
- 直接运行 `SM3Hash.exe`，拖拽文件或点击“浏览”添加文件。
- 勾选“Upper”可输出大写 SM3；“Size”“Time”控制附加信息。
- 结果可复制或保存到文本文件。

## 发布
- 构建最新 `SM3Hash.exe` 后推送标签：`git tag v1.0 && git push --tags`。
- GitHub Release 可附带 `SM3Hash.exe` 作为二进制资产发布。
