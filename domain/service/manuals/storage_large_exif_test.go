package manuals

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"
)

func TestJPEGOrientationReadsLargeAPP1Segment(t *testing.T) {
	payload := make([]byte, 8<<10)
	copy(payload, []byte("Exif\x00\x00II"))
	binary.LittleEndian.PutUint16(payload[8:10], 42)
	binary.LittleEndian.PutUint32(payload[10:14], 8)
	binary.LittleEndian.PutUint16(payload[14:16], 1)
	binary.LittleEndian.PutUint16(payload[16:18], 0x0112)
	binary.LittleEndian.PutUint16(payload[18:20], 3)
	binary.LittleEndian.PutUint32(payload[20:24], 1)
	binary.LittleEndian.PutUint16(payload[24:26], 6)
	segmentLength := len(payload) + 2
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe1, byte(segmentLength >> 8), byte(segmentLength)}
	jpeg = append(jpeg, payload...)
	jpeg = append(jpeg, 0xff, 0xd9)

	if orientation := jpegOrientation(bufio.NewReader(bytes.NewReader(jpeg))); orientation != 6 {
		t.Fatalf("large EXIF APP1 orientation = %d, want 6", orientation)
	}
}
