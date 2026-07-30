// Package sketch reads and writes the excalidraw scene embedded in
// *.excalidraw.png files. The format (excalidraw's export-embed-scene): a
// PNG tEXt chunk keyed "application/vnd.excalidraw+json" whose text is a
// JSON envelope {version, encoding:"bstring", compressed, encoded} — the
// encoded field being a latin-1 byte string of the zlib-deflated scene JSON.
package sketch

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"strings"
)

const keyword = "application/vnd.excalidraw+json"

var pngSig = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// latin1 bytes → string (each byte is one rune), the inverse of byteString.
func latin1ToString(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

// string → latin1 bytes (each rune's low byte) — excalidraw's byte strings.
func stringToLatin1(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		out = append(out, byte(r))
	}
	return out
}

type envelope struct {
	Version    string `json:"version"`
	Encoding   string `json:"encoding"`
	Compressed bool   `json:"compressed"`
	Encoded    string `json:"encoded"`
}

// ExtractScene returns the scene JSON embedded in an excalidraw PNG export.
func ExtractScene(png []byte) (string, error) {
	if !bytes.HasPrefix(png, pngSig) {
		return "", fmt.Errorf("not a PNG file")
	}
	rest := png[len(pngSig):]
	for len(rest) >= 12 {
		length := binary.BigEndian.Uint32(rest[:4])
		typ := string(rest[4:8])
		if uint32(len(rest)) < 12+length {
			break
		}
		data := rest[8 : 8+length]
		rest = rest[12+length:]
		if typ != "tEXt" {
			if typ == "IEND" {
				break
			}
			continue
		}
		sep := bytes.IndexByte(data, 0)
		if sep < 0 || string(data[:sep]) != keyword {
			continue
		}
		text := latin1ToString(data[sep+1:])
		var env envelope
		if err := json.Unmarshal([]byte(text), &env); err != nil || env.Encoded == "" {
			// pre-envelope exports carry the raw scene JSON directly
			if strings.Contains(text, `"excalidraw"`) {
				return text, nil
			}
			return "", fmt.Errorf("unrecognized scene payload")
		}
		if env.Encoding != "bstring" {
			return "", fmt.Errorf("unknown scene encoding %q", env.Encoding)
		}
		raw := stringToLatin1(env.Encoded)
		if !env.Compressed {
			return string(raw), nil
		}
		zr, err := zlib.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", fmt.Errorf("scene inflate: %v", err)
		}
		defer zr.Close()
		scene, err := io.ReadAll(zr)
		if err != nil {
			return "", fmt.Errorf("scene inflate: %v", err)
		}
		return string(scene), nil
	}
	return "", fmt.Errorf("no embedded scene — plain image?")
}

// EmbedScene inserts (or replaces) the scene chunk in a PNG, excalidraw
// format-compatible. Used by tests and future re-embedding; the image pixels
// are untouched, so a re-embedded preview can lag the scene.
func EmbedScene(png []byte, scene string) ([]byte, error) {
	if !bytes.HasPrefix(png, pngSig) {
		return nil, fmt.Errorf("not a PNG file")
	}
	var deflated bytes.Buffer
	zw := zlib.NewWriter(&deflated)
	if _, err := zw.Write([]byte(scene)); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	env, err := json.Marshal(envelope{Version: "1", Encoding: "bstring", Compressed: true, Encoded: latin1ToString(deflated.Bytes())})
	if err != nil {
		return nil, err
	}
	chunkData := append(append([]byte(keyword), 0), stringToLatin1(string(env))...)
	var chunk bytes.Buffer
	_ = binary.Write(&chunk, binary.BigEndian, uint32(len(chunkData)))
	chunk.WriteString("tEXt")
	chunk.Write(chunkData)
	crc := crc32.NewIEEE()
	crc.Write([]byte("tEXt"))
	crc.Write(chunkData)
	_ = binary.Write(&chunk, binary.BigEndian, crc.Sum32())

	// rebuild: copy every chunk except old scene tEXt, insert before IEND
	out := bytes.NewBuffer(nil)
	out.Write(pngSig)
	rest := png[len(pngSig):]
	for len(rest) >= 12 {
		length := binary.BigEndian.Uint32(rest[:4])
		if uint32(len(rest)) < 12+length {
			return nil, fmt.Errorf("truncated PNG")
		}
		typ := string(rest[4:8])
		whole := rest[:12+length]
		data := rest[8 : 8+length]
		rest = rest[12+length:]
		if typ == "tEXt" {
			if sep := bytes.IndexByte(data, 0); sep >= 0 && string(data[:sep]) == keyword {
				continue // replace the old scene chunk
			}
		}
		if typ == "IEND" {
			out.Write(chunk.Bytes())
		}
		out.Write(whole)
	}
	return out.Bytes(), nil
}
