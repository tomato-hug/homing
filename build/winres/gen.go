// Command winres generates a Windows COFF resource object (.syso) containing:
//   1. Multi-resolution Homing application icon (16, 24, 32, 48, 64, 128, 256 px PNG/ICO frames)
//   2. Windows 10/11 PerMonitorV2 High-DPI & Common Controls v6 application manifest
//
// Go's linker automatically embeds cmd/homing/rsrc_windows_<arch>.syso into Homing.exe
// without requiring mingw windres or external third-party tools.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

const (
	imageFileMachineAMD64 = 0x8664
	imageFileMachineARM64 = 0xAA64

	relocAMD64Addr32NB = 0x0003
	relocARM64Addr32NB = 0x0002

	rtIcon      = 3
	rtGroupIcon = 14
	rtManifest  = 24
)

const appManifestXML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0" xmlns:asmv3="urn:schemas-microsoft-com:asm.v3">
  <assemblyIdentity type="win32" name="dev.homing.desktop" version="1.0.0.0"/>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
  <asmv3:application>
    <asmv3:windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2,PerMonitor</dpiAwareness>
    </asmv3:windowsSettings>
  </asmv3:application>
</assembly>`

const (
	pathFull = "M96 41 L87 37 C86 28 74 24 68 30 C64 34 64 42 62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C84 47 85 44 88 43 Z"
	pathNeck = "M63 44 C70 50 80 51 86 49 L87 55 C79 57 68 55 61 50 Z"
	pathWing = "M36 71 C50 61 66 61 76 68 C64 80 48 82 36 71 Z"
)

func main() {
	arch := flag.String("arch", "amd64", "target Windows architecture: amd64 or arm64")
	out := flag.String("out", "", "output .syso path")
	icoOut := flag.String("ico", "", "optional standalone .ico output path")
	flag.Parse()

	var machine, relocType uint16
	switch *arch {
	case "amd64":
		machine = imageFileMachineAMD64
		relocType = relocAMD64Addr32NB
	case "arm64":
		machine = imageFileMachineARM64
		relocType = relocARM64Addr32NB
	default:
		log.Fatalf("unsupported -arch %q (want amd64 or arm64)", *arch)
	}
	if *out == "" {
		log.Fatal("-out is required")
	}

	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	full, neck, wing := mustParsePath(pathFull), mustParsePath(pathNeck), mustParsePath(pathWing)
	iconBlobs := make([][]byte, len(sizes))
	for i, px := range sizes {
		img := renderAppBadge(px, full, neck, wing)
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			log.Fatal(err)
		}
		iconBlobs[i] = buf.Bytes()
	}

	if *icoOut != "" {
		if err := os.MkdirAll(filepath.Dir(*icoOut), 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(*icoOut, buildICOFile(sizes, iconBlobs), 0o644); err != nil {
			log.Fatal(err)
		}
	}

	groupBlob := buildGroupIconDir(sizes, iconBlobs)
	syso := buildCOFFResourceObject(machine, relocType, iconBlobs, groupBlob, []byte(appManifestXML))
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, syso, 0o644); err != nil {
		log.Fatal(err)
	}
}

func buildGroupIconDir(sizes []int, blobs [][]byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint16(0))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(len(sizes)))
	for i, px := range sizes {
		w, h := byte(px), byte(px)
		if px >= 256 {
			w, h = 0, 0
		}
		b.WriteByte(w)
		b.WriteByte(h)
		b.WriteByte(0)
		b.WriteByte(0)
		_ = binary.Write(&b, binary.LittleEndian, uint16(1))
		_ = binary.Write(&b, binary.LittleEndian, uint16(32))
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(blobs[i])))
		_ = binary.Write(&b, binary.LittleEndian, uint16(i+1)) // nID (1-based RT_ICON ID)
	}
	return b.Bytes()
}

func buildICOFile(sizes []int, blobs [][]byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint16(0))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(len(sizes)))
	offset := uint32(6 + 16*len(sizes))
	for i, px := range sizes {
		w, h := byte(px), byte(px)
		if px >= 256 {
			w, h = 0, 0
		}
		b.WriteByte(w)
		b.WriteByte(h)
		b.WriteByte(0)
		b.WriteByte(0)
		_ = binary.Write(&b, binary.LittleEndian, uint16(1))
		_ = binary.Write(&b, binary.LittleEndian, uint16(32))
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(blobs[i])))
		_ = binary.Write(&b, binary.LittleEndian, offset)
		offset += uint32(len(blobs[i]))
	}
	for _, raw := range blobs {
		b.Write(raw)
	}
	return b.Bytes()
}

type resLeaf struct {
	typeID uint16
	nameID uint16
	langID uint16
	data   []byte
}

func buildCOFFResourceObject(machine, relocType uint16, icons [][]byte, groupIcon, manifest []byte) []byte {
	var leaves []resLeaf
	for i, raw := range icons {
		leaves = append(leaves, resLeaf{typeID: rtIcon, nameID: uint16(i + 1), langID: 0x0409, data: raw})
	}
	leaves = append(leaves, resLeaf{typeID: rtGroupIcon, nameID: 1, langID: 0x0409, data: groupIcon})
	leaves = append(leaves, resLeaf{typeID: rtManifest, nameID: 1, langID: 0x0409, data: manifest})

	// Group leaves by typeID in ascending order (3 = RT_ICON, 14 = RT_GROUP_ICON, 24 = RT_MANIFEST).
	typeOrder := []uint16{rtIcon, rtGroupIcon, rtManifest}
	byType := map[uint16][]resLeaf{}
	for _, lf := range leaves {
		byType[lf.typeID] = append(byType[lf.typeID], lf)
	}

	var rsrc bytes.Buffer
	writeDir := func(idCount uint16) {
		rsrc.Write(make([]byte, 12))
		_ = binary.Write(&rsrc, binary.LittleEndian, uint16(0))
		_ = binary.Write(&rsrc, binary.LittleEndian, idCount)
	}
	writeEntry := func(id uint16, offset uint32, isSubdir bool) {
		_ = binary.Write(&rsrc, binary.LittleEndian, uint32(id))
		if isSubdir {
			offset |= 0x80000000
		}
		_ = binary.Write(&rsrc, binary.LittleEndian, offset)
	}

	// Calculate deterministic offsets inside .rsrc:
	// Level 1 (Type dir): 16 + 8*len(typeOrder)
	l1Size := uint32(16 + 8*len(typeOrder))
	l2Offset := l1Size
	l2Offsets := make([]uint32, len(typeOrder))
	cur := l2Offset
	for i, t := range typeOrder {
		l2Offsets[i] = cur
		cur += uint32(16 + 8*len(byType[t]))
	}
	l3Offsets := make([]uint32, len(leaves))
	for i := range leaves {
		l3Offsets[i] = cur
		cur += 24 // 16-byte dir + 1 entry (8 bytes)
	}
	dataEntryOffsets := make([]uint32, len(leaves))
	for i := range leaves {
		dataEntryOffsets[i] = cur
		cur += 16 // IMAGE_RESOURCE_DATA_ENTRY
	}

	// Emit Level 1
	writeDir(uint16(len(typeOrder)))
	for i, t := range typeOrder {
		writeEntry(t, l2Offsets[i], true)
	}
	// Emit Level 2
	leafIdx := 0
	for _, t := range typeOrder {
		list := byType[t]
		writeDir(uint16(len(list)))
		for _, lf := range list {
			writeEntry(lf.nameID, l3Offsets[leafIdx], true)
			leafIdx++
		}
	}
	// Emit Level 3
	for i, lf := range leaves {
		writeDir(1)
		writeEntry(lf.langID, dataEntryOffsets[i], false)
	}

	// Precompute raw payload offsets (8-byte aligned)
	payloadStart := (cur + 7) &^ 7
	payloadOffsets := make([]uint32, len(leaves))
	pCur := payloadStart
	for i, lf := range leaves {
		payloadOffsets[i] = pCur
		pCur = (pCur + uint32(len(lf.data)) + 7) &^ 7
	}

	// Emit IMAGE_RESOURCE_DATA_ENTRY list + record relocation positions
	relocAddrs := make([]uint32, len(leaves))
	for i, lf := range leaves {
		relocAddrs[i] = uint32(rsrc.Len()) // OffsetToData field is at byte 0 of DATA_ENTRY
		_ = binary.Write(&rsrc, binary.LittleEndian, payloadOffsets[i])
		_ = binary.Write(&rsrc, binary.LittleEndian, uint32(len(lf.data)))
		_ = binary.Write(&rsrc, binary.LittleEndian, uint32(0))
		_ = binary.Write(&rsrc, binary.LittleEndian, uint32(0))
	}
	for uint32(rsrc.Len()) < payloadStart {
		rsrc.WriteByte(0)
	}
	for _, lf := range leaves {
		rsrc.Write(lf.data)
		for rsrc.Len()%8 != 0 {
			rsrc.WriteByte(0)
		}
	}

	rsrcBytes := rsrc.Bytes()
	const (
		coffHeaderSize    = 20
		sectionHeaderSize = 40
		relocEntrySize    = 10
		symbolSize        = 18
	)
	rawDataPtr := uint32(coffHeaderSize + sectionHeaderSize)
	relocPtr := rawDataPtr + uint32(len(rsrcBytes))
	symPtr := relocPtr + uint32(len(relocAddrs)*relocEntrySize)

	var out bytes.Buffer
	// IMAGE_FILE_HEADER (20 bytes)
	_ = binary.Write(&out, binary.LittleEndian, machine)
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))   // NumberOfSections
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))   // TimeDateStamp
	_ = binary.Write(&out, binary.LittleEndian, symPtr)      // PointerToSymbolTable
	_ = binary.Write(&out, binary.LittleEndian, uint32(1))   // NumberOfSymbols
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))   // SizeOfOptionalHeader
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))   // Characteristics

	// IMAGE_SECTION_HEADER for ".rsrc" (40 bytes)
	var secName [8]byte
	copy(secName[:], ".rsrc")
	out.Write(secName[:])
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))               // VirtualSize
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))               // VirtualAddress
	_ = binary.Write(&out, binary.LittleEndian, uint32(len(rsrcBytes)))  // SizeOfRawData
	_ = binary.Write(&out, binary.LittleEndian, rawDataPtr)              // PointerToRawData
	_ = binary.Write(&out, binary.LittleEndian, relocPtr)                // PointerToRelocations
	_ = binary.Write(&out, binary.LittleEndian, uint32(0))               // PointerToLinenumbers
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(relocAddrs))) // NumberOfRelocations
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))               // NumberOfLinenumbers
	_ = binary.Write(&out, binary.LittleEndian, uint32(0x40000040))      // CNT_INITIALIZED_DATA | MEM_READ

	// Section raw data
	out.Write(rsrcBytes)

	// Relocations (10 bytes each)
	for _, addr := range relocAddrs {
		_ = binary.Write(&out, binary.LittleEndian, addr)      // VirtualAddress
		_ = binary.Write(&out, binary.LittleEndian, uint32(0)) // SymbolTableIndex (symbol 0 = .rsrc)
		_ = binary.Write(&out, binary.LittleEndian, relocType) // Type (ADDR32NB)
	}

	// Symbol 0: ".rsrc" (18 bytes)
	out.Write(secName[:])
	_ = binary.Write(&out, binary.LittleEndian, uint32(0)) // Value
	_ = binary.Write(&out, binary.LittleEndian, int16(1))  // SectionNumber = 1
	_ = binary.Write(&out, binary.LittleEndian, uint16(0)) // Type
	out.WriteByte(3)                                       // StorageClass = IMAGE_SYM_CLASS_STATIC
	out.WriteByte(0)                                       // NumberOfAuxSymbols

	// Empty string table (4 bytes: size = 4)
	_ = binary.Write(&out, binary.LittleEndian, uint32(4))
	return out.Bytes()
}

// --- Vector rendering for crisp Windows desktop icon (16..256 px) ---

type pt struct{ x, y float64 }
type seg struct{ a, b pt }

func renderAppBadge(px int, full, neck, wing []seg) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	cBgTop := color.NRGBA{0x13, 0x1E, 0x35, 0xFF}
	cBgBot := color.NRGBA{0x0B, 0x11, 0x20, 0xFF}
	cBorder := color.NRGBA{0x2D, 0xD4, 0xBF, 0x55}
	cBody := color.NRGBA{0xF8, 0xFA, 0xFC, 0xFF}
	cWing := color.NRGBA{0x2D, 0xD4, 0xBF, 0xFF}
	cEye := color.NRGBA{0x0F, 0x17, 0x2A, 0xFF}

	// 4x4 supersampling for crisp edges at every DPI
	const ss = 4
	fpx := float64(px)
	radius := fpx * 0.225
	pad := fpx * 0.03

	mapBird := func(x, y float64) (float64, float64) {
		// Map [0..1] normalized icon space to bird box [8..102, 14..102]
		bx := 8.0 + (x-0.10)/0.80*90.0
		by := 14.0 + (y-0.11)/0.80*86.0
		return bx, by
	}

	for y := 0; y < px; y++ {
		tGrad := float64(y) / math.Max(1, fpx-1)
		bgR := float64(cBgTop.R)*(1-tGrad) + float64(cBgBot.R)*tGrad
		bgG := float64(cBgTop.G)*(1-tGrad) + float64(cBgBot.G)*tGrad
		bgB := float64(cBgTop.B)*(1-tGrad) + float64(cBgBot.B)*tGrad

		for x := 0; x < px; x++ {
			var rSum, gSum, bSum, aSum float64
			for sy := 0; sy < ss; sy++ {
				py := float64(y) + (float64(sy)+0.5)/ss
				for sx := 0; sx < ss; sx++ {
					pxx := float64(x) + (float64(sx)+0.5)/ss
					dist := sdRoundedBox(pxx-fpx*0.5, py-fpx*0.5, fpx*0.5-pad, fpx*0.5-pad, radius)
					if dist > 0 {
						continue
					}
					cr, cg, cb := bgR, bgG, bgB
					if dist > -math.Max(1.0, fpx*0.025) {
						ba := float64(cBorder.A) / 255.0
						cr = cr*(1-ba) + float64(cBorder.R)*ba
						cg = cg*(1-ba) + float64(cBorder.G)*ba
						cb = cb*(1-ba) + float64(cBorder.B)*ba
					}
					bx, by := mapBird(pxx/fpx, py/fpx)
					if inPath(bx, by, full) && !inPath(bx, by, neck) {
						cr, cg, cb = float64(cBody.R), float64(cBody.G), float64(cBody.B)
						if inPath(bx, by, wing) {
							cr, cg, cb = float64(cWing.R), float64(cWing.G), float64(cWing.B)
						}
						if math.Hypot(bx-78.0, by-34.0) <= 3.1 {
							cr, cg, cb = float64(cEye.R), float64(cEye.G), float64(cEye.B)
						}
					}
					rSum += cr
					gSum += cg
					bSum += cb
					aSum += 255.0
				}
			}
			n := float64(ss * ss)
			alpha := aSum / n
			if alpha <= 0.5 {
				continue
			}
			hits := aSum / 255.0
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(rSum / hits)),
				G: uint8(math.Round(gSum / hits)),
				B: uint8(math.Round(bSum / hits)),
				A: uint8(math.Round(alpha)),
			})
		}
	}
	return img
}

func sdRoundedBox(px, py, hx, hy, r float64) float64 {
	qx := math.Abs(px) - hx + r
	qy := math.Abs(py) - hy + r
	out := math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	in := math.Min(math.Max(qx, qy), 0)
	return out + in - r
}

func inPath(x, y float64, segs []seg) bool {
	inside := false
	for _, s := range segs {
		if (s.a.y > y) != (s.b.y > y) {
			ix := s.a.x + (y-s.a.y)*(s.b.x-s.a.x)/(s.b.y-s.a.y)
			if x < ix {
				inside = !inside
			}
		}
	}
	return inside
}

func mustParsePath(d string) []seg {
	toks := tokenizeSVG(d)
	var segs []seg
	var cur, start pt
	i := 0
	nextF := func() float64 {
		v, err := strconv.ParseFloat(toks[i], 64)
		if err != nil {
			panic(fmt.Sprintf("bad float %q: %v", toks[i], err))
		}
		i++
		return v
	}
	for i < len(toks) {
		cmd := toks[i]
		i++
		switch cmd {
		case "M":
			cur = pt{nextF(), nextF()}
			start = cur
		case "L":
			nxt := pt{nextF(), nextF()}
			segs = append(segs, seg{cur, nxt})
			cur = nxt
		case "C":
			p1 := pt{nextF(), nextF()}
			p2 := pt{nextF(), nextF()}
			p3 := pt{nextF(), nextF()}
			prev := cur
			const steps = 18
			for s := 1; s <= steps; s++ {
				t := float64(s) / steps
				mt := 1 - t
				p := pt{
					x: mt*mt*mt*cur.x + 3*mt*mt*t*p1.x + 3*mt*t*t*p2.x + t*t*t*p3.x,
					y: mt*mt*mt*cur.y + 3*mt*mt*t*p1.y + 3*mt*t*t*p2.y + t*t*t*p3.y,
				}
				segs = append(segs, seg{prev, p})
				prev = p
			}
			cur = p3
		case "Z", "z":
			if cur != start {
				segs = append(segs, seg{cur, start})
				cur = start
			}
		default:
			panic("unsupported SVG command: " + cmd)
		}
	}
	return segs
}

func tokenizeSVG(d string) []string {
	var out []string
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			out = append(out, buf.String())
			buf.Reset()
		}
	}
	for _, r := range d {
		switch {
		case unicode.IsSpace(r) || r == ',':
			flush()
		case unicode.IsLetter(r):
			flush()
			out = append(out, string(r))
		default:
			buf.WriteRune(r)
		}
	}
	flush()
	return out
}
