# SM3Hash

单文件SM3哈希计算工具， 基于 Go/WinAPI 实现，仿MyHash 1.4.7界面设计，无需三方依赖。

## 功能

- 拖放或浏览文件或目录（支持批量队列），逐个计算 SM3。
- 可选输出：文件大小、耗时、结果大写。
- 结果区域支持复制/保存，进度条实时更新。
- 窗口可调整大小，布局自适应。
- 仅依赖标准库 + WinAPI，不需额外 DLL。

## 构建

在 Windows 下：

```powershell
go mod tidy
go fmt ./...
go build -ldflags "-H=windowsgui" -o SM3Hash.exe sm3hash.go
```

如需嵌入图标/版本/manifest，可在已有的 `SM3Hash.exe` 基础上运行（需准备 icon.png 和 tools/embedres）：

```powershell
$env:TEMP="$PWD\tmp"; $env:TMP="$PWD\tmp"
tools\embedres.exe
```

## 说明

- SM3 实现遵循 GM/T 0004-2012。

  
  


