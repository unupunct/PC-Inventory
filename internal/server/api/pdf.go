package api

import (
	"bytes"
	"fmt"
	"html"
	"strings"
)

// renderHTML builds a printable report page.
func renderHTML(title string, sections []reportSection, cols []string, rows [][]string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>` + html.EscapeString(title) + `</title>
<style>
body{font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;margin:24px;color:#111}
h1{font-size:20px;margin:0 0 4px}
.meta{color:#555;font-size:12px;margin-bottom:16px}
h2{font-size:14px;margin:18px 0 6px}
table{border-collapse:collapse;width:100%;font-size:11px}
th,td{border:1px solid #ccc;padding:3px 5px;text-align:left;vertical-align:top}
th{background:#eee}
tr:nth-child(even) td{background:#fafafa}
@media print{body{margin:8mm} @page{size:A4 landscape;margin:8mm}}
</style></head><body>`)
	b.WriteString("<h1>" + html.EscapeString(title) + "</h1>")
	b.WriteString(`<div class="meta">PC Inventory Manager &middot; generated from live agent data</div>`)
	for _, s := range sections {
		if s.Heading != "" {
			b.WriteString("<h2>" + html.EscapeString(s.Heading) + "</h2>")
		}
		if len(s.Columns) > 0 {
			writeTable(&b, s.Columns, s.Rows)
		}
	}
	if len(cols) > 0 {
		writeTable(&b, cols, rows)
	}
	b.WriteString("</body></html>")
	return b.String()
}

func writeTable(b *strings.Builder, cols []string, rows [][]string) {
	b.WriteString("<table><thead><tr>")
	for _, c := range cols {
		b.WriteString("<th>" + html.EscapeString(c) + "</th>")
	}
	b.WriteString("</tr></thead><tbody>")
	for _, r := range rows {
		b.WriteString("<tr>")
		for _, c := range r {
			b.WriteString("<td>" + html.EscapeString(c) + "</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</tbody></table>")
}

// renderPDF produces a real PDF (PDF 1.4, built-in Helvetica) with one table
// per page in landscape A4. No external dependencies are needed.
func renderPDF(title string, sections []reportSection, cols []string, rows [][]string, count int) ([]byte, error) {
	const (
		pageW  = 842.0
		pageH  = 595.0
		margin = 28.0
	)
	// Column widths: proportional to the longest cell text (bounded).
	widths := make([]float64, len(cols))
	total := 0.0
	for i, c := range cols {
		maxLen := len(c)
		for _, r := range rows {
			if i < len(r) {
				if l := len(r[i]); l > maxLen {
					maxLen = l
				}
			}
		}
		if maxLen > 40 {
			maxLen = 40
		}
		if maxLen < 6 {
			maxLen = 6
		}
		widths[i] = float64(maxLen)
		total += widths[i]
	}
	avail := pageW - 2*margin
	for i := range widths {
		widths[i] = widths[i] / total * avail
	}

	var pages []string
	newPage := func() *strings.Builder {
		b := &strings.Builder{}
		return b
	}
	cur := newPage()
	y := pageH - margin
	line := func(s string, size float64, x float64) {
		cur.WriteString(fmt.Sprintf("BT /F1 %.1f Tf %.2f %.2f Td (%s) Tj ET\n", size, x, y, pdfEscape(s)))
	}
	line("PC Inventory Manager", 9, margin)
	line(title, 15, margin)
	y -= 12
	line(fmt.Sprintf("Generated %s from live agent data", nowLocal()), 8, margin)
	y -= 16
	for _, s := range sections {
		if s.Heading != "" {
			line(s.Heading, 9, margin)
			y -= 14
		}
	}
	// Table header
	drawRow := func(cells []string, size float64, bold bool, fill bool) {
		x := margin
		if fill {
			cur.WriteString(fmt.Sprintf("0.92 0.92 0.92 rg %.2f %.2f %.2f 13 re f 0 0 0 rg\n", margin, y-3, avail))
		}
		for i, c := range cells {
			if i >= len(widths) {
				break
			}
			text := truncate(c, int(widths[i]/(size*0.52)))
			font := "/F1"
			if bold {
				font = "/F2"
			}
			cur.WriteString(fmt.Sprintf("BT %s %.1f Tf %.2f %.2f Td (%s) Tj ET\n", font, size, x+2, y, pdfEscape(text)))
			x += widths[i]
		}
		y -= 13
	}
	if len(cols) > 0 {
		drawRow(cols, 7.5, true, true)
		for _, r := range rows {
			if y < margin+20 {
				pages = append(pages, cur.String())
				cur = newPage()
				y = pageH - margin
				drawRow(cols, 7.5, true, true)
			}
			drawRow(r, 7, false, false)
		}
	}
	if len(sections) > 0 && len(cols) == 0 {
		for _, s := range sections {
			if len(s.Rows) == 0 {
				continue
			}
			drawRow(s.Columns, 7.5, true, true)
			for _, r := range s.Rows {
				drawRow(r, 7, false, false)
			}
		}
	}
	pages = append(pages, cur.String())
	return buildPDF(pages, pageW, pageH, title), nil
}

func truncate(s string, max int) string {
	if max <= 1 {
		return ""
	}
	if len([]rune(s)) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

func pdfEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, "(", `\(`)
	s = strings.ReplaceAll(s, ")", `\)`)
	// Replace non-WinAnsi characters (the built-in fonts are WinAnsi).
	var b strings.Builder
	for _, r := range s {
		if r < 256 {
			b.WriteRune(r)
		} else if r == '…' {
			b.WriteString("...")
		} else {
			b.WriteByte('?')
		}
	}
	return b.String()
}

func buildPDF(pageContents []string, w, h float64, title string) []byte {
	var out bytes.Buffer
	offsets := []int{}
	write := func(s string) {
		out.WriteString(s)
	}
	obj := func(n int) {
		offsets = append(offsets, out.Len())
		write(fmt.Sprintf("%d 0 obj\n", n))
	}
	write("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	nPages := len(pageContents)
	// object numbering: 1 catalog, 2 pages, 3 font F1, 4 font F2, then per page: content + page
	obj(1)
	write("<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	obj(2)
	kids := make([]string, nPages)
	for i := 0; i < nPages; i++ {
		kids[i] = fmt.Sprintf("%d 0 R", 5+i*2+1)
	}
	write(fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>\nendobj\n", nPages, strings.Join(kids, " ")))
	obj(3)
	write("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>\nendobj\n")
	obj(4)
	write("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>\nendobj\n")
	for i, content := range pageContents {
		cNum := 5 + i*2
		pNum := cNum + 1
		obj(cNum)
		write(fmt.Sprintf("<< /Length %d >>\nstream\n", len(content)))
		write(content)
		write("endstream\nendobj\n")
		obj(pNum)
		write(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 3 0 R /F2 4 0 R >> >> /Contents %d 0 R >>\nendobj\n", w, h, cNum))
	}
	xrefPos := out.Len()
	total := len(offsets) + 1
	write(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", total))
	for _, off := range offsets {
		write(fmt.Sprintf("%010d 00000 n \n", off))
	}
	write(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R /Info << /Title (%s) /Producer (PC Inventory Manager) >> >>\nstartxref\n%d\n%%%%EOF\n", total, pdfEscape(title), xrefPos))
	return out.Bytes()
}
