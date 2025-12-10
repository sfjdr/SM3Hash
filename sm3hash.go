package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Single-file Go + WinAPI SM3 tool. Resizable UI, drag&drop, queue, no external deps.

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	procPostQuitMessage      = user32.NewProc("PostQuitMessage")
	procRegisterClassExW     = user32.NewProc("RegisterClassExW")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procDefWindowProcW       = user32.NewProc("DefWindowProcW")
	procShowWindow           = user32.NewProc("ShowWindow")
	procUpdateWindow         = user32.NewProc("UpdateWindow")
	procGetMessageW          = user32.NewProc("GetMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessageW     = user32.NewProc("DispatchMessageW")
	procSendMessageW         = user32.NewProc("SendMessageW")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procSetWindowTextW       = user32.NewProc("SetWindowTextW")
	procMessageBoxW          = user32.NewProc("MessageBoxW")
	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	procGetOpenFileNameW     = comdlg32.NewProc("GetOpenFileNameW")
	procGetSaveFileNameW     = comdlg32.NewProc("GetSaveFileNameW")
	procGetStockObject       = gdi32.NewProc("GetStockObject")
	procGetClientRect        = user32.NewProc("GetClientRect")
	procMoveWindow           = user32.NewProc("MoveWindow")
	procOpenClipboard        = user32.NewProc("OpenClipboard")
	procCloseClipboard       = user32.NewProc("CloseClipboard")
	procEmptyClipboard       = user32.NewProc("EmptyClipboard")
	procSetClipboardData     = user32.NewProc("SetClipboardData")
	procGlobalAlloc          = kernel32.NewProc("GlobalAlloc")
	procGlobalLock           = kernel32.NewProc("GlobalLock")
	procGlobalUnlock         = kernel32.NewProc("GlobalUnlock")
	procEnableWindow         = user32.NewProc("EnableWindow")
	procDragAcceptFiles      = shell32.NewProc("DragAcceptFiles")
	procDragQueryFileW       = shell32.NewProc("DragQueryFileW")
	procDragFinish           = shell32.NewProc("DragFinish")
)

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_BORDER           = 0x00800000
	WS_VSCROLL          = 0x00200000
	WS_TABSTOP          = 0x00010000
	WS_GROUP            = 0x00020000
	WS_EX_CLIENTEDGE    = 0x00000200
	WS_EX_ACCEPTFILES   = 0x00000010

	ES_MULTILINE   = 0x0004
	ES_AUTOVSCROLL = 0x0040
	ES_READONLY    = 0x0800

	BS_GROUPBOX     = 0x00000007
	BS_AUTOCHECKBOX = 0x00000003
	BS_PUSHBUTTON   = 0x00000000

	PBM_SETRANGE = 0x0401
	PBM_SETPOS   = 0x0402

	BM_GETCHECK = 0x00F0
	BM_SETCHECK = 0x00F1

	BST_UNCHECKED = 0
	BST_CHECKED   = 1

	WM_DESTROY   = 0x0002
	WM_COMMAND   = 0x0111
	WM_CREATE    = 0x0001
	WM_APP       = 0x8000
	WM_DROPFILES = 0x0233
	WM_SETFONT   = 0x0030
	WM_SIZE      = 0x0005

	MSG_PROGRESS = WM_APP + 1
	MSG_DONE     = WM_APP + 2
	MSG_ERROR    = WM_APP + 3
	MSG_REFRESH  = WM_APP + 4

	IDC_ARROW = 32512
	SW_SHOW   = 5

	CF_UNICODETEXT = 13
	GMEM_MOVEABLE  = 0x0002

	ICC_PROGRESS_CLASS = 0x00000020

	margin         int32 = 10
	groupHeight    int32 = 45
	progressHeight int32 = 20
	btnWidth       int32 = 70
	btnHeight      int32 = 24
)

const (
	idEditOut   = 1001
	idChkSize   = 1002
	idChkTime   = 1003
	idChkUpper  = 1004
	idBtnBrowse = 1005
	idBtnClear  = 1006
	idBtnCopy   = 1007
	idBtnSave   = 1008
	idBtnStart  = 1009
	idBtnExit   = 1010
	idProgBar   = 1011
	idProgLabel = 1012
)

