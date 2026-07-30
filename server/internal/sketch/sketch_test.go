package sketch

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"strings"
	"testing"
)

// a real 1x1 transparent PNG (signature + IHDR + IDAT + IEND)
const tinyPngB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func tinyPng(t *testing.T) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(tinyPngB64)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const scene = `{"type":"excalidraw","version":2,"source":"test","elements":[{"type":"rectangle","x":10,"y":10,"width":170,"height":60}],"appState":{},"files":{}}`

func TestEmbedExtractRoundTrip(t *testing.T) {
	png, err := EmbedScene(tinyPng(t), scene)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExtractScene(png)
	if err != nil {
		t.Fatal(err)
	}
	if got != scene {
		t.Fatalf("round trip mismatch:\n%s", got)
	}
	// re-embedding replaces the chunk instead of stacking a second one
	png2, err := EmbedScene(png, `{"type":"excalidraw","version":2,"elements":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := ExtractScene(png2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got2, `"elements":[]`) || bytes.Count(png2, []byte("application/vnd.excalidraw+json")) != 1 {
		t.Fatalf("re-embed did not replace: %s", got2)
	}
}

// mirror excalidraw's exact writer independently of EmbedScene: envelope with
// a zlib byte-string in a tEXt chunk — guards the reader against drift in our
// own writer.
func TestExtractExcalidrawFormat(t *testing.T) {
	var deflated bytes.Buffer
	zw := zlib.NewWriter(&deflated)
	zw.Write([]byte(scene))
	zw.Close()
	var bstr strings.Builder
	for _, c := range deflated.Bytes() {
		bstr.WriteRune(rune(c))
	}
	env, _ := json.Marshal(map[string]any{"version": "1", "encoding": "bstring", "compressed": true, "encoded": bstr.String()})
	// latin-1 the envelope like png-chunk-text does
	var payload []byte
	for _, r := range string(env) {
		payload = append(payload, byte(r))
	}
	chunkData := append(append([]byte("application/vnd.excalidraw+json"), 0), payload...)
	var chunk bytes.Buffer
	binary.Write(&chunk, binary.BigEndian, uint32(len(chunkData)))
	chunk.WriteString("tEXt")
	chunk.Write(chunkData)
	crc := crc32.NewIEEE()
	crc.Write([]byte("tEXt"))
	crc.Write(chunkData)
	binary.Write(&chunk, binary.BigEndian, crc.Sum32())

	base := tinyPng(t)
	iend := bytes.Index(base, []byte("IEND")) - 4 // chunk length precedes the type
	png := append(append(append([]byte{}, base[:iend]...), chunk.Bytes()...), base[iend:]...)

	got, err := ExtractScene(png)
	if err != nil {
		t.Fatal(err)
	}
	if got != scene {
		t.Fatalf("mismatch:\n%s", got)
	}
}

func TestExtractRefusesPlainImage(t *testing.T) {
	if _, err := ExtractScene(tinyPng(t)); err == nil {
		t.Fatal("plain image accepted")
	}
	if _, err := ExtractScene([]byte("not a png")); err == nil {
		t.Fatal("non-png accepted")
	}
}
