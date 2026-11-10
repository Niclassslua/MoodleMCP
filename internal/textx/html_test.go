package textx

import "testing"

func TestHTMLTextSchedule(t *testing.T) {
	input := `<p>Seminar &amp; Praxis</p>
<table><tbody><tr><th>Datum</th><th>Thema</th></tr>
<tr><td><p>22.09.</p></td><td><p>Einführung #2</p><p>Zielorganisationen &amp;<br>Projektgruppen</p></td></tr>
<TR class="week"><TD>17.11.</TD><TD><em>Awareness</em><p>Gastvortrag: Lüders</p></TD></TR>
<tr><td>12.01.</td><td>&lt;Finale&gt; &amp; Diskussion</td></tr></tbody></table><p>Ende</p>`
	want := "Seminar & Praxis\nDatum | Thema\n22.09. | Einführung #2 Zielorganisationen & Projektgruppen\n17.11. | Awareness Gastvortrag: Lüders\n12.01. | <Finale> & Diskussion\nEnde"
	if got := HTMLText(input); got != want {
		t.Fatalf("HTMLText = %q, want %q", got, want)
	}
	if got := HTMLText("<p> &nbsp; </p>"); got != "" {
		t.Errorf("empty HTML = %q", got)
	}
}
