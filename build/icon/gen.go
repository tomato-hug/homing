// Command gen draws Homing's tray / menu-bar icons and the click animation frames
// from the pigeon's vector parts, the way Magpie's build/icon/gen.go does: the
// frames are rendered once at build time and the app swaps them every 30 ms.
//
//	go run ./build/icon                 # writes internal/gui/assets/tray
//	go run ./build/icon -out /tmp/tray  # somewhere else
//
// Output:
//
//	mac/<state>.png, mac/<state>@2x.png        template images (black + alpha), 22 / 44 px
//	mac/tap-NN.png, mac/tap-NN@2x.png          click animation, 20 frames, 30 ms apart
//	win/<state>.ico                            colour icons, 16/20/24/32 px inside one .ico
//	win/tap-NN.ico                             click animation frames, same sizes
//
// States: ok, new (new message), wait (waiting for you), off (offline), error.
// The motion matches the "轻点" scene in docs/homing-v2/pigeon_v2.json and the
// storyboard on the design canvas. Standard library only.
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

// Bird parts in bird coordinates (same paths as the logo and the Android app).
const (
	pathFull = "M96 41 L87 37 C86 28 74 24 68 30 C64 34 64 42 62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C84 47 85 44 88 43 Z"
	pathNeck = "M63 44 C70 50 80 51 86 49 L87 55 C79 57 68 55 61 50 Z"
	pathWing = "M36 71 C50 61 66 61 76 68 C64 80 48 82 36 71 Z"
)

// The icon shows this square of bird coordinates.
// The bird sits a little low so a status mark fits above its head, top right.
const boxX, boxY, boxW = 2.0, 3.0, 100.0

var (
	feet    = pt{58, 97} // squash pivot
	eye     = pt{78, 34}
	markPos = pt{92, 13} // status mark, top right, clear of the head
)

var states = []string{"ok", "new", "wait", "off", "error"}

// Colours (Windows icons only; macOS template images are black + alpha).
var (
	cBody    = rgb(0xF1, 0xF5, 0xF9)
	cBodyOff = rgb(0x94, 0xA3, 0xB8)
	cWing    = rgb(0x64, 0x74, 0x8B)
	cEye     = rgb(0x0F, 0x17, 0x2A)
	cTeal    = rgb(0x2D, 0xD4, 0xBF)
	cAmber   = rgb(0xFB, 0xBF, 0x24)
	cRed     = rgb(0xF8, 0x71, 0x71)
	cSlate   = rgb(0x64, 0x74, 0x8B)
)

// Tap scene squash keys: time (s), scale x, scale y, lift (bird units).
var tapKeys = []struct{ t, sx, sy, dy float64 }{
	{0, 1, 1, 0}, {0.08, 1.1, 0.86, 0}, {0.20, 0.95, 1.07, -2}, {0.32, 1.02, 0.98, 0}, {0.42, 1, 1, 0}, {0.60, 1, 1, 0},
}

const (
	frameStep  = 0.030 // seconds between frames
	frameCount = 20
)

func main() {
	out := flag.String("out", "internal/gui/assets/tray", "output directory")
	flag.Parse()
	for _, d := range []string{"mac", "win"} {
		if err := os.MkdirAll(filepath.Join(*out, d), 0o755); err != nil {
			log.Fatal(err)
		}
	}
	full, neck, wing := mustPath(pathFull), mustPath(pathNeck), mustPath(pathWing)
	winSizes := []int{16, 20, 24, 32}

	for _, st := range states {
		id := squash{1, 1, 0}
		for _, s := range []struct {
			px   int
			name string
		}{{22, st + ".png"}, {44, st + "@2x.png"}} {
			write(filepath.Join(*out, "mac", s.name), encodePNG(macIcon(s.px, st, id, full, neck)))
		}
		var imgs []*image.NRGBA
		for _, px := range winSizes {
			imgs = append(imgs, winIcon(px, st, id, full, neck, wing))
		}
		write(filepath.Join(*out, "win", st+".ico"), encodeICO(imgs))
	}

	for i := 0; i < frameCount; i++ {
		sq := tapAt(float64(i) * frameStep)
		name := fmt.Sprintf("tap-%02d", i+1)
		write(filepath.Join(*out, "mac", name+".png"), encodePNG(macIcon(22, "ok", sq, full, neck)))
		write(filepath.Join(*out, "mac", name+"@2x.png"), encodePNG(macIcon(44, "ok", sq, full, neck)))
		var imgs []*image.NRGBA
		for _, px := range winSizes {
			imgs = append(imgs, winIcon(px, "ok", sq, full, neck, wing))
		}
		write(filepath.Join(*out, "win", name+".ico"), encodeICO(imgs))
	}
	fmt.Printf("wrote %d states and %d tap frames to %s\n", len(states), frameCount, *out)
}

