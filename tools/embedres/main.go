package main

// Small helper to generate app.ico from icon.png and inject icon + version info into SM3Hash.exe.
// Uses Win32 UpdateResource; no external tools required.

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procBeginUpdateResource = kernel32.NewProc("BeginUpdateResourceW")
	procUpdateResource      = kernel32.NewProc("UpdateResourceW")
	procEndUpdateResource   = kernel32.NewProc("EndUpdateResourceW")
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	langENUS    = 0x0409
)

func check(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}

func utf16BytesNoNull(s string) []byte {
	u16 := utf16.Encode([]rune(s))
	buf := make([]byte, len(u16)*2)
	for i, v := range u16 {
		binary.LittleEndian.PutUint16(buf[i*2:], v)
	}
	return buf
}

func align4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

func resizeNearest(src image.Image, size int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	srcBounds := src.Bounds()
	sw := srcBounds.Dx()
	sh := srcBounds.Dy()
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sx := srcBounds.Min.X + x*sw/size
			sy := srcBounds.Min.Y + y*sh/size
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func buildICO(pngs [][]byte, sizes []int) []byte {
	var buf bytes.Buffer
	count := len(pngs)
	// ICONDIR
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(count))
	offset := 6 + 16*count
	for i, data := range pngs {
		sz := sizes[i]
		wb := byte(sz)
		hb := byte(sz)
		if sz >= 256 {
			wb, hb = 0, 0
		}
		buf.WriteByte(wb)
		buf.WriteByte(hb)
		buf.WriteByte(0)                                    // colors
		buf.WriteByte(0)                                    // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))  // planes
		binary.Write(&buf, binary.LittleEndian, uint16(32)) // bitcount
		binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
		binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(data)
	}
	for _, data := range pngs {
		buf.Write(data)
	}
	return buf.Bytes()
}

type iconEntry struct {
	width, height byte
	size          int
	id            uint16
	data          []byte
}

func updateIconResources(exe string, entries []iconEntry) {
	handle, _, err := procBeginUpdateResource.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(exe))), uintptr(0))
	if handle == 0 {
		check(err, "BeginUpdateResource")
	}
	defer func() {
		r, _, e := procEndUpdateResource.Call(handle, uintptr(0))
		if r == 0 {
			check(e, "EndUpdateResource")
		}
	}()

	for _, it := range entries {
		if len(it.data) == 0 {
			continue
		}
		r, _, e := procUpdateResource.Call(
			handle,
			uintptr(rtIcon),
			uintptr(it.id),
			uintptr(langENUS),
			uintptr(unsafe.Pointer(&it.data[0])),
			uintptr(len(it.data)),
		)
		if r == 0 {
			check(e, "UpdateResource RT_ICON")
		}
	}

	grp := buildGroupIcon(entries)
	r, _, e := procUpdateResource.Call(
		handle,
		uintptr(rtGroupIcon),
		uintptr(1),
		uintptr(langENUS),
		uintptr(unsafe.Pointer(&grp[0])),
		uintptr(len(grp)),
	)
	if r == 0 {
		check(e, "UpdateResource RT_GROUP_ICON")
	}
}

func buildGroupIcon(entries []iconEntry) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(len(entries)))
	for _, it := range entries {
		buf.WriteByte(it.width)
		buf.WriteByte(it.height)
		buf.WriteByte(0)
		buf.WriteByte(0)
		binary.Write(&buf, binary.LittleEndian, uint16(1))
		binary.Write(&buf, binary.LittleEndian, uint16(32))
		binary.Write(&buf, binary.LittleEndian, uint32(it.size))
		binary.Write(&buf, binary.LittleEndian, it.id)
	}
	return buf.Bytes()
}

func versionDwords(v string) (ms, ls uint32) {
	parts := [4]uint16{}
	chunks := strings.Split(v, ".")
	for i := 0; i < len(parts) && i < len(chunks); i++ {
		var val uint64
		for _, c := range chunks[i] {
			if c >= '0' && c <= '9' {
				val = val*10 + uint64(c-'0')
			}
		}
		if val > 0xFFFF {
			val = 0xFFFF
		}
		parts[i] = uint16(val)
	}
	ms = uint32(parts[0])<<16 | uint32(parts[1])
	ls = uint32(parts[2])<<16 | uint32(parts[3])
	return
}

