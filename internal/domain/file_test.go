package domain

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestInspectAttachmentRecognizesImageBySignature(t *testing.T) {
	var payload bytes.Buffer
	if err := png.Encode(&payload, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	metadata, err := InspectAttachment(payload.Bytes(), "not-trusted.exe", DefaultPolicy())
	if err != nil {
		t.Fatalf("inspect image: %v", err)
	}
	if metadata.Kind != AttachmentKindImage || metadata.MIMEType != "image/png" || *metadata.Width != 3 || *metadata.Height != 2 || len(metadata.ChecksumSHA256) != 32 {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
}

func TestInspectAttachmentRejectsFileOutsideMIMEAllowlist(t *testing.T) {
	_, err := InspectAttachment([]byte("<html><body>unsafe</body></html>"), "payload.html", DefaultPolicy())
	if err == nil {
		t.Fatal("HTML attachment accepted")
	}
}