type hwnd = syscall.Handle

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type msg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type rect struct{ left, top, right, bottom int32 }

type initCommonControlsEx struct {
	dwSize uint32
	dwICC  uint32
}

type openFileNameW struct {
	lStructSize       uint32
	hwndOwner         syscall.Handle
	hInstance         syscall.Handle
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        unsafe.Pointer
	dwReserved        uint32
	flagsEx           uint32
}

var (
	mainHWND         hwnd
	outputHWND       hwnd
	settingsHWND     hwnd
	algoHWND         hwnd
	sm3LabelHWND     hwnd
	progressTextHWND hwnd
	progressHWND     hwnd
	progressLblHWND  hwnd
	chkSizeHWND      hwnd
	chkTimeHWND      hwnd
	chkUpperHWND     hwnd
	btnBrowseHWND    hwnd
	btnClearHWND     hwnd
	btnCopyHWND      hwnd
	btnSaveHWND      hwnd
	btnStartHWND     hwnd
	btnExitHWND      hwnd

	outputMu   sync.Mutex
	outputText string
	errMu      sync.Mutex
	errText    string

	queueMu       sync.Mutex
	queue         []string
	workerRunning bool
)

func main() {
	os.Setenv("GOTELEMETRY", "off")
	initCommonControls()
	hInstance := getModuleHandle()
	className := toUTF16Ptr("SM3HashWin")
	wcx := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInstance,
		hCursor:       loadCursor(IDC_ARROW),
		hbrBackground: syscall.Handle(6),
		lpszClassName: className,
	}
	if atom, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcx))); atom == 0 {
		panic(err)
	}
	title := toUTF16Ptr("SM3校验工具 (Go)")
	hw, _, err := procCreateWindowExW.Call(
		WS_EX_ACCEPTFILES,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		WS_OVERLAPPEDWINDOW|WS_VISIBLE,
		100, 100, 560, 440,
		0, 0, uintptr(hInstance), 0,
	)
	if hw == 0 {
		panic(err)
	}
	mainHWND = hwnd(hw)
	procShowWindow.Call(hw, SW_SHOW)
	procUpdateWindow.Call(hw)

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) == 0 || int32(ret) == -1 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func wndProc(h hwnd, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case WM_CREATE:
		createControls(h)
	case WM_COMMAND:
		handleCommand(wParam)
	case MSG_PROGRESS:
		setProgress(int(wParam))
	case MSG_DONE:
		setProgress(100)
		updateButtons(true)
		refreshOutput()
	case MSG_ERROR:
		updateButtons(true)
		showError(currentError())
	case MSG_REFRESH:
		refreshOutput()
	case WM_DROPFILES:
		handleDrop(wParam)
	case WM_SIZE:
		layoutControls()
	case WM_DESTROY:
		procPostQuitMessage.Call(0)
	default:
		ret, _, _ := procDefWindowProcW.Call(uintptr(h), uintptr(message), wParam, lParam)
		return ret
	}
	return 0
}

