// Server-side renderer for model-drawn sketches: rasterizes the normalized
// element subset (rectangle, ellipse, diamond, arrow, line, freedraw, text)
// into the PNG whose pixels ship with the embedded scene — the same
// natively-viewable *.excalidraw.png the in-app editor exports. Text uses the
// embedded Virgil font (excalidraw's fontFamily 1); shapes are clean lines
// (no roughness). Opening the sketch in the editor re-renders from the
// scene, so fidelity of the scene beats fidelity of the pixels.
package sketch

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// Virgil — excalidraw's hand-drawn font (fontFamily 1, SIL OFL 1.1; see
// fonts/README.md) — so the rendered pixels match the in-app editor.
//
//go:embed fonts/Virgil-Regular.ttf
var virgilTTF []byte

const (
	exportScale = 2  // supersample for crisp text at typical embed sizes
	exportPad   = 20 // scene-space margin around the drawing
)

var (
	fontOnce sync.Once
	fontSF   *opentype.Font
	faceMu   sync.Mutex
	faces    = map[float64]font.Face{}
)

func face(size float64) font.Face {
	fontOnce.Do(func() {
		f, err := opentype.Parse(virgilTTF)
		if err != nil {
			panic("embedded font: " + err.Error())
		}
		fontSF = f
	})
	faceMu.Lock()
	defer faceMu.Unlock()
	if f, ok := faces[size]; ok {
		return f
	}
	f, err := opentype.NewFace(fontSF, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic("embedded font face: " + err.Error())
	}
	faces[size] = f
	return f
}

// measureText returns the rendered width of one line at the given font size,
// in scene px. Shared with Normalize so measured boxes match the pixels.
func measureText(line string, size float64) float64 {
	return float64(font.MeasureString(face(size), line)) / 64
}

// parseColor understands #rgb / #rrggbb / #rrggbbaa and "transparent";
// anything else falls back to the default stroke color so a typo'd color
// still draws visibly instead of vanishing.
func parseColor(s string) (color.NRGBA, bool) {
	if s == "" || s == "transparent" || s == "none" {
		return color.NRGBA{}, false
	}
	hex := s
	if hex[0] == '#' {
		hex = hex[1:]
	}
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 && len(hex) != 8 {
		return color.NRGBA{R: 0x1a, G: 0x1e, B: 0x24, A: 0xff}, true
	}
	v, err := strconv.ParseUint(hex, 16, 64)
	if err != nil {
		return color.NRGBA{R: 0x1a, G: 0x1e, B: 0x24, A: 0xff}, true
	}
	if len(hex) == 8 {
		return color.NRGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}, true
	}
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}, true
}

func withOpacity(c color.NRGBA, opacity float64) color.NRGBA {
	if opacity < 100 {
		c.A = uint8(float64(c.A) * opacity / 100)
	}
	return c
}

// canvas maps scene coordinates onto the output image.
type canvas struct {
	img      *image.NRGBA
	ox, oy   float64 // scene-space translation (bounds min minus padding)
	contours [][][2]float64
}

func (c *canvas) px(p [2]float64) (float32, float32) {
	return float32((p[0] - c.ox) * exportScale), float32((p[1] - c.oy) * exportScale)
}

func (c *canvas) contour(pts ...[2]float64) { c.contours = append(c.contours, pts) }

// flush rasterizes the queued contours as one fill and clears the queue.
// Every contour is forced to the same orientation first: the rasterizer's
// winding accumulation cancels overlapping regions of opposite orientation,
// which turns overlapping stroke quads and join circles into gaps otherwise.
func (c *canvas) flush(col color.NRGBA) {
	if len(c.contours) == 0 || col.A == 0 {
		c.contours = nil
		return
	}
	b := c.img.Bounds()
	z := vector.NewRasterizer(b.Dx(), b.Dy())
	for _, ct := range c.contours {
		if len(ct) < 3 {
			continue
		}
		if signedArea(ct) < 0 {
			for i, j := 0, len(ct)-1; i < j; i, j = i+1, j-1 {
				ct[i], ct[j] = ct[j], ct[i]
			}
		}
		x, y := c.px(ct[0])
		z.MoveTo(x, y)
		for _, p := range ct[1:] {
			x, y = c.px(p)
			z.LineTo(x, y)
		}
		z.ClosePath()
	}
	z.Draw(c.img, b, image.NewUniform(col), image.Point{})
	c.contours = nil
}