// ---------------------------------------------------------------- drawing

type squash struct{ sx, sy, dy float64 }

func (q squash) apply(p pt) pt {
	return pt{feet.x + (p.x-feet.x)*q.sx, feet.y + (p.y-feet.y)*q.sy + q.dy}
}

// tapAt samples the tap scene at time t with the same per-segment easing as the
// CSS / Compose versions (cubic-bezier(.45,0,.55,1)).
func tapAt(t float64) squash {
	for i := 1; i < len(tapKeys); i++ {
		a, b := tapKeys[i-1], tapKeys[i]
		if t <= b.t {
			u := ease((t - a.t) / (b.t - a.t))
			return squash{lerp(a.sx, b.sx, u), lerp(a.sy, b.sy, u), lerp(a.dy, b.dy, u)}
		}
	}
	return squash{1, 1, 0}
}

// macIcon is a template image: only alpha matters, macOS tints it for light and
// dark menu bars. Eye and collar are cut out; status marks are shapes, not colours.
func macIcon(px int, state string, q squash, full, neck [][]pt) *image.NRGBA {
	r := newRaster(px)
	bird := r.cover(transform(full, q))
	holes := r.cover(transform(neck, q))
	r.union(holes, r.disc(q.apply(eye), 4.6))
	if state != "ok" && state != "off" {
		r.union(holes, r.disc(markPos, 13)) // keep the mark readable
	}
	alpha := r.minus(bird, holes)
	switch state {
	case "new":
		r.union(alpha, r.disc(markPos, 9.5))
	case "wait":
		r.union(alpha, r.ring(markPos, 5.5, 10))
	case "error":
		r.union(alpha, r.cover([][]pt{{{92, 3}, {102, 21}, {82, 21}}}))
	}
	k := 1.0
	if state == "off" {
		k = 0.4
	}
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	for i, a := range alpha {
		img.Pix[i*4+3] = uint8(math.Round(clamp01(a*k) * 255))
	}
	return img
}

// winIcon is a full-colour icon for the Windows notification area.
func winIcon(px int, state string, q squash, full, neck, wing [][]pt) *image.NRGBA {
	r := newRaster(px)
	img := image.NewNRGBA(image.Rect(0, 0, px, px))
	gap := make([]float64, px*px)
	if state == "new" || state == "wait" || state == "error" {
		gap = r.disc(markPos, 13)
	}
	keep := func(c []float64) []float64 { return r.minus(c, gap) }

	body, neckC := cBody, cTeal
	switch state {
	case "wait":
		neckC = cAmber
	case "error":
		neckC = cRed
	case "off":
		body, neckC = cBodyOff, cSlate
	}
	paint(img, keep(r.cover(transform(full, q))), body)
	paint(img, keep(r.cover(transform(wing, q))), cWing)
	paint(img, keep(r.cover(transform(neck, q))), neckC)
	paint(img, keep(r.disc(q.apply(eye), 4)), cEye)
	switch state {
	case "new":
		paint(img, r.disc(markPos, 10), cTeal)
	case "wait":
		paint(img, r.disc(markPos, 10), cAmber)
	case "error":
		paint(img, r.disc(markPos, 10), cRed)
	}
	if state == "off" {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = uint8(float64(img.Pix[i]) * 0.7)
		}
	}
	return img
}

func transform(polys [][]pt, q squash) [][]pt {
	out := make([][]pt, len(polys))
	for i, poly := range polys {
		out[i] = make([]pt, len(poly))
		for j, p := range poly {
			out[i][j] = q.apply(p)
		}
	}
	return out
}