func createControls(h hwnd) {
	font := getDefaultFont()
	outputHWND = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_READONLY, WS_EX_CLIENTEDGE, 10, 10, 500, 180, h, idEditOut)
	setFont(outputHWND, font)

	settingsHWND = createWindow("BUTTON", "设置", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 0, 10, 195, 500, groupHeight, h, 0)
	chkSizeHWND = createWindow("BUTTON", "文件大小", WS_CHILD|WS_VISIBLE|BS_AUTOCHECKBOX, 0, 20, 213, 80, 18, h, idChkSize)
	chkTimeHWND = createWindow("BUTTON", "计算时间", WS_CHILD|WS_VISIBLE|BS_AUTOCHECKBOX, 0, 110, 213, 80, 18, h, idChkTime)
	chkUpperHWND = createWindow("BUTTON", "结果大写", WS_CHILD|WS_VISIBLE|BS_AUTOCHECKBOX, 0, 200, 213, 80, 18, h, idChkUpper)
	sendMessage(chkSizeHWND, BM_SETCHECK, BST_CHECKED, 0)
	sendMessage(chkTimeHWND, BM_SETCHECK, BST_CHECKED, 0)
	sendMessage(chkUpperHWND, BM_SETCHECK, BST_CHECKED, 0)
	setFont(chkSizeHWND, font)
	setFont(chkTimeHWND, font)
	setFont(chkUpperHWND, font)

	algoHWND = createWindow("BUTTON", "算法", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 0, 10, 245, 500, groupHeight, h, 0)
	sm3LabelHWND = createWindow("STATIC", "SM3", WS_CHILD|WS_VISIBLE, 0, 20, 265, 80, 18, h, 0)
	setFont(sm3LabelHWND, font)

	progressTextHWND = createWindow("STATIC", "进度:", WS_CHILD|WS_VISIBLE, 0, 10, 295, 40, 18, h, idProgLabel)
	setFont(progressTextHWND, font)
	progressHWND = createWindow("msctls_progress32", "", WS_CHILD|WS_VISIBLE, WS_EX_CLIENTEDGE, 60, 295, 320, 18, h, idProgBar)
	sendMessage(progressHWND, PBM_SETRANGE, 0, uintptr((100<<16)|0))
	progressLblHWND = createWindow("STATIC", "0%", WS_CHILD|WS_VISIBLE, 0, 390, 295, 40, 18, h, 0)
	setFont(progressLblHWND, font)

	btnBrowseHWND = createButton("浏览...", 10, 330, h, idBtnBrowse, font)
	btnClearHWND = createButton("清除", 90, 330, h, idBtnClear, font)
	btnCopyHWND = createButton("复制", 170, 330, h, idBtnCopy, font)
	btnSaveHWND = createButton("保存", 250, 330, h, idBtnSave, font)
	btnStartHWND = createButton("开始", 330, 330, h, idBtnStart, font)
	btnExitHWND = createButton("退出", 410, 330, h, idBtnExit, font)

	procDragAcceptFiles.Call(uintptr(h), 1)
	layoutControls()
}

func createButton(text string, x, y int32, parent hwnd, id int32, font syscall.Handle) hwnd {
	h := createWindow("BUTTON", text, WS_CHILD|WS_VISIBLE|BS_PUSHBUTTON|WS_TABSTOP, 0, x, y, btnWidth, btnHeight, parent, id)
	setFont(h, font)
	return h
}

func createWindow(class, title string, style uint32, exStyle int32, x, y, w, h int32, parent hwnd, id int32) hwnd {
	ret, _, _ := procCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(toUTF16Ptr(class))),
		uintptr(unsafe.Pointer(toUTF16Ptr(title))),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(parent), uintptr(id), uintptr(getModuleHandle()), 0,
	)
	return hwnd(ret)
}

func layoutControls() {
	if mainHWND == 0 {
		return
	}
	var rc rect
	procGetClientRect.Call(uintptr(mainHWND), uintptr(unsafe.Pointer(&rc)))
	w := rc.right - rc.left
	h := rc.bottom - rc.top
	if w <= 0 || h <= 0 {
		return
	}
	y := margin
	outH := h - (groupHeight*2 + progressHeight + btnHeight + margin*6)
	if outH < 140 {
		outH = 140
	}
	cw := maxInt32(w-2*margin, 120)
	moveWindow(outputHWND, margin, y, cw, outH)
	y += outH + margin

	moveWindow(settingsHWND, margin, y, cw, groupHeight)
	moveWindow(chkSizeHWND, margin+10, y+18, 80, 20)
	moveWindow(chkTimeHWND, margin+110, y+18, 80, 20)
	moveWindow(chkUpperHWND, margin+210, y+18, 80, 20)
	y += groupHeight + margin

	moveWindow(algoHWND, margin, y, cw, groupHeight)
	moveWindow(sm3LabelHWND, margin+10, y+18, 80, 20)
	y += groupHeight + margin

	px := margin + 50
	pw := w - px - margin - 60
	if pw < 50 {
		pw = 50
	}
	moveWindow(progressTextHWND, margin, y, 40, progressHeight)
	moveWindow(progressHWND, px, y, pw, progressHeight)
	moveWindow(progressLblHWND, px+pw+margin, y, 50, progressHeight)
	y += progressHeight + margin

	btnY := h - margin - btnHeight
	spacing := int32(10)
	if total := 6*btnWidth + 5*spacing; total+margin*2 > w {
		spacing = maxInt32(4, (w-2*margin-6*btnWidth)/5)
	}
	x := margin
	moveWindow(btnBrowseHWND, x, btnY, btnWidth, btnHeight)
	x += btnWidth + spacing
	moveWindow(btnClearHWND, x, btnY, btnWidth, btnHeight)
	x += btnWidth + spacing
	moveWindow(btnCopyHWND, x, btnY, btnWidth, btnHeight)
	x += btnWidth + spacing
	moveWindow(btnSaveHWND, x, btnY, btnWidth, btnHeight)
	x += btnWidth + spacing
	moveWindow(btnStartHWND, x, btnY, btnWidth, btnHeight)
	x += btnWidth + spacing
	moveWindow(btnExitHWND, x, btnY, btnWidth, btnHeight)
}

