package textx

import (
	"archive/zip"
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPPTXSlidesInNumericOrder(t *testing.T) {
	slide := func(text string) string {
		return `<p:sld xmlns:p="p" xmlns:a="a"><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:sld>`
	}
	data := zipOf(t, map[string]string{
		"ppt/slides/slide10.xml": slide("ten"),
		"ppt/slides/slide2.xml":  slide("two"),
		"ppt/slides/slide1.xml":  slide("one &amp; only"),
	})
	got, err := Extract("Folien.pptx", "", data)
	if err != nil {
		t.Fatal(err)
	}
	want := "--- Slide 1 ---\none & only\n--- Slide 2 ---\ntwo\n--- Slide 10 ---\nten\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDOCXParagraphs(t *testing.T) {
	doc := `<w:document xmlns:w="w"><w:body>
		<w:p><w:r><w:t>Hello</w:t></w:r><w:r><w:t xml:space="preserve"> world</w:t></w:r></w:p>
		<w:p><w:r><w:t>Second</w:t></w:r></w:p>
	</w:body></w:document>`
	got, err := Extract("a.docx", "", zipOf(t, map[string]string{"word/document.xml": doc}))
	if err != nil || got != "Hello world\nSecond" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestUnsupported(t *testing.T) {
	if _, err := Extract("video.mp4", "video/mp4", nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v", err)
	}
}

func TestPDF(t *testing.T) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		t.Skip("pdftotext not installed")
	}
	// Minimal one-page PDF showing "Hello PDF"; xref offsets are not validated by poppler.
	pdf := "%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 300 100]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>endobj\n" +
		"4 0 obj<</Length 41>>stream\nBT /F1 18 Tf 20 40 Td (Hello PDF) Tj ET\nendstream endobj\n" +
		"5 0 obj<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n"
	got, err := Extract("a.pdf", "application/pdf", []byte(pdf))
	if err != nil || !strings.Contains(got, "Hello PDF") {
		t.Errorf("got %q, %v", got, err)
	}
}