// paint composites colour c with per-pixel coverage onto img ("over").
func paint(img *image.NRGBA, cov []float64, c color.NRGBA) {
	for i, a := range cov {
		if a <= 0 {
			continue
		}
		p := img.Pix[i*4 : i*4+4]
		da := float64(p[3]) / 255
		oa := a + da*(1-a)
		if oa == 0 {
			continue
		}
		for k, sc := range []uint8{c.R, c.G, c.B} {
			p[k] = uint8(math.Round((float64(sc)*a + float64(p[k])*da*(1-a)) / oa))
		}
		p[3] = uint8(math.Round(oa * 255))
	}
}

// ---------------------------------------------------------------- raster

// raster maps bird coordinates onto a px×px grid and measures coverage with
// 5×5 supersampling and the even-odd rule.
type raster struct {
	px    int
	scale float64
}

const ss = 5

func newRaster(px int) raster { return raster{px, float64(px) / boxW} }

func (r raster) sample(fn func(p pt) bool) []float64 {
	cov := make([]float64, r.px*r.px)
	for y := 0; y < r.px; y++ {
		for x := 0; x < r.px; x++ {
			hit := 0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					p := pt{
						boxX + (float64(x)+(float64(sx)+0.5)/ss)/r.scale,
						boxY + (float64(y)+(float64(sy)+0.5)/ss)/r.scale,
					}
					if fn(p) {
						hit++
					}
				}
			}
			cov[y*r.px+x] = float64(hit) / (ss * ss)
		}
	}
	return cov
}

func (r raster) cover(polys [][]pt) []float64 {
	return r.sample(func(p pt) bool {
		in := false
		for _, poly := range polys {
			if inside(poly, p) {
				in = !in
			}
		}
		return in
	})
}

func (r raster) disc(c pt, rad float64) []float64 {
	return r.sample(func(p pt) bool { return math.Hypot(p.x-c.x, p.y-c.y) <= rad })
}

func (r raster) ring(c pt, in, out float64) []float64 {
	return r.sample(func(p pt) bool {
		d := math.Hypot(p.x-c.x, p.y-c.y)
		return d >= in && d <= out
	})
}

func (r raster) union(dst, src []float64) {
	for i := range dst {
		dst[i] = math.Max(dst[i], src[i])
	}
}

func (r raster) minus(a, b []float64) []float64 {
	out := make([]float64, len(a))
	for i := range a {
		out[i] = clamp01(a[i] - b[i])
	}
	return out
}

func inside(poly []pt, p pt) bool {
	in := false
	for i, j := 0, len(poly)-1; i < len(poly); j, i = i, i+1 {
		a, b := poly[i], poly[j]
		if (a.y > p.y) != (b.y > p.y) && p.x < (b.x-a.x)*(p.y-a.y)/(b.y-a.y)+a.x {
			in = !in
		}
	}
	return in
}

// ---------------------------------------------------------------- SVG path subset

type pt struct{ x, y float64 }