func moveWindow(h hwnd, x, y, w, ht int32) {
	if h != 0 {
		procMoveWindow.Call(uintptr(h), uintptr(x), uintptr(y), uintptr(w), uintptr(ht), 1)
	}
}

func handleCommand(wParam uintptr) {
	id := int32(wParam & 0xFFFF)
	switch id {
	case idBtnBrowse:
		onBrowse()
	case idBtnClear:
		onClear()
	case idBtnCopy:
		onCopy()
	case idBtnSave:
		onSave()
	case idBtnStart:
		startWorker()
	case idBtnExit:
		procPostQuitMessage.Call(0)
	}
}

func onBrowse() {
	path, ok := openFileDialog("选择要校验的文件")
	if !ok {
		return
	}
	enqueueExpanded([]string{path})
}

func onClear() {
	outputMu.Lock()
	outputText = ""
	outputMu.Unlock()
	requestRefresh()
}

func onCopy() {
	outputMu.Lock()
	text := outputText
	outputMu.Unlock()
	if text == "" {
		return
	}
	setClipboardText(text)
}

func onSave() {
	path, ok := saveFileDialog("保存校验结果", "sm3_result.txt")
	if !ok {
		return
	}
	outputMu.Lock()
	text := outputText
	outputMu.Unlock()
	_ = os.WriteFile(path, []byte(text), 0644)
}

func enqueueExpanded(paths []string) {
	files := expandPaths(paths)
	if len(files) == 0 {
		return
	}
	queueMu.Lock()
	queue = append(queue, files...)
	running := workerRunning
	queueMu.Unlock()
	appendOutput(fmt.Sprintf("加入任务: %d 个文件", len(files)))
	if !running {
		startWorker()
	}
}

func expandPaths(paths []string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, p := range paths {
		if p == "" {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			continue
		}
		if info.IsDir() {
			filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if _, ok := seen[path]; ok {
					return nil
				}
				seen[path] = struct{}{}
				out = append(out, path)
				return nil
			})
		} else {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}

func startWorker() {
	queueMu.Lock()
	if workerRunning || len(queue) == 0 {
		queueMu.Unlock()
		return
	}
	workerRunning = true
	queueMu.Unlock()
	updateButtons(false)
	go safeProcessQueue()
}

func safeProcessQueue() {
	defer func() {
		if r := recover(); r != nil {
			appendOutput(fmt.Sprintf("内部错误: %v", r))
		}
		queueMu.Lock()
		workerRunning = false
		queueMu.Unlock()
		updateButtons(true)
		procPostMessageW.Call(uintptr(mainHWND), MSG_DONE, 0, 0)
	}()
	for {
		queueMu.Lock()
		if len(queue) == 0 {
			queueMu.Unlock()
			return
		}
		path := queue[0]
		queue = queue[1:]
		queueMu.Unlock()
		processFile(path)
	}
}

