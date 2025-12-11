package main

// Helper to embed an existing app.ico and version/manifest into SM3Hash.exe using Win32 UpdateResource.

import (
	"bytes"
	"encoding/binary"
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

type iconEntry struct {
	width, height byte
	size          int
	id            uint16
	data          []byte
}

func parseICO(path string) ([]iconEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < 6 {
		return nil, os.ErrInvalid
	}
	count := int(binary.LittleEndian.Uint16(raw[4:6]))
	entries := make([]iconEntry, 0, count)
	dirOffset := 6
	for i := 0; i < count; i++ {
		entryOff := dirOffset + i*16
		if entryOff+16 > len(raw) {
			break
		}
		w := raw[entryOff]
		h := raw[entryOff+1]
		size := int(binary.LittleEndian.Uint32(raw[entryOff+8 : entryOff+12]))
		offset := int(binary.LittleEndian.Uint32(raw[entryOff+12 : entryOff+16]))
		if offset+size > len(raw) || size <= 0 {
			continue
		}
		data := raw[offset : offset+size]
		entries = append(entries, iconEntry{
			width:  w,
			height: h,
			size:   size,
			id:     uint16(len(entries) + 1),
			data:   data,
		})
	}
	return entries, nil
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

func stringBlock(name, value string) []byte {
	var buf bytes.Buffer
	start := buf.Len()
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	binary.Write(&buf, binary.LittleEndian, uint16(len(value)*2))
	binary.Write(&buf, binary.LittleEndian, uint16(1)) // text
	buf.Write(utf16BytesNoNull(name))
	binary.Write(&buf, binary.LittleEndian, uint16(0))
	align4(&buf)
	buf.Write(utf16BytesNoNull(value))
	b := buf.Bytes()
	binary.LittleEndian.PutUint16(b[start:], uint16(len(b)))
	return b
}

func buildVersionInfoMinimal(fileVer, prodVer string) []byte {
	ma1, ma2, mi1, mi2 := versionParts(fileVer)
	pa1, pa2, pi1, pi2 := versionParts(prodVer)

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
		"FileDescription":  "SM3Hash",
		"FileVersion":      fileVer,
		"ProductVersion":   prodVer,
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
	// Null-terminate for SxS parser safety (single terminator).
	return append([]byte(xml), 0)
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
	iconPath := filepath.Join(".", "app.ico")
	exePath := filepath.Join(".", "SM3Hash.exe")
	entries, err := parseICO(iconPath)
	check(err, "read app.ico")

	verData := buildVersionInfoMinimal(versionStr, versionStr)
	updateResources(exePath, entries, verData)

	log.Println("Icon + version", versionStr, "injected into", exePath)
}