// parsePath reads absolute M, L, H, V, C, Q and Z commands and flattens curves
// into polylines (one per subpath).
func parsePath(d string) ([][]pt, error) {
	toks := tokenize(d)
	var polys [][]pt
	var cur []pt
	var pos pt
	cmd := byte(0)
	num := func(i *int) (float64, error) {
		if *i >= len(toks) {
			return 0, fmt.Errorf("path ends early")
		}
		v, err := strconv.ParseFloat(toks[*i], 64)
		*i++
		return v, err
	}
	for i := 0; i < len(toks); {
		if t := toks[i]; len(t) == 1 && unicode.IsLetter(rune(t[0])) {
			cmd = t[0]
			i++
		}
		switch cmd {
		case 'M':
			x, err := num(&i)
			if err != nil {
				return nil, err
			}
			y, err := num(&i)
			if err != nil {
				return nil, err
			}
			if len(cur) > 0 {
				polys = append(polys, cur)
			}
			pos = pt{x, y}
			cur = []pt{pos}
			cmd = 'L'
		case 'L':
			x, err := num(&i)
			if err != nil {
				return nil, err
			}
			y, err := num(&i)
			if err != nil {
				return nil, err
			}
			pos = pt{x, y}
			cur = append(cur, pos)
		case 'H':
			x, err := num(&i)
			if err != nil {
				return nil, err
			}
			pos = pt{x, pos.y}
			cur = append(cur, pos)
		case 'V':
			y, err := num(&i)
			if err != nil {
				return nil, err
			}
			pos = pt{pos.x, y}
			cur = append(cur, pos)
		case 'C', 'Q':
			n := 3
			if cmd == 'Q' {
				n = 2
			}
			ctrl := make([]pt, n)
			for k := range ctrl {
				x, err := num(&i)
				if err != nil {
					return nil, err
				}
				y, err := num(&i)
				if err != nil {
					return nil, err
				}
				ctrl[k] = pt{x, y}
			}
			p0 := pos
			for s := 1; s <= 16; s++ {
				t := float64(s) / 16
				if n == 3 {
					cur = append(cur, cubic(p0, ctrl[0], ctrl[1], ctrl[2], t))
				} else {
					cur = append(cur, quad(p0, ctrl[0], ctrl[1], t))
				}
			}
			pos = ctrl[n-1]
		case 'Z', 'z':
			if len(cur) > 0 {
				polys = append(polys, cur)
				pos = cur[0]
				cur = nil
			}
			cmd = 0
		default:
			return nil, fmt.Errorf("unsupported path command %q", cmd)
		}
	}
	if len(cur) > 0 {
		polys = append(polys, cur)
	}
	return polys, nil
}

func tokenize(d string) []string {
	var out []string
	var b strings.Builder
	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}
	for _, c := range d {
		switch {
		case unicode.IsLetter(c) && c != 'e':
			flush()
			out = append(out, string(c))
		case c == ' ' || c == ',' || c == '\n' || c == '\t':
			flush()
		case c == '-' && b.Len() > 0 && !strings.HasSuffix(b.String(), "e"):
			flush()
			b.WriteRune(c)
		default:
			b.WriteRune(c)
		}
	}
	flush()
	return out
}

func mustPath(d string) [][]pt {
	p, err := parsePath(d)
	if err != nil {
		log.Fatal(err)
	}
	return p
}

func cubic(p0, p1, p2, p3 pt, t float64) pt {
	u := 1 - t
	a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
	return pt{a*p0.x + b*p1.x + c*p2.x + d*p3.x, a*p0.y + b*p1.y + c*p2.y + d*p3.y}
}

func quad(p0, p1, p2 pt, t float64) pt {
	u := 1 - t
	return pt{u*u*p0.x + 2*u*t*p1.x + t*t*p2.x, u*u*p0.y + 2*u*t*p1.y + t*t*p2.y}
}

// ease is cubic-bezier(.45,0,.55,1), solved for x by bisection.
func ease(x float64) float64 {
	x = clamp01(x)
	bez := func(t, p1, p2 float64) float64 {
		u := 1 - t
		return 3*u*u*t*p1 + 3*u*t*t*p2 + t*t*t
	}
	lo, hi := 0.0, 1.0
	for i := 0; i < 40; i++ {
		mid := (lo + hi) / 2
		if bez(mid, 0.45, 0.55) < x {
			lo = mid
		} else {
			hi = mid
		}
	}
	return bez((lo+hi)/2, 0, 1)
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func rgb(r, g, b uint8) color.NRGBA { return color.NRGBA{r, g, b, 255} }

// ---------------------------------------------------------------- files

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatal(err)
	}
	return buf.Bytes()
}

// encodeICO packs PNG-compressed images into one .ico (supported since Vista).
func encodeICO(imgs []*image.NRGBA) []byte {
	var head, body bytes.Buffer
	le := binary.LittleEndian
	_ = binary.Write(&head, le, [3]uint16{0, 1, uint16(len(imgs))})
	offset := 6 + 16*len(imgs)
	for _, img := range imgs {
		data := encodePNG(img)
		w := img.Bounds().Dx()
		_ = binary.Write(&head, le, struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{uint8(w % 256), uint8(w % 256), 0, 0, 1, 32, uint32(len(data)), uint32(offset)})
		body.Write(data)
		offset += len(data)
	}
	return append(head.Bytes(), body.Bytes()...)
}

func write(path string, data []byte) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatal(err)
	}
}