func processFile(path string) {
	appendOutput(fmt.Sprintf("开始计算: %s", path))
	setProgress(0)
	showSize := isChecked(chkSizeHWND)
	showTime := isChecked(chkTimeHWND)
	upper := isChecked(chkUpperHWND)

	start := time.Now()
	res, err := computeSM3File(path, func(pct int) { procPostMessageW.Call(uintptr(mainHWND), MSG_PROGRESS, uintptr(pct), 0) })
	if err != nil {
		setError(err.Error())
		appendOutput(fmt.Sprintf("错误: %v", err))
		procPostMessageW.Call(uintptr(mainHWND), MSG_ERROR, 0, 0)
		return
	}
	if upper {
		res = strings.ToUpper(res)
	} else {
		res = strings.ToLower(res)
	}
	lines := []string{fmt.Sprintf("SM3: %s", res)}
	if showSize {
		if st, err := os.Stat(path); err == nil {
			lines = append(lines, fmt.Sprintf("文件大小: %d 字节", st.Size()))
		}
	}
	if showTime {
		lines = append(lines, fmt.Sprintf("耗时: %.2f s", time.Since(start).Seconds()))
	}
	lines = append(lines, "完成。")
	appendLines(lines)
	procPostMessageW.Call(uintptr(mainHWND), MSG_PROGRESS, uintptr(100), 0)
}

func setFont(h hwnd, font syscall.Handle) {
	procSendMessageW.Call(uintptr(h), WM_SETFONT, uintptr(font), 1)
}
func setEditText(text string) {
	procSetWindowTextW.Call(uintptr(outputHWND), uintptr(unsafe.Pointer(toUTF16Ptr(text))))
}

func appendOutput(line string) {
	outputMu.Lock()
	if outputText != "" {
		outputText += "\r\n"
	}
	outputText += line
	outputMu.Unlock()
	requestRefresh()
}

func appendLines(lines []string) {
	outputMu.Lock()
	for _, ln := range lines {
		if outputText != "" {
			outputText += "\r\n"
		}
		outputText += ln
	}
	outputMu.Unlock()
	requestRefresh()
}

func setProgress(pct int) {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	sendMessage(progressHWND, PBM_SETPOS, uintptr(pct), 0)
	setLabel(progressLblHWND, fmt.Sprintf("%d%%", pct))
}

func setLabel(h hwnd, text string) {
	procSetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(toUTF16Ptr(text))))
}

func updateButtons(enabled bool) {
	en := uintptr(0)
	if enabled {
		en = 1
	}
	procEnableWindow.Call(uintptr(btnStartHWND), en)
	procEnableWindow.Call(uintptr(btnBrowseHWND), en)
}

func isChecked(h hwnd) bool { return sendMessage(h, BM_GETCHECK, 0, 0) == BST_CHECKED }

func refreshOutput() { outputMu.Lock(); text := outputText; outputMu.Unlock(); setEditText(text) }
func requestRefresh() {
	if mainHWND != 0 {
		procPostMessageW.Call(uintptr(mainHWND), MSG_REFRESH, 0, 0)
	}
}

// 拖拽处理：收集全部路径（文件/文件夹），递归展开后入队。
func handleDrop(wParam uintptr) {
	hDrop := wParam
	defer procDragFinish.Call(hDrop)

	count, _, _ := procDragQueryFileW.Call(hDrop, 0xFFFFFFFF, 0, 0)
	if count == 0 {
		return
	}
	var paths []string
	for i := uint(0); i < uint(count); i++ {
		len1, _, _ := procDragQueryFileW.Call(hDrop, uintptr(i), 0, 0)
		if len1 == 0 {
			continue
		}
		buf := make([]uint16, len1+1)
		procDragQueryFileW.Call(hDrop, uintptr(i), uintptr(unsafe.Pointer(&buf[0])), len1+1)
		paths = append(paths, syscall.UTF16ToString(buf))
	}
	if len(paths) > 0 {
		enqueueExpanded(paths)
	}
}

