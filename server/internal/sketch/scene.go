// Scene parsing and normalization for model-drawn sketches. The speccy hands
// us a loose element subset (shapes with an optional `label`, bare texts,
// arrows); Normalize turns that into elements excalidraw's restore accepts —
// stable ids, measured text boxes, container-bound labels — so the sketch
// opens cleanly in the editor and renders the same everywhere.
package sketch

import (
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"
)

// Scene is a parsed excalidraw document. Elements stay loose maps so fields
// we don't model (roundness, bindings, freedraw pressures…) survive a
// read-modify-redraw round trip untouched.
type Scene struct {
	Elements []map[string]any
	AppState map[string]any
	Files    map[string]any
}

// ParseScene accepts a full scene object or a bare elements array.
func ParseScene(raw string) (*Scene, error) {
	raw = strings.TrimSpace(raw)
	sc := &Scene{AppState: map[string]any{}, Files: map[string]any{}}
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &sc.Elements); err != nil {
			return nil, fmt.Errorf("scene is not valid JSON: %v", err)
		}
		return sc, nil
	}
	var doc struct {
		Elements []map[string]any `json:"elements"`
		AppState map[string]any   `json:"appState"`
		Files    map[string]any   `json:"files"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return nil, fmt.Errorf("scene is not valid JSON: %v", err)
	}
	if doc.Elements == nil {
		return nil, fmt.Errorf("scene needs an elements array")
	}
	sc.Elements = doc.Elements
	if doc.AppState != nil {
		sc.AppState = doc.AppState
	}
	if doc.Files != nil {
		sc.Files = doc.Files
	}
	return sc, nil
}

// DocJSON renders the canonical on-disk envelope (also what gets embedded in
// the PNG scene chunk).
func (sc *Scene) DocJSON() (string, error) {
	out, err := json.MarshalIndent(map[string]any{
		"type": "excalidraw", "version": 2, "source": "specquill",
		"elements": sc.Elements, "appState": sc.AppState, "files": sc.Files,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

const (
	defaultStroke   = "#1a1e24"
	defaultFontSize = 16
	lineHeight      = 1.25
	labelPad        = 10 // keep wrapped labels off the container border
)

func num(el map[string]any, key string, def float64) float64 {
	if v, ok := el[key].(float64); ok {
		return v
	}
	return def
}

func str(el map[string]any, key string) string {
	v, _ := el[key].(string)
	return v
}

func setDefault(el map[string]any, key string, v any) {
	if _, ok := el[key]; !ok {
		el[key] = v
	}
}

// points returns an arrow/line element's relative point list.
func points(el map[string]any) [][2]float64 {
	raw, _ := el["points"].([]any)
	pts := make([][2]float64, 0, len(raw))
	for _, p := range raw {
		pair, _ := p.([]any)
		if len(pair) < 2 {
			continue
		}
		x, _ := pair[0].(float64)
		y, _ := pair[1].(float64)
		pts = append(pts, [2]float64{x, y})
	}
	return pts
}

// Normalize fills in what the model reliably gets wrong: ids (stable, needed
// for text↔container binding), element defaults excalidraw expects, measured
// text dimensions, and `label` expansion into a centered bound text element.
func (sc *Scene) Normalize() error {
	used := map[string]bool{}
	for _, el := range sc.Elements {
		if id := str(el, "id"); id != "" {
			used[id] = true
		}
	}
	serial := 0
	newID := func() string {
		for {
			serial++
			id := fmt.Sprintf("sq-%d", serial)
			if !used[id] {
				used[id] = true
				return id
			}
		}
	}

	out := make([]map[string]any, 0, len(sc.Elements))
	for i, el := range sc.Elements {
		if el == nil {
			return fmt.Errorf("elements[%d] is not an object", i)
		}
		typ := str(el, "type")
		if typ == "" {
			return fmt.Errorf("elements[%d] has no type", i)
		}
		if str(el, "id") == "" {
			el["id"] = newID()
		}
		id := str(el, "id")
		setDefault(el, "x", float64(0))
		setDefault(el, "y", float64(0))
		setDefault(el, "angle", float64(0))
		setDefault(el, "strokeColor", defaultStroke)
		setDefault(el, "backgroundColor", "transparent")
		setDefault(el, "fillStyle", "solid")
		setDefault(el, "strokeWidth", float64(2))
		setDefault(el, "strokeStyle", "solid")
		setDefault(el, "roughness", float64(1))
		setDefault(el, "opacity", float64(100))
		setDefault(el, "groupIds", []any{})
		setDefault(el, "isDeleted", false)
		setDefault(el, "version", float64(1))
		setDefault(el, "seed", float64(crc32.ChecksumIEEE([]byte(id))))

		switch typ {
		case "rectangle", "ellipse", "diamond":
			setDefault(el, "width", float64(170))
			setDefault(el, "height", float64(60))
		case "arrow", "line":
			pts := points(el)
			if len(pts) < 2 {
				pts = [][2]float64{{0, 0}, {num(el, "width", 100), num(el, "height", 0)}}
				el["points"] = []any{[]any{pts[0][0], pts[0][1]}, []any{pts[1][0], pts[1][1]}}
			}
			minX, minY, maxX, maxY := pts[0][0], pts[0][1], pts[0][0], pts[0][1]
			for _, p := range pts {
				minX, maxX = min(minX, p[0]), max(maxX, p[0])
				minY, maxY = min(minY, p[1]), max(maxY, p[1])
			}
			el["width"], el["height"] = maxX-minX, maxY-minY
			if typ == "arrow" {
				setDefault(el, "endArrowhead", "arrow")
			}
		case "text":
			normalizeText(el, 0)
		}

		out = append(out, el)

		// label: "…" on a shape or arrow becomes a proper container-bound
		// text element — centered, measured, wrapped — so the model never
		// has to place or size captions itself.
		label, _ := el["label"].(string)
		delete(el, "label")
		if label == "" {
			continue
		}
		switch typ {
		case "rectangle", "ellipse", "diamond", "arrow":
		default:
			continue
		}
		txt := map[string]any{
			"id": newID(), "type": "text", "text": label,
			"fontSize":    num(el, "fontSize", defaultFontSize),
			"strokeColor": str(el, "strokeColor"),
			"containerId": id, "textAlign": "center", "verticalAlign": "middle",
			"angle": float64(0), "backgroundColor": "transparent", "fillStyle": "solid",
			"strokeWidth": float64(1), "strokeStyle": "solid", "roughness": float64(1),
			"opacity": float64(100), "groupIds": []any{}, "isDeleted": false,
			"version": float64(1), "seed": float64(crc32.ChecksumIEEE([]byte(id + "/label"))),
		}
		delete(el, "fontSize") // label sizing hint, not a shape property
		wrapWidth := num(el, "width", 0) - 2*labelPad
		if typ == "arrow" {
			wrapWidth = 160 // arrows have no box to fill — cap caption lines
		}
		normalizeText(txt, wrapWidth)
		// center on the container (arrows: on the midpoint of the path)
		cx := num(el, "x", 0) + num(el, "width", 0)/2
		cy := num(el, "y", 0) + num(el, "height", 0)/2
		if typ == "arrow" {
			pts := points(el)
			mid := pts[len(pts)/2]
			first, last := pts[0], pts[len(pts)-1]
			if len(pts) == 2 {
				mid = [2]float64{(first[0] + last[0]) / 2, (first[1] + last[1]) / 2}
			}
			cx, cy = num(el, "x", 0)+mid[0], num(el, "y", 0)+mid[1]
		}
		txt["x"] = cx - num(txt, "width", 0)/2
		txt["y"] = cy - num(txt, "height", 0)/2
		bound, _ := el["boundElements"].([]any)
		el["boundElements"] = append(bound, map[string]any{"id": str(txt, "id"), "type": "text"})
		out = append(out, txt)
	}
	sc.Elements = out
	return nil
}

// normalizeText fills a text element's font defaults and measured dimensions.
// wrapWidth > 0 word-wraps the text to fit (container labels); the unwrapped
// original is kept in originalText, matching excalidraw's bound-text shape.
func normalizeText(el map[string]any, wrapWidth float64) {
	setDefault(el, "fontSize", float64(defaultFontSize))
	setDefault(el, "fontFamily", float64(1))
	setDefault(el, "textAlign", "left")
	setDefault(el, "verticalAlign", "top")
	setDefault(el, "lineHeight", lineHeight)
	fs := num(el, "fontSize", defaultFontSize)
	text := str(el, "text")
	if wrapWidth > 0 {
		wrapped := wrapText(text, fs, wrapWidth)
		if wrapped != text {
			setDefault(el, "originalText", text)
			el["text"] = wrapped
			text = wrapped
		}
	}
	lines := strings.Split(text, "\n")
	w := 0.0
	for _, ln := range lines {
		w = max(w, measureText(ln, fs))
	}
	if num(el, "width", 0) <= 0 {
		el["width"] = w
	}
	if num(el, "height", 0) <= 0 {
		el["height"] = float64(len(lines)) * fs * lineHeight
	}
}

// wrapText breaks text into lines no wider than maxW (existing newlines are
// kept; a single word longer than maxW stays on its own line).
func wrapText(text string, fs, maxW float64) string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, para)
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if measureText(line+" "+word, fs) <= maxW {
				line += " " + word
			} else {
				out = append(out, line)
				line = word
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
