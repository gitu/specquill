package sketch

import (
	"bytes"
	"image/png"
	"strings"
	"testing"
)

func TestParseSceneForms(t *testing.T) {
	if sc, err := ParseScene(`[{"type":"rectangle"}]`); err != nil || len(sc.Elements) != 1 {
		t.Fatalf("bare array: %v", err)
	}
	if sc, err := ParseScene(`{"elements":[{"type":"text","text":"a"}],"appState":{"k":1}}`); err != nil || len(sc.Elements) != 1 || sc.AppState["k"] == nil {
		t.Fatalf("object form: %v", err)
	}
	if _, err := ParseScene(`not json`); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := ParseScene(`{"foo":1}`); err == nil {
		t.Fatal("object without elements accepted")
	}
}

func TestNormalizeExpandsLabels(t *testing.T) {
	sc, err := ParseScene(`[
		{"type":"rectangle","x":10,"y":10,"width":170,"height":60,"label":"OMS"},
		{"type":"arrow","x":180,"y":40,"points":[[0,0],[80,0]],"label":"fills"},
		{"type":"text","x":0,"y":120,"text":"note"}
	]`)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.Normalize(); err != nil {
		t.Fatal(err)
	}
	// rect + its label, arrow + its label, free text
	if len(sc.Elements) != 5 {
		t.Fatalf("got %d elements", len(sc.Elements))
	}
	rect, rectLabel, arrow, arrowLabel, note := sc.Elements[0], sc.Elements[1], sc.Elements[2], sc.Elements[3], sc.Elements[4]
	if _, ok := rect["label"]; ok {
		t.Fatal("label property survived normalization")
	}
	if rectLabel["containerId"] != rect["id"] {
		t.Fatalf("label not bound: %v vs %v", rectLabel["containerId"], rect["id"])
	}
	bound, _ := rect["boundElements"].([]any)
	if len(bound) != 1 || bound[0].(map[string]any)["id"] != rectLabel["id"] {
		t.Fatalf("container boundElements = %v", rect["boundElements"])
	}
	// label is measured and centered on the container
	lw, lh := num(rectLabel, "width", 0), num(rectLabel, "height", 0)
	if lw <= 0 || lh <= 0 {
		t.Fatalf("label unmeasured: %vx%v", lw, lh)
	}
	if cx, lx := 10.0+170/2, num(rectLabel, "x", 0)+lw/2; cx != lx {
		t.Fatalf("label off-center: container %v label %v", cx, lx)
	}
	if arrowLabel["containerId"] != arrow["id"] || str(arrowLabel, "textAlign") != "center" {
		t.Fatalf("arrow label not bound/centered: %v", arrowLabel)
	}
	// free text keeps its position and gets dimensions
	if num(note, "width", 0) <= 0 || str(note, "verticalAlign") != "top" {
		t.Fatalf("free text not normalized: %v", note)
	}
	// ids are unique
	seen := map[string]bool{}
	for _, el := range sc.Elements {
		id := str(el, "id")
		if id == "" || seen[id] {
			t.Fatalf("bad/duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestNormalizeWrapsLongLabels(t *testing.T) {
	sc, _ := ParseScene(`[{"type":"rectangle","x":0,"y":0,"width":170,"height":60,"label":"a very long caption that cannot fit on one single line"}]`)
	if err := sc.Normalize(); err != nil {
		t.Fatal(err)
	}
	label := sc.Elements[1]
	if !strings.Contains(str(label, "text"), "\n") {
		t.Fatalf("label not wrapped: %q", label["text"])
	}
	if !strings.Contains(str(label, "originalText"), "single line") {
		t.Fatalf("originalText missing: %v", label["originalText"])
	}
	if w := num(label, "width", 0); w > 170 {
		t.Fatalf("wrapped label wider than container: %v", w)
	}
}

func TestNormalizeRejectsMalformedElements(t *testing.T) {
	sc, _ := ParseScene(`[{"x":1}]`)
	if err := sc.Normalize(); err == nil {
		t.Fatal("typeless element accepted")
	}
}

func TestExportPNGRoundTrip(t *testing.T) {
	sc, err := ParseScene(`[
		{"type":"rectangle","x":10,"y":10,"width":170,"height":60,"label":"Order Service","backgroundColor":"#e5edfb","strokeColor":"#2563c9"},
		{"type":"ellipse","x":300,"y":10,"width":170,"height":60,"label":"Risk"},
		{"type":"diamond","x":10,"y":150,"width":170,"height":80},
		{"type":"arrow","x":180,"y":40,"points":[[0,0],[120,0]],"label":"orders"}
	]`)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.Normalize(); err != nil {
		t.Fatal(err)
	}
	data, err := sc.ExportPNG()
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("not a decodable PNG: %v", err)
	}
	if img.Bounds().Dx() < 100 || img.Bounds().Dy() < 100 {
		t.Fatalf("implausible size %v", img.Bounds())
	}
	// something actually drawn
	opaque := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 4 {
		for x := b.Min.X; x < b.Max.X; x += 4 {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				opaque++
			}
		}
	}
	if opaque == 0 {
		t.Fatal("rendered image is fully transparent")
	}
	// the scene survives embedded and intact
	scene, err := ExtractScene(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"type": "excalidraw"`, "Order Service", `"containerId"`, "orders"} {
		if !strings.Contains(scene, want) {
			t.Fatalf("embedded scene missing %q:\n%s", want, scene)
		}
	}
}

func TestClipPolylineCutsLabelBox(t *testing.T) {
	parts := clipPolyline([][2]float64{{0, 0}, {100, 0}}, [4]float64{40, -5, 60, 5})
	if len(parts) != 2 {
		t.Fatalf("parts = %v", parts)
	}
	if parts[0][1][0] != 40 || parts[1][0][0] != 60 {
		t.Fatalf("cut points wrong: %v", parts)
	}
	// a segment that misses the box stays whole
	whole := clipPolyline([][2]float64{{0, 20}, {100, 20}}, [4]float64{40, -5, 60, 5})
	if len(whole) != 1 || whole[0][0] != [2]float64{0, 20} || whole[0][1] != [2]float64{100, 20} {
		t.Fatalf("miss clipped: %v", whole)
	}
}

func TestExportPNGEmptySceneAndSizeGuard(t *testing.T) {
	sc, _ := ParseScene(`[]`)
	if err := sc.Normalize(); err != nil {
		t.Fatal(err)
	}
	if _, err := sc.ExportPNG(); err != nil {
		t.Fatalf("empty scene: %v", err)
	}
	big, _ := ParseScene(`[{"type":"rectangle","x":0,"y":0,"width":99999,"height":10}]`)
	_ = big.Normalize()
	if _, err := big.ExportPNG(); err == nil {
		t.Fatal("oversized scene accepted")
	}
}
