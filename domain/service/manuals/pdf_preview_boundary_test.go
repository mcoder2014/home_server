package manuals

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mcoder2014/home_server/config"
	"github.com/mcoder2014/home_server/domain/model"
	"github.com/stretchr/testify/require"
)

func TestPDFPreviewStrictlyBoundsDecodedJPEG(t *testing.T) {
	pdftoppm, err := exec.LookPath("pdftoppm")
	if err != nil {
		t.Skip("pdftoppm is required for the real PDF preview boundary test")
	}
	prlimit, err := exec.LookPath("prlimit")
	if err != nil {
		t.Skip("prlimit is required for the real PDF preview boundary test")
	}
	root := filepath.Join(t.TempDir(), "manuals")
	conf := config.ManualsConfig{Enabled: true, StorageRoot: root, PDFToPPMPath: pdftoppm, PRLimitPath: prlimit}
	require.NoError(t, Init(&conf, "", ""))

	stored, err := StoreFile(&conf, 9, 10, 11, "boundary.pdf", bytes.NewReader(pdfPreviewBoundaryFixture()))
	require.NoError(t, err)
	require.Equal(t, model.ManualItemPDF, stored.Kind)
	require.Equal(t, model.ManualPreviewReady, stored.PreviewStatus)
	preview, err := os.Open(filepath.Join(root, filepath.FromSlash(stored.ThumbnailKey)))
	require.NoError(t, err)
	defer preview.Close()
	decoded, format, err := image.Decode(preview)
	require.NoError(t, err)
	require.Equal(t, "jpeg", format)
	require.LessOrEqual(t, decoded.Bounds().Dx(), ThumbnailMaxEdge)
	require.LessOrEqual(t, decoded.Bounds().Dy(), ThumbnailMaxEdge)
}

func pdfPreviewBoundaryFixture() []byte {
	var document bytes.Buffer
	document.WriteString("%PDF-1.4\n")
	offsets := make([]int, 5)
	writeObject := func(id int, body string) {
		offsets[id] = document.Len()
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", id, body)
	}
	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObject(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObject(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << >> >>")
	stream := "0.2 0.6 0.9 rg\n20 20 160 160 re f\n"
	writeObject(4, fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream))
	xref := document.Len()
	document.WriteString("xref\n0 5\n0000000000 65535 f \n")
	for id := 1; id <= 4; id++ {
		fmt.Fprintf(&document, "%010d 00000 n \n", offsets[id])
	}
	fmt.Fprintf(&document, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return document.Bytes()
}