// stroke queues a polyline (closed or open) as thick-line quads with round
// joins — one code path covers rects, diamonds, ellipse polygons, arrows and
// freedraw alike.
func (c *canvas) stroke(pts [][2]float64, closed bool, w float64) {
	if len(pts) < 2 {
		return
	}
	segs := len(pts) - 1
	if closed {
		segs = len(pts)
	}
	for i := 0; i < segs; i++ {
		a, b := pts[i], pts[(i+1)%len(pts)]
		dx, dy := b[0]-a[0], b[1]-a[1]
		l := math.Hypot(dx, dy)
		if l == 0 {
			continue
		}
		nx, ny := -dy/l*w/2, dx/l*w/2
		c.contour([2]float64{a[0] + nx, a[1] + ny}, [2]float64{b[0] + nx, b[1] + ny},
			[2]float64{b[0] - nx, b[1] - ny}, [2]float64{a[0] - nx, a[1] - ny})
	}
	for _, p := range pts { // round joins and caps
		c.contour(circle(p[0], p[1], w/2, 12)...)
	}
}

func signedArea(pts [][2]float64) float64 {
	a := 0.0
	for i, p := range pts {
		q := pts[(i+1)%len(pts)]
		a += p[0]*q[1] - q[0]*p[1]
	}
	return a / 2
}

func circle(cx, cy, r float64, n int) [][2]float64 {
	pts := make([][2]float64, n)
	for i := range pts {
		a := 2 * math.Pi * float64(i) / float64(n)
		pts[i] = [2]float64{cx + r*math.Cos(a), cy + r*math.Sin(a)}
	}
	return pts
}

func ellipsePoly(x, y, w, h float64) [][2]float64 {
	const n = 64
	pts := make([][2]float64, n)
	for i := range pts {
		a := 2 * math.Pi * float64(i) / n
		pts[i] = [2]float64{x + w/2 + w/2*math.Cos(a), y + h/2 + h/2*math.Sin(a)}
	}
	return pts
}

func shapePoly(el map[string]any) [][2]float64 {
	x, y := num(el, "x", 0), num(el, "y", 0)
	w, h := num(el, "width", 0), num(el, "height", 0)
	switch str(el, "type") {
	case "ellipse":
		return ellipsePoly(x, y, w, h)
	case "diamond":
		return [][2]float64{{x + w/2, y}, {x + w, y + h/2}, {x + w/2, y + h}, {x, y + h/2}}
	default: // rectangle
		return [][2]float64{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}
	}
}

// absPoints returns an arrow/line/freedraw element's points in scene space.
func absPoints(el map[string]any) [][2]float64 {
	x, y := num(el, "x", 0), num(el, "y", 0)
	pts := points(el)
	out := make([][2]float64, len(pts))
	for i, p := range pts {
		out[i] = [2]float64{x + p[0], y + p[1]}
	}
	return out
}

func arrowhead(tip, prev [2]float64) ([2]float64, [2]float64) {
	a := math.Atan2(tip[1]-prev[1], tip[0]-prev[0])
	const l, spread = 12.0, 0.45
	return [2]float64{tip[0] - l*math.Cos(a-spread), tip[1] - l*math.Sin(a-spread)},
		[2]float64{tip[0] - l*math.Cos(a+spread), tip[1] - l*math.Sin(a+spread)}
}

// bounds computes the scene-space bounding box over all visible elements.
func bounds(els []map[string]any) (minX, minY, maxX, maxY float64) {
	minX, minY, maxX, maxY = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	grow := func(x, y float64) {
		minX, minY = min(minX, x), min(minY, y)
		maxX, maxY = max(maxX, x), max(maxY, y)
	}
	for _, el := range els {
		if b, _ := el["isDeleted"].(bool); b {
			continue
		}
		switch str(el, "type") {
		case "arrow", "line", "freedraw":
			for _, p := range absPoints(el) {
				grow(p[0], p[1])
			}
		default:
			x, y := num(el, "x", 0), num(el, "y", 0)
			grow(x, y)
			grow(x+num(el, "width", 0), y+num(el, "height", 0))
		}
	}
	if minX > maxX { // empty scene: a small blank canvas
		return 0, 0, 60, 30
	}
	return
}