func buildVersionInfo(fileVer, productVer string) []byte {
	fileMS, fileLS := versionDwords(fileVer)
	prodMS, prodLS := versionDwords(productVer)

	// Fixed file info
	var ffi bytes.Buffer
	binary.Write(&ffi, binary.LittleEndian, uint32(0xFEEF04BD))
	binary.Write(&ffi, binary.LittleEndian, uint32(0x00010000))
	binary.Write(&ffi, binary.LittleEndian, fileMS)
	binary.Write(&ffi, binary.LittleEndian, fileLS)
	binary.Write(&ffi, binary.LittleEndian, prodMS)
	binary.Write(&ffi, binary.LittleEndian, prodLS)
	binary.Write(&ffi, binary.LittleEndian, uint32(0x3F))
	binary.Write(&ffi, binary.LittleEndian, uint32(0))
	binary.Write(&ffi, binary.LittleEndian, uint32(0x00040004)) // VOS_NT_WINDOWS32
	binary.Write(&ffi, binary.LittleEndian, uint32(0x00000001)) // VFT_APP
	binary.Write(&ffi, binary.LittleEndian, uint32(0))
	binary.Write(&ffi, binary.LittleEndian, uint32(0))
	binary.Write(&ffi, binary.LittleEndian, uint32(0))

	stringsKV := map[string]string{
		"CompanyName":      "sfjdr",
		"FileDescription":  "SM3 Hash Tool",
		"FileVersion":      fileVer,
		"InternalName":     "SM3Hash",
		"OriginalFilename": "SM3Hash.exe",
		"ProductName":      "SM3Hash",
		"ProductVersion":   productVer,
	}

	stringsBlock := func(key, val string) []byte {
		var b bytes.Buffer
		start := b.Len()
		valWords := len(utf16.Encode([]rune(val))) + 1
		binary.Write(&b, binary.LittleEndian, uint16(0))        // wLength placeholder
		binary.Write(&b, binary.LittleEndian, uint16(valWords)) // wValueLength (in WORDs, includes null)
		binary.Write(&b, binary.LittleEndian, uint16(1))        // text
		b.Write(utf16BytesNoNull(key))
		binary.Write(&b, binary.LittleEndian, uint16(0)) // null
		align4(&b)
		b.Write(utf16BytesNoNull(val))
		binary.Write(&b, binary.LittleEndian, uint16(0)) // null
		align4(&b)
		data := b.Bytes()
		binary.LittleEndian.PutUint16(data[start:], uint16(len(data)))
		return data
	}

	var stringTable bytes.Buffer
	stStart := stringTable.Len()
	binary.Write(&stringTable, binary.LittleEndian, uint16(0)) // len placeholder
	binary.Write(&stringTable, binary.LittleEndian, uint16(0)) // value length
	binary.Write(&stringTable, binary.LittleEndian, uint16(1)) // text
	stringTable.Write(utf16BytesNoNull("040904B0"))
	binary.Write(&stringTable, binary.LittleEndian, uint16(0)) // null
	align4(&stringTable)
	for k, v := range stringsKV {
		stringTable.Write(stringsBlock(k, v))
	}
	stBytes := stringTable.Bytes()
	binary.LittleEndian.PutUint16(stBytes[stStart:], uint16(len(stBytes)))

	var stringFileInfo bytes.Buffer
	sfiStart := stringFileInfo.Len()
	binary.Write(&stringFileInfo, binary.LittleEndian, uint16(0)) // len placeholder
	binary.Write(&stringFileInfo, binary.LittleEndian, uint16(0)) // value length
	binary.Write(&stringFileInfo, binary.LittleEndian, uint16(1)) // text
	stringFileInfo.Write(utf16BytesNoNull("StringFileInfo"))
	binary.Write(&stringFileInfo, binary.LittleEndian, uint16(0)) // null
	align4(&stringFileInfo)
	stringFileInfo.Write(stBytes)
	sfiBytes := stringFileInfo.Bytes()
	binary.LittleEndian.PutUint16(sfiBytes[sfiStart:], uint16(len(sfiBytes)))

	var varBlock bytes.Buffer
	vbStart := varBlock.Len()
	binary.Write(&varBlock, binary.LittleEndian, uint16(0)) // len placeholder
	binary.Write(&varBlock, binary.LittleEndian, uint16(4)) // value length (bytes)
	binary.Write(&varBlock, binary.LittleEndian, uint16(0)) // binary
	varBlock.Write(utf16BytesNoNull("Translation"))
	binary.Write(&varBlock, binary.LittleEndian, uint16(0)) // null
	align4(&varBlock)
	translations := [][2]uint16{{0x0409, 0x04B0}, {0x0804, 0x04B0}}
	for _, t := range translations {
		binary.Write(&varBlock, binary.LittleEndian, t[0])
		binary.Write(&varBlock, binary.LittleEndian, t[1])
	}
	vbBytes := varBlock.Bytes()
	binary.LittleEndian.PutUint16(vbBytes[vbStart+2:], uint16(len(translations)*4)) // wValueLength in bytes
	binary.LittleEndian.PutUint16(vbBytes[vbStart:], uint16(len(vbBytes)))

	var varFileInfo bytes.Buffer
	vfiStart := varFileInfo.Len()
	binary.Write(&varFileInfo, binary.LittleEndian, uint16(0)) // len placeholder
	binary.Write(&varFileInfo, binary.LittleEndian, uint16(0)) // value length
	binary.Write(&varFileInfo, binary.LittleEndian, uint16(0)) // binary
	varFileInfo.Write(utf16BytesNoNull("VarFileInfo"))
	binary.Write(&varFileInfo, binary.LittleEndian, uint16(0)) // null
	align4(&varFileInfo)
	varFileInfo.Write(vbBytes)
	vfiBytes := varFileInfo.Bytes()
	binary.LittleEndian.PutUint16(vfiBytes[vfiStart:], uint16(len(vfiBytes)))

	var root bytes.Buffer
	rootStart := root.Len()
	binary.Write(&root, binary.LittleEndian, uint16(0))                // len placeholder
	binary.Write(&root, binary.LittleEndian, uint16(len(ffi.Bytes()))) // value length
	binary.Write(&root, binary.LittleEndian, uint16(0))                // binary
	root.Write(utf16BytesNoNull("VS_VERSION_INFO"))
	binary.Write(&root, binary.LittleEndian, uint16(0)) // null
	align4(&root)
	root.Write(ffi.Bytes())
	align4(&root)
	root.Write(sfiBytes)
	align4(&root)
	root.Write(vfiBytes)
	rootBytes := root.Bytes()
	binary.LittleEndian.PutUint16(rootBytes[rootStart:], uint16(len(rootBytes)))
	return rootBytes
}