func openFileDialog(title string) (string, bool) {
	buf := make([]uint16, 260)
	filter := toWinFilter("所有文件(*.*)\x00*.*\x00")
	ofn := openFileNameW{lStructSize: uint32(unsafe.Sizeof(openFileNameW{})), hwndOwner: mainHWND, lpstrFilter: filter, lpstrFile: &buf[0], nMaxFile: uint32(len(buf)), lpstrTitle: toUTF16Ptr(title), flags: 0x00080000 | 0x00001000}
	ret, _, _ := procGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ret == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

func saveFileDialog(title, defaultName string) (string, bool) {
	buf := make([]uint16, 260)
	copy(buf, utf16FromString(defaultName))
	filter := toWinFilter("文本文件 (*.txt)\x00*.txt\x00所有文件 (*.*)\x00*.*\x00")
	ofn := openFileNameW{lStructSize: uint32(unsafe.Sizeof(openFileNameW{})), hwndOwner: mainHWND, lpstrFilter: filter, lpstrFile: &buf[0], nMaxFile: uint32(len(buf)), lpstrTitle: toUTF16Ptr(title), flags: 0x00080000}
	ret, _, _ := procGetSaveFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if ret == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

func setClipboardText(text string) {
	procOpenClipboard.Call(0)
	defer procCloseClipboard.Call(0)
	procEmptyClipboard.Call(0)
	u16 := utf16FromString(text + "\x00")
	size := uintptr(len(u16) * 2)
	hMem, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, size)
	if hMem == 0 {
		return
	}
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size)
	src := unsafe.Slice((*byte)(unsafe.Pointer(&u16[0])), size)
	copy(dst, src)
	procGlobalUnlock.Call(hMem)
	procSetClipboardData.Call(CF_UNICODETEXT, hMem)
}

func showError(msg string) {
	procMessageBoxW.Call(uintptr(mainHWND), uintptr(unsafe.Pointer(toUTF16Ptr(msg))), uintptr(unsafe.Pointer(toUTF16Ptr("提示"))), 0x10)
}
func setError(msg string)  { errMu.Lock(); errText = msg; errMu.Unlock() }
func currentError() string { errMu.Lock(); defer errMu.Unlock(); return errText }

func getDefaultFont() syscall.Handle {
	h, _, _ := procGetStockObject.Call(17)
	return syscall.Handle(h)
}
func getModuleHandle() syscall.Handle {
	h, _, _ := procGetModuleHandleW.Call(0)
	return syscall.Handle(h)
}
func loadCursor(id int32) syscall.Handle {
	h, _, _ := user32.NewProc("LoadCursorW").Call(0, uintptr(id))
	return syscall.Handle(h)
}
func sendMessage(h hwnd, msg uint32, wParam, lParam uintptr) uintptr {
	ret, _, _ := procSendMessageW.Call(uintptr(h), uintptr(msg), wParam, lParam)
	return ret
}
func toUTF16Ptr(s string) *uint16       { u, _ := syscall.UTF16PtrFromString(s); return u }
func utf16FromString(s string) []uint16 { return append(syscall.StringToUTF16(s), 0) }
func toWinFilter(s string) *uint16      { return toUTF16Ptr(s + "\x00") }

func initCommonControls() {
	icc := initCommonControlsEx{dwSize: uint32(unsafe.Sizeof(initCommonControlsEx{})), dwICC: ICC_PROGRESS_CLASS}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
}

// ---- SM3 ----
var sm3IV = [8]uint32{0x7380166F, 0x4914B2B9, 0x172442D7, 0xDA8A0600, 0xA96F30BC, 0x163138AA, 0xE38DEE4D, 0xB0FB0E4E}