// ExportPNG renders the scene and returns PNG bytes with the scene embedded —
// ready to save as a *.excalidraw.png sketch. Background is transparent, like
// the in-app editor's export (dark mode inverts via CSS).
func (sc *Scene) ExportPNG() ([]byte, error) {
	doc, err := sc.DocJSON()
	if err != nil {
		return nil, err
	}
	minX, minY, maxX, maxY := bounds(sc.Elements)
	minX, minY, maxX, maxY = minX-exportPad, minY-exportPad, maxX+exportPad, maxY+exportPad
	w := int(math.Ceil((maxX - minX) * exportScale))
	h := int(math.Ceil((maxY - minY) * exportScale))
	if w > 8000 || h > 8000 {
		return nil, fmt.Errorf("scene too large to render (%dx%d px) — shrink the coordinates", w, h)
	}
	c := &canvas{img: image.NewNRGBA(image.Rect(0, 0, w, h)), ox: minX, oy: minY}
	byID := map[string]map[string]any{}
	for _, el := range sc.Elements {
		if id := str(el, "id"); id != "" {
			byID[id] = el
		}
	}

	for _, el := range sc.Elements {
		if b, _ := el["isDeleted"].(bool); b {
			continue
		}
		opacity := num(el, "opacity", 100)
		strokeCol, hasStroke := parseColor(str(el, "strokeColor"))
		strokeCol = withOpacity(strokeCol, opacity)
		sw := num(el, "strokeWidth", 2)
		switch str(el, "type") {
		case "rectangle", "ellipse", "diamond":
			poly := shapePoly(el)
			if bg, ok := parseColor(str(el, "backgroundColor")); ok {
				c.contour(poly...)
				c.flush(withOpacity(bg, opacity))
			}
			if hasStroke {
				c.stroke(poly, true, sw)
				c.flush(strokeCol)
			}
		case "arrow", "line", "freedraw":
			pts := absPoints(el)
			if len(pts) < 2 || !hasStroke {
				continue
			}
			// a bound label sits ON the stroke: draw the polyline with the
			// label's box cut out so the caption stays readable
			if lr, ok := labelRect(el, byID); ok {
				for _, part := range clipPolyline(pts, lr) {
					c.stroke(part, false, sw)
				}
			} else {
				c.stroke(pts, false, sw)
			}
			if str(el, "type") == "arrow" && str(el, "endArrowhead") != "" {
				l, r := arrowhead(pts[len(pts)-1], pts[len(pts)-2])
				c.stroke([][2]float64{l, pts[len(pts)-1], r}, false, sw)
			}
			if str(el, "type") == "arrow" && str(el, "startArrowhead") != "" {
				l, r := arrowhead(pts[0], pts[1])
				c.stroke([][2]float64{l, pts[0], r}, false, sw)
			}
			c.flush(strokeCol)
		case "text":
			c.drawText(el, strokeCol)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, c.img); err != nil {
		return nil, err
	}
	return EmbedScene(buf.Bytes(), doc)
}

// labelRect returns the scene-space box (inflated by a margin) of the text
// element bound to this arrow/line, if any.
func labelRect(el map[string]any, byID map[string]map[string]any) ([4]float64, bool) {
	bound, _ := el["boundElements"].([]any)
	for _, b := range bound {
		bm, _ := b.(map[string]any)
		if bm == nil || str(bm, "type") != "text" {
			continue
		}
		txt := byID[str(bm, "id")]
		if txt == nil {
			continue
		}
		const m = 4
		x, y := num(txt, "x", 0), num(txt, "y", 0)
		return [4]float64{x - m, y - m, x + num(txt, "width", 0) + m, y + num(txt, "height", 0) + m}, true
	}
	return [4]float64{}, false
}

// clipPolyline splits a polyline into the pieces lying outside rect
// (Liang-Barsky per segment); round caps at the cut points come from stroke.
func clipPolyline(pts [][2]float64, rect [4]float64) [][][2]float64 {
	var out [][][2]float64
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		lo, hi := 0.0, 1.0 // parameter interval of the segment INSIDE rect
		inside := true
		for axis := 0; axis < 2; axis++ {
			d := b[axis] - a[axis]
			rmin, rmax := rect[axis], rect[axis+2]
			if d == 0 {
				if a[axis] < rmin || a[axis] > rmax {
					inside = false
					break
				}
				continue
			}
			t0, t1 := (rmin-a[axis])/d, (rmax-a[axis])/d
			if t0 > t1 {
				t0, t1 = t1, t0
			}
			lo, hi = max(lo, t0), min(hi, t1)
		}
		at := func(t float64) [2]float64 {
			return [2]float64{a[0] + t*(b[0]-a[0]), a[1] + t*(b[1]-a[1])}
		}
		if !inside || lo >= hi {
			out = append(out, [][2]float64{a, b})
			continue
		}
		if lo > 0.01 {
			out = append(out, [][2]float64{a, at(lo)})
		}
		if hi < 0.99 {
			out = append(out, [][2]float64{at(hi), b})
		}
	}
	return out
}

func (c *canvas) drawText(el map[string]any, col color.NRGBA) {
	fs := num(el, "fontSize", defaultFontSize)
	f := face(fs * exportScale)
	metrics := f.Metrics()
	lines := strings.Split(str(el, "text"), "\n")
	x, y := num(el, "x", 0), num(el, "y", 0)
	width := num(el, "width", 0)
	center := str(el, "textAlign") == "center"
	d := font.Drawer{Dst: c.img, Src: image.NewUniform(col), Face: f}
	for i, ln := range lines {
		lx := x
		if center && width > 0 {
			lx = x + (width-measureText(ln, fs))/2
		}
		pxX, pxY := c.px([2]float64{lx, y + float64(i)*fs*lineHeight})
		d.Dot = fixed.Point26_6{X: fixed.Int26_6(pxX * 64), Y: fixed.Int26_6(pxY*64) + metrics.Ascent}
		d.DrawString(ln)
	}
}
