package engine

import "testing"

func TestExecIntakeSkipsALineWithAnUnknownField(t *testing.T) {
	cmd := lpIntakeScript(t, `
echo '{"id":"a","title":"Good one"}'
echo '{"id":"b","title":"Typo in a field","prioriti":5}'
echo '{"id":"c","title":"Two values"} {"id":"d","title":"Second"}'
`)
	env := lpNewEngine(t, lpIntakeDoc(t, lpIntakeSource("inbox", cmd, "", "")))

	env.run(loopExecIntake)

	tasks := env.tasks("")
	if len(tasks) != 1 || tasks[0].Title != "Good one" {
		var got []string
		for _, tk := range tasks {
			got = append(got, tk.SourceRef+"="+tk.Title)
		}
		t.Fatalf("tasks = %v, want exactly one task titled \"Good one\"", got)
	}
}

func TestDecodeIntakeLine(t *testing.T) {
	full := `{"id":"a","title":"t","body":"b","kind":"bugfix","priority":3,"role":"implementer"}`
	want := lpIntakeLine{ID: "a", Title: "t", Body: "b", Kind: "bugfix", Priority: 3, Role: "implementer"}

	cases := []struct {
		name    string
		raw     string
		want    lpIntakeLine
		wantErr bool
	}{
		{name: "all six fields", raw: full, want: want},
		{name: "unknown field", raw: `{"id":"a","title":"t","extra":1}`, wantErr: true},
		{name: "two values on one line", raw: `{"id":"a","title":"t"} {}`, wantErr: true},
		{name: "trailing spaces", raw: `{"id":"a","title":"t"}   `, want: lpIntakeLine{ID: "a", Title: "t"}},
		{name: "not json", raw: `not json`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := lpDecodeIntakeLine([]byte(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("lpDecodeIntakeLine(%q) = %+v, want an error", tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("lpDecodeIntakeLine(%q) error = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("lpDecodeIntakeLine(%q) = %+v, want %+v", tc.raw, got, tc.want)
			}
		})
	}
}