func updateVersionResource(exe string, data []byte) {
	handle, _, err := procBeginUpdateResource.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(exe))), uintptr(0))
	if handle == 0 {
		check(err, "BeginUpdateResource (version)")
	}
	defer func() {
		r, _, e := procEndUpdateResource.Call(handle, uintptr(0))
		if r == 0 {
			check(e, "EndUpdateResource (version)")
		}
	}()

	r, _, e := procUpdateResource.Call(
		handle,
		uintptr(rtVersion),
		uintptr(1),
		uintptr(langENUS),
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)),
	)
	if r == 0 {
		check(e, "UpdateResource RT_VERSION")
	}
}

func main() {
	iconPath := filepath.Join(".", "icon.png")
	exePath := filepath.Join(".", "SM3Hash.exe")
	imgFile, err := os.Open(iconPath)
	check(err, "open icon.png")
	defer imgFile.Close()
	img, _, err := image.Decode(imgFile)
	check(err, "decode icon.png")

	sizes := []int{256, 128, 64, 48, 32, 16}
	var pngs [][]byte
	var entries []iconEntry
	for i, sz := range sizes {
		resized := resizeNearest(img, sz)
		var buf bytes.Buffer
		err := png.Encode(&buf, resized)
		check(err, "encode png")
		data := buf.Bytes()
		pngs = append(pngs, data)
		wb := byte(sz)
		hb := byte(sz)
		if sz >= 256 {
			wb, hb = 0, 0
		}
		entries = append(entries, iconEntry{
			width:  wb,
			height: hb,
			size:   len(data),
			id:     uint16(i + 1),
			data:   data,
		})
	}

	icoBytes := buildICO(pngs, sizes)
	err = os.WriteFile("app.ico", icoBytes, 0644)
	check(err, "write app.ico")

	updateIconResources(exePath, entries)

	verData := buildVersionInfo("1.0.0.0", "1.0.0.0")
	updateVersionResource(exePath, verData)

	log.Println("Icon + version injected into", exePath)
}
