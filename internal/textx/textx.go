// Package textx extracts plain text from course files so models can read them.
package textx

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Niclassslua/mcd/internal/forum"
)

var ErrUnsupported = errors.New("unsupported file type")

// Extract returns the text of a file, choosing the extractor by extension,
// then MIME type.
func Extract(name, mime string, data []byte) (string, error) {
	ext := strings.ToLower(path.Ext(name))
	switch {
	case ext == ".pdf" || mime == "application/pdf":
		return pdfText(data)
	case ext == ".docx":
		return docxText(data)
	case ext == ".pptx":
		return pptxText(data)
	case ext == ".html" || ext == ".htm" || mime == "text/html":
		return forum.StripHTML(string(data)), nil
	case strings.HasPrefix(mime, "text/") || mime == "application/json" ||
		ext == ".txt" || ext == ".md" || ext == ".csv" || ext == ".json":
		return strings.ToValidUTF8(string(data), "�"), nil
	}
	return "", fmt.Errorf("%w: %s (%s)", ErrUnsupported, ext, mime)
}

func pdfText(data []byte) (string, error) {
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		return "", fmt.Errorf("%w: reading PDFs needs pdftotext; install poppler (brew install poppler)", ErrUnsupported)
	}
	f, err := os.CreateTemp("", "mcd-*.pdf")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-layout", "-enc", "UTF-8", f.Name(), "-").Output()
	if err != nil {
		return "", fmt.Errorf("pdftotext: %w", err)
	}
	return string(out), nil
}

func docxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	return ooxmlPart(zr, "word/document.xml")
}

var slideName = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

func pptxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	var slides []int
	for _, f := range zr.File {
		if m := slideName.FindStringSubmatch(f.Name); m != nil {
			n, _ := strconv.Atoi(m[1])
			slides = append(slides, n)
		}
	}
	sort.Ints(slides)
	var b strings.Builder
	for _, n := range slides {
		text, err := ooxmlPart(zr, fmt.Sprintf("ppt/slides/slide%d.xml", n))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "--- Slide %d ---\n%s\n", n, text)
	}
	return b.String(), nil
}

// ooxmlPart collects the <*:t> text runs of one OOXML part, one line per
// <*:p> paragraph. Word (w:) and PowerPoint (a:) share this structure.
func ooxmlPart(zr *zip.Reader, name string) (string, error) {
	f, err := zr.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	dec := xml.NewDecoder(io.LimitReader(f, 64<<20))
	var b strings.Builder
	inText := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inText = true
			case "tab":
				b.WriteByte('\t')
			case "br":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				b.Write(t)
			}
		}
	}
	return strings.TrimSpace(b.String()), nil
}
