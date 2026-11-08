package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"finallyshell/internal/config"
	"finallyshell/internal/importer"
	"finallyshell/locales"

	"github.com/egoist/mygo/ui"
)

// fakeSaver stands in for the dialog asking where to save a file.
type fakeSaver struct {
	saveMu    sync.Mutex
	savePath  string // what it returns: "" for canceled
	saveErr   error
	saveAsked []savePick
}

// savePick is what a place to save was asked for with.
type savePick struct {
	parent                WindowKind
	title, path, typeName string
	extensions            []string
}

func (p *fakeSaver) SaveFile(parent WindowKind, title, path, typeName string, extensions []string) (string, error) {
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	p.saveAsked = append(p.saveAsked, savePick{parent, title, path, typeName, extensions})
	return p.savePath, p.saveErr
}

// pickSave is what the next save dialogs return.
func (p *fakeSaver) pickSave(path string, err error) {
	p.saveMu.Lock()
	p.savePath, p.saveErr = path, err
	p.saveMu.Unlock()
}

func (p *fakeSaver) savePicks() []savePick {
	p.saveMu.Lock()
	defer p.saveMu.Unlock()
	return slices.Clone(p.saveAsked)
}

// tableSample is the importer's sample server list name.
func tableSample(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "importer", "testdata", "table", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// openTable opens the import window on the server lists, with path
// chosen in the file chooser, and waits for it to read.
func openTable(t *testing.T, a *App, p *fakePlatform, path string) *ui.Tester {
	t.Helper()
	a.importKind = importer.Table
	p.pickTyped(path)
	_, tt := openImport(t, a, p)
	if a.imp.source.Kind != importer.Table {
		t.Fatalf("opened on %s", a.imp.source.Kind)
	}
	p.until(t, "the list read", func() bool { return !a.imp.busy })
	tt.Frame()
	return tt
}

// A CSV with passwords: the hosts are added in their folders, their
// passwords go in the credential store, never in hosts.json, and the
// window shows which rows have one without showing it.
func TestImportTable(t *testing.T) {
	a, p := testApp(t)
	saved := savedHosts(a)
	list := tableSample(t, "servers-en.csv")
	tt := openTable(t, a, p, list)
	picks := p.typedPicks()
	if len(picks) != 1 || picks[0].title != locales.ChooseTableFile() || picks[0].typeName != locales.TableFileTypes() ||
		!slices.Equal(picks[0].extensions, []string{"csv", "tsv", "txt", "xlsx", "xls"}) {
		t.Fatalf("picks %+v", picks)
	}
	for _, s := range []string{"web-1", "web-2", "db-1", "bastion", "lab", "192.0.2.11:2222", "db.example.com:5432", "[2001:db8::1]:2200",
		"Production / Web", "Production / Databases", "ops", locales.ImportColPassword(), locales.TableNote(), locales.SaveTemplate(),
		locales.ImportCount(5), locales.ImportProblems(2, 0),
		locales.ProblemLine("servers-en.csv:7", locales.ProblemNoHost()), locales.ProblemLine("servers-en.csv:8", locales.ProblemBadPort())} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	dots := 0
	for _, s := range tt.Texts() {
		if s == "••••" {
			dots++
		}
		if strings.Contains(s, "sample-password") || strings.Contains(s, "sample,password") {
			t.Errorf("a password shown: %q", s)
		}
	}
	if dots != 2 {
		t.Errorf("%d rows with a password; texts %q", dots, tt.Texts())
	}
	saveShot(t, tt, "import-table-en.png")
	p.SetLocale("zh-Hans")
	tt.Frame()
	for _, s := range []string{"CSV 或 Excel 文件", "密码", "保存模板…", "列表里的密码存进系统的密钥库，不写进连接文件。", "servers-en.csv:8：端口不在 1 到 65535 之间"} {
		if !tt.HasText(s) {
			t.Errorf("missing %q; texts %q", s, tt.Texts())
		}
	}
	saveShot(t, tt, "import-table-zh.png")
	tt.SetDark(true)
	tt.Frame()
	saveShot(t, tt, "import-table-dark-zh.png")
	tt.SetDark(false)
	p.SetLocale("")
	tt.Frame()

	if err := tt.Click(locales.ImportCount(5)); err != nil {
		t.Fatalf("%v; texts %q", err, tt.Texts())
	}
	hosts := a.hosts.Live()
	var got []string
	for _, h := range hosts {
		got = append(got, a.folderLabel(h.Folder)+"|"+h.Title()+"|"+h.Target())
	}
	want := []string{"Production / Web|web-1|root@192.0.2.10", "Production / Web|web-2|deploy@192.0.2.11:2222",
		"Production / Databases|db-1|postgres@db.example.com:5432", "|bastion|ops@bastion.example.com", "|lab|pi@2001:db8::1:2200"}
	if !slices.Equal(got, want) {
		t.Fatalf("hosts %q, want %q", got, want)
	}
	if h := hosts[1]; h.Auth != config.AuthKey || h.KeyPath != "~/.ssh/id_ed25519" {
		t.Errorf("web-2 %+v", h)
	}
	if hosts[0].Notes != "Front end" || hosts[2].Notes != `Primary database, "main"` {
		t.Errorf("notes %q %q", hosts[0].Notes, hosts[2].Notes)
	}
	for i, pw := range map[int]string{0: "sample-password-1", 2: "sample,password-2"} {
		key := hostSecretKey(hosts[i].ID, secretPassword)
		p.until(t, "the password stored", func() bool { return p.secrets()[key] == pw })
		if !hosts[i].RemembersPassword() {
			t.Errorf("%s does not remember its password", hosts[i].Title())
		}
	}
	if len(p.secrets()) != 2 {
		t.Errorf("secrets %v", p.secrets())
	}
	if !strings.Contains(string(*saved), "db.example.com") || strings.Contains(string(*saved), "sample-password") || strings.Contains(string(*saved), "sample,password") {
		t.Errorf("saved %s", *saved)
	}
	manager := p.show(t, WindowManager)
	manager.Frame()
	if want := locales.ImportResult(5, 0, 2); !manager.HasText(want) {
		t.Errorf("no %q; texts %q", want, manager.Texts())
	}
}

// Excel workbooks, and lists without a host column or that are not
// workbooks it reads, say what is wrong.
func TestImportTableFiles(t *testing.T) {
	a, p := testApp(t)
	tt := openTable(t, a, p, tableSample(t, "excel.xlsx"))
	if !tt.HasText("東京") || !tt.HasText("生产 / 网站") || !tt.HasText("192.0.2.60:2222") || !tt.HasText(locales.ImportColPassword()) {
		t.Errorf("texts %q", tt.Texts())
	}
	// In this order: the check after the loop is of the list read last,
	// which has no passwords.
	for _, c := range []struct{ path, want string }{
		{tableSample(t, "libreoffice.xlsx"), "calc-1"},
		{tableSample(t, "no-host.csv"), locales.TableNoHostColumn("“Name”, “Port”, “User”")},
	} {
		path, want := c.path, c.want
		p.pickTyped(path)
		if err := tt.Click(locales.ChooseFile()); err != nil {
			t.Fatalf("%v; texts %q", err, tt.Texts())
		}
		p.until(t, "the list read", func() bool { return !a.imp.busy })
		tt.Frame()
		if !tt.HasText(want) {
			t.Errorf("%s: no %q; texts %q", path, want, tt.Texts())
		}
	}
	if tt.HasText(locales.ImportColPassword()) {
		t.Error("a password column for a list without passwords")
	}
	saveShot(t, tt, "import-table-no-host-en.png")
	xls := filepath.Join(t.TempDir(), "old.xls")
	os.WriteFile(xls, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"), 0o600)
	p.pickTyped(xls)
	if err := tt.Click(locales.ChooseFile()); err != nil {
		t.Fatal(err)
	}
	p.until(t, "the list read", func() bool { return !a.imp.busy })
	tt.Frame()
	if !tt.HasText(locales.TableWorkbook()) {
		t.Errorf("texts %q", tt.Texts())
	}
	a.closeWindow(WindowImport)
}

// "Save template…" writes the template where the user says; read back,
// it is the example host, in either language.
func TestImportTableTemplate(t *testing.T) {
	for _, lang := range []string{"", "zh-Hans"} {
		a, p := testApp(t)
		p.SetLocale(lang)
		a.importKind = importer.Table
		_, tt := openImport(t, a, p) // the chooser canceled
		if !tt.HasText(locales.TableNotFound()) {
			t.Fatalf("texts %q", tt.Texts())
		}
		if lang == "" {
			saveShot(t, tt, "import-table-choose-en.png")
		}

		// Canceled, then failing, then saved.
		path := filepath.Join(t.TempDir(), "servers.csv")
		for _, c := range []struct {
			path string
			err  error
			want string
		}{
			{"", nil, ""},
			{"", errors.New("no portal"), locales.TemplateSaveFailed("no portal")},
			{path, nil, locales.TemplateSaved(path)},
		} {
			p.pickSave(c.path, c.err)
			if err := tt.Click(locales.SaveTemplate()); err != nil {
				t.Fatalf("%v; texts %q", err, tt.Texts())
			}
			p.until(t, "the template saved", func() bool { return !a.imp.saving })
			tt.Frame()
			if c.want != "" && !tt.HasText(c.want) {
				t.Errorf("no %q; texts %q", c.want, tt.Texts())
			}
		}
		picks := p.savePicks()
		if len(picks) != 3 || picks[0].parent != WindowImport || picks[0].title != locales.SaveTemplateTitle() ||
			picks[0].path != filepath.Join(p.DownloadsDir(), "servers.csv") || picks[0].typeName != locales.CsvFiles() || !slices.Equal(picks[0].extensions, []string{"csv"}) {
			t.Errorf("picks %+v", picks)
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.HasPrefix(data, []byte("\xef\xbb\xbf"+locales.TableColName()+","+locales.TableColHost()+",")) {
			t.Fatalf("template %q, %v", data, err)
		}
		if lang == "zh-Hans" {
			saveShot(t, tt, "import-table-template-zh.png")
		}

		p.pickTyped(path)
		if err := tt.Click(locales.ChooseFile()); err != nil {
			t.Fatal(err)
		}
		p.until(t, "the template read", func() bool { return !a.imp.busy && a.imp.src != nil })
		src := a.imp.src
		if len(src.Entries) != 1 || len(src.Problems) != 0 {
			t.Fatalf("%s: %+v", lang, src)
		}
		e := src.Entries[0]
		if h := e.Host; h.Name != locales.TableExampleName() || h.Addr != "192.0.2.10" || h.PortOr22() != 22 || h.User != "root" ||
			h.Notes != locales.TableExampleNotes() || e.Password != "" || e.KeyFile != "" || strings.Join(e.Folder, "/") != locales.TableExampleFolder() {
			t.Errorf("%s: example %+v %+v", lang, e, h)
		}
		// Each header of the template is its column's, in each language.
		for _, col := range importer.TableColumns {
			if got := importer.ColumnOf(tableHeader(col)); got != col {
				t.Errorf("%s: %q is %q, not %q", lang, tableHeader(col), got, col)
			}
		}
		a.closeWindow(WindowImport)
	}
}

func TestTableErrorText(t *testing.T) {
	many := &importer.NoHostColumnError{}
	for i := range maxHeadersShown + 3 {
		many.Headers = append(many.Headers, string(rune('a'+i)))
	}
	if got := tableErrorText(many); !strings.Contains(got, "“l”"+locales.ListSeparator()+"…") || strings.Contains(got, "“m”") {
		t.Errorf("many: %q", got)
	}
	if got := tableErrorText(errors.New("other")); got != "" {
		t.Errorf("other: %q", got)
	}
	if got := tableErrorText(importer.ErrTooLarge); got != locales.TableTooLarge() {
		t.Errorf("too large: %q", got)
	}
}
