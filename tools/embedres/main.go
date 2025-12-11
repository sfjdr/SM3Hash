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
	rtManifest  = 24
	langENUS    = 0x0409
	langNeutral = 0
	versionStr  = "1.0.0.0"
	appName     = "SM3Hash"
	appDesc     = "SM3 Hash Utility"
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

func updateResources(exe string, entries []iconEntry, verData []byte) {
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

	if len(verData) > 0 {
		r, _, e = procUpdateResource.Call(
			handle,
			uintptr(rtVersion),
			uintptr(1),
			uintptr(langENUS),
			uintptr(unsafe.Pointer(&verData[0])),
			uintptr(len(verData)),
		)
		if r == 0 {
			check(e, "UpdateResource RT_VERSION")
		}
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

func versionParts(v string) (a, b, c, d uint16) {
	fields := strings.Split(v, ".")
	val := func(i int) uint16 {
		if i >= len(fields) {
			return 0
		}
		n := 0
		for _, ch := range fields[i] {
			if ch >= '0' && ch <= '9' {
				n = n*10 + int(ch-'0')
			}
		}
		if n > 0xFFFF {
			return 0xFFFF
		}
		return uint16(n)
	}
	return val(0), val(1), val(2), val(3)
}

func buildVersionInfoMinimal(fileVer, productVer string) []byte {
	ma1, ma2, mi1, mi2 := versionParts(fileVer)
	pa1, pa2, pi1, pi2 := versionParts(productVer)

	stringBlock := func(key, val string) []byte {
		var b bytes.Buffer
		start := b.Len()
		valWords := len(utf16.Encode([]rune(val))) + 1
		binary.Write(&b, binary.LittleEndian, uint16(0))        // wLength
		binary.Write(&b, binary.LittleEndian, uint16(valWords)) // wValueLength (words)
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

	var st bytes.Buffer
	stStart := st.Len()
	binary.Write(&st, binary.LittleEndian, uint16(0))
	binary.Write(&st, binary.LittleEndian, uint16(0))
	binary.Write(&st, binary.LittleEndian, uint16(1))
	st.Write(utf16BytesNoNull("040904B0"))
	binary.Write(&st, binary.LittleEndian, uint16(0))
	align4(&st)
	pairs := map[string]string{
		"CompanyName":      "sfjdr",
		"FileDescription":  "SM3 Hash Tool",
		"FileVersion":      fileVer,
		"ProductVersion":   productVer,
		"InternalName":     "SM3Hash",
		"OriginalFilename": "SM3Hash.exe",
		"ProductName":      "SM3Hash",
	}
	for k, v := range pairs {
		st.Write(stringBlock(k, v))
	}
	stBytes := st.Bytes()
	binary.LittleEndian.PutUint16(stBytes[stStart:], uint16(len(stBytes)))

	var sfi bytes.Buffer
	sfiStart := sfi.Len()
	binary.Write(&sfi, binary.LittleEndian, uint16(0))
	binary.Write(&sfi, binary.LittleEndian, uint16(0))
	binary.Write(&sfi, binary.LittleEndian, uint16(1))
	sfi.Write(utf16BytesNoNull("StringFileInfo"))
	binary.Write(&sfi, binary.LittleEndian, uint16(0))
	align4(&sfi)
	sfi.Write(stBytes)
	sfiBytes := sfi.Bytes()
	binary.LittleEndian.PutUint16(sfiBytes[sfiStart:], uint16(len(sfiBytes)))

	var vb bytes.Buffer
	vbStart := vb.Len()
	binary.Write(&vb, binary.LittleEndian, uint16(0))
	binary.Write(&vb, binary.LittleEndian, uint16(4)) // bytes
	binary.Write(&vb, binary.LittleEndian, uint16(0)) // binary
	vb.Write(utf16BytesNoNull("Translation"))
	binary.Write(&vb, binary.LittleEndian, uint16(0))
	align4(&vb)
	binary.Write(&vb, binary.LittleEndian, uint16(0x0409))
	binary.Write(&vb, binary.LittleEndian, uint16(0x04B0))
	align4(&vb)
	vbBytes := vb.Bytes()
	binary.LittleEndian.PutUint16(vbBytes[vbStart:], uint16(len(vbBytes)))

	var vfi bytes.Buffer
	vfiStart := vfi.Len()
	binary.Write(&vfi, binary.LittleEndian, uint16(0))
	binary.Write(&vfi, binary.LittleEndian, uint16(0))
	binary.Write(&vfi, binary.LittleEndian, uint16(1))
	vfi.Write(utf16BytesNoNull("VarFileInfo"))
	binary.Write(&vfi, binary.LittleEndian, uint16(0))
	align4(&vfi)
	vfi.Write(vbBytes)
	vfiBytes := vfi.Bytes()
	binary.LittleEndian.PutUint16(vfiBytes[vfiStart:], uint16(len(vfiBytes)))

	var root bytes.Buffer
	rootStart := root.Len()
	binary.Write(&root, binary.LittleEndian, uint16(0))
	binary.Write(&root, binary.LittleEndian, uint16(uint16(unsafe.Sizeof(VS_FIXEDFILEINFO{}))))
	binary.Write(&root, binary.LittleEndian, uint16(0))
	root.Write(utf16BytesNoNull("VS_VERSION_INFO"))
	binary.Write(&root, binary.LittleEndian, uint16(0))
	align4(&root)

	ffi := VS_FIXEDFILEINFO{
		Signature:        0xFEEF04BD,
		StrucVersion:     0x00010000,
		FileVersionMS:    uint32(ma1)<<16 | uint32(ma2),
		FileVersionLS:    uint32(mi1)<<16 | uint32(mi2),
		ProductVersionMS: uint32(pa1)<<16 | uint32(pa2),
		ProductVersionLS: uint32(pi1)<<16 | uint32(pi2),
		FileFlagsMask:    0x3F,
		FileFlags:        0,
		FileOS:           0x00040004,
		FileType:         0x00000001,
		FileSubtype:      0,
		FileDateMS:       0,
		FileDateLS:       0,
	}
	binary.Write(&root, binary.LittleEndian, ffi)
	align4(&root)
	root.Write(sfiBytes)
	align4(&root)
	root.Write(vfiBytes)
	rootBytes := root.Bytes()
	binary.LittleEndian.PutUint16(rootBytes[rootStart:], uint16(len(rootBytes)))
	return rootBytes
}

type VS_FIXEDFILEINFO struct {
	Signature        uint32
	StrucVersion     uint32
	FileVersionMS    uint32
	FileVersionLS    uint32
	ProductVersionMS uint32
	ProductVersionLS uint32
	FileFlagsMask    uint32
	FileFlags        uint32
	FileOS           uint32
	FileType         uint32
	FileSubtype      uint32
	FileDateMS       uint32
	FileDateLS       uint32
}

func buildManifest(appName, appDesc string) []byte {
	xml := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="1.0.0.0" processorArchitecture="*" name="` + appName + `" type="win32"/>
  <description>` + appDesc + `</description>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1e9b04fb-f9e5-4b3a-915f-6b9ab8cdf0c8}"/>
      <supportedOS Id="{4f476546-35e5-4b0a-b4fd-226d8f235a97}"/>
      <supportedOS Id="{a4b1d670-d5e6-4b07-b87e-efb2a1a3d0f0}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware>true/pm</dpiAware>
    </windowsSettings>
  </application>
</assembly>`
	// Null-terminate for SxS parser safety.
	return append([]byte(xml), 0, 0)
}

func updateManifestResource(exe string, manifest []byte) {
	handle, _, err := procBeginUpdateResource.Call(uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(exe))), uintptr(0))
	if handle == 0 {
		check(err, "BeginUpdateResource (manifest)")
	}
	defer func() {
		r, _, e := procEndUpdateResource.Call(handle, uintptr(0))
		if r == 0 {
			check(e, "EndUpdateResource (manifest)")
		}
	}()

	r, _, e := procUpdateResource.Call(
		handle,
		uintptr(rtManifest),
		uintptr(1),
		uintptr(langNeutral),
		uintptr(unsafe.Pointer(&manifest[0])),
		uintptr(len(manifest)),
	)
	if r == 0 {
		check(e, "UpdateResource RT_MANIFEST")
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

	verData := buildVersionInfoMinimal(versionStr, versionStr)
	updateResources(exePath, entries, verData)
	updateManifestResource(exePath, buildManifest(appName, appDesc))

	log.Println("Icon, manifest, version", versionStr, "injected into", exePath)
}