func computeSM3File(path string, progress func(int)) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, _ := f.Stat()
	length := info.Size()
	v := sm3IV
	var block [64]byte
	buf := make([]byte, 256*1024)
	bufLen := 0
	var total int64
	lastPct := -1
	lastSend := time.Now()
	if progress != nil {
		progress(0)
	}
	for {
		n, err := f.Read(buf)
		if n > 0 {
			total += int64(n)
			remain := n
			offset := 0
			for remain > 0 {
				toCopy := minInt(64-bufLen, remain)
				copy(block[bufLen:], buf[offset:offset+toCopy])
				bufLen += toCopy
				offset += toCopy
				remain -= toCopy
				if bufLen == 64 {
					sm3Compress(&v, block[:])
					bufLen = 0
				}
			}
			if length > 0 && progress != nil {
				pct := int((total * 100) / length)
				if pct > 100 {
					pct = 100
				}
				if pct != lastPct && (pct-lastPct >= 1 || time.Since(lastSend) > 200*time.Millisecond) {
					lastPct = pct
					lastSend = time.Now()
					progress(pct)
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	sm3PadAndProcess(&v, block[:], bufLen, total*8)
	if progress != nil {
		progress(100)
	}
	return sm3ToHex(v), nil
}

func sm3PadAndProcess(v *[8]uint32, block []byte, bufLen int, bitLen int64) {
	block[bufLen] = 0x80
	bufLen++
	if bufLen > 56 {
		for i := bufLen; i < 64; i++ {
			block[i] = 0
		}
		sm3Compress(v, block)
		bufLen = 0
	}
	for i := bufLen; i < 56; i++ {
		block[i] = 0
	}
	b := uint64(bitLen)
	block[56] = byte(b >> 56)
	block[57] = byte(b >> 48)
	block[58] = byte(b >> 40)
	block[59] = byte(b >> 32)
	block[60] = byte(b >> 24)
	block[61] = byte(b >> 16)
	block[62] = byte(b >> 8)
	block[63] = byte(b)
	sm3Compress(v, block)
}

func sm3Compress(v *[8]uint32, block []byte) {
	var w [68]uint32
	var w1 [64]uint32
	for i := 0; i < 16; i++ {
		idx := i * 4
		w[i] = uint32(block[idx])<<24 | uint32(block[idx+1])<<16 | uint32(block[idx+2])<<8 | uint32(block[idx+3])
	}
	for j := 16; j < 68; j++ {
		x := w[j-16] ^ w[j-9] ^ rotl(w[j-3], 15)
		w[j] = p1(x) ^ rotl(w[j-13], 7) ^ w[j-6]
	}
	for j := 0; j < 64; j++ {
		w1[j] = w[j] ^ w[j+4]
	}
	a, b, c, d := v[0], v[1], v[2], v[3]
	e, f, g, h := v[4], v[5], v[6], v[7]
	for j := 0; j < 64; j++ {
		ss1 := rotl((rotl(a, 12)+e+rotl(t(j), j))&0xFFFFFFFF, 7)
		ss2 := ss1 ^ rotl(a, 12)
		tt1 := (ff(j, a, b, c) + d + ss2 + w1[j]) & 0xFFFFFFFF
		tt2 := (gg(j, e, f, g) + h + ss1 + w[j]) & 0xFFFFFFFF
		d = c
		c = rotl(b, 9)
		b = a
		a = tt1
		h = g
		g = rotl(f, 19)
		f = e
		e = p0(tt2)
	}
	v[0] ^= a
	v[1] ^= b
	v[2] ^= c
	v[3] ^= d
	v[4] ^= e
	v[5] ^= f
	v[6] ^= g
	v[7] ^= h
}

func ff(j int, x, y, z uint32) uint32 {
	if j < 16 {
		return x ^ y ^ z
	}
	return (x & y) | (x & z) | (y & z)
}
func gg(j int, x, y, z uint32) uint32 {
	if j < 16 {
		return x ^ y ^ z
	}
	return (x & y) | (^x & z)
}
func p0(x uint32) uint32 { return x ^ rotl(x, 9) ^ rotl(x, 17) }
func p1(x uint32) uint32 { return x ^ rotl(x, 15) ^ rotl(x, 23) }
func t(j int) uint32 {
	if j < 16 {
		return 0x79CC4519
	}
	return 0x7A879D8A
}
func rotl(x uint32, n int) uint32 {
	n &= 31
	if n == 0 {
		return x
	}
	return (x << n) | (x >> (32 - n))
}

func sm3ToHex(v [8]uint32) string {
	var out [32]byte
	for i := 0; i < 8; i++ {
		idx := i * 4
		out[idx] = byte(v[i] >> 24)
		out[idx+1] = byte(v[i] >> 16)
		out[idx+2] = byte(v[i] >> 8)
		out[idx+3] = byte(v[i])
	}
	const hex = "0123456789abcdef"
	dst := make([]byte, 64)
	for i, b := range out {
		dst[i*2] = hex[b>>4]
		dst[i*2+1] = hex[b&0x0F]
	}
	return string(dst)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
