package app

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"

	"finalshelly/locales"

	"github.com/egoist/mygo/ui/editor"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// textEncoding is a character set the editor reads and saves files in.
type textEncoding string

const (
	encUTF8    textEncoding = "UTF-8"
	encGBK     textEncoding = "GBK"
	encGB18030 textEncoding = "GB18030"
)

// textEncodings are the encodings the status bar offers.
var textEncodings = []textEncoding{encUTF8, encGBK, encGB18030}

// utf8BOM is the byte order mark some editors start UTF-8 files with.
var utf8BOM = []byte("\xef\xbb\xbf")

func (e textEncoding) codec() encoding.Encoding {
	switch e {
	case encGBK:
		return simplifiedchinese.GBK
	case encGB18030:
		return simplifiedchinese.GB18030
	}
	return nil
}

// decodedText is a file's text as the editor holds it.
type decodedText struct {
	text   string // with \n line endings
	ending string // the line ending to save with: "\n" or "\r\n"
	bom    bool   // the file starts with UTF-8's byte order mark, which text leaves out
}

// decodeText reads raw in enc. UTF-8 keeps bytes that are not UTF-8 as
// they are, so that a file saves back as it was where it was not edited.
// Line endings become \n, and the file's, \r\n when most of its lines
// end in it, is what it saves with: a file with both saves with one.
func decodeText(raw []byte, enc textEncoding) decodedText {
	var d decodedText
	if enc == encUTF8 && bytes.HasPrefix(raw, utf8BOM) {
		raw, d.bom = raw[len(utf8BOM):], true
	}
	if codec := enc.codec(); codec != nil {
		// Bytes that are not of the encoding become U+FFFD.
		if out, err := codec.NewDecoder().Bytes(raw); err == nil {
			raw = out
		}
	}
	text := string(raw)
	crlf := strings.Count(text, "\r\n")
	lf := strings.Count(text, "\n") - crlf
	d.ending = "\n"
	if crlf > 0 {
		text = strings.ReplaceAll(text, "\r\n", "\n")
		if crlf >= lf {
			d.ending = "\r\n"
		}
	}
	d.text = text
	return d
}

// unencodableError is a character the encoding a file saves in has not.
type unencodableError struct {
	enc  textEncoding
	char rune
	line int // from 1
}

func (e *unencodableError) Error() string {
	return locales.EditorCannotEncode(e.line, string(e.enc), string(e.char))
}

// encodeText is the bytes of a file of text, its line endings \n, saved
// in enc with ending.
func encodeText(text string, enc textEncoding, ending string, bom bool) ([]byte, error) {
	if ending == "\r\n" {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	codec := enc.codec()
	if codec == nil {
		if bom {
			return append(bytes.Clone(utf8BOM), text...), nil
		}
		return []byte(text), nil
	}
	out, err := codec.NewEncoder().Bytes([]byte(text))
	if err == nil {
		return out, nil
	}
	// Say which character it was.
	e := codec.NewEncoder()
	line := 1
	for _, r := range text {
		if r == '\n' {
			line++
		}
		if r == utf8.RuneError {
			continue
		}
		if _, err := e.String(string(r)); err != nil {
			return nil, &unencodableError{enc, r, line}
		}
	}
	return nil, errors.Join(errors.New(string(enc)), err)
}

// languageOf is the language of the file p, by its name, or for a
// script without an extension by its #! line; nil for plain text.
func languageOf(p, text string) *editor.Language {
	if l := editor.LanguageFor(p); l != nil {
		return l
	}
	first, _, _ := strings.Cut(text, "\n")
	if !strings.HasPrefix(first, "#!") {
		return nil
	}
	fields := strings.Fields(strings.TrimPrefix(first, "#!"))
	if len(fields) == 0 {
		return nil
	}
	prog := fields[0][strings.LastIndex(fields[0], "/")+1:]
	if prog == "env" {
		for _, f := range fields[1:] {
			if !strings.HasPrefix(f, "-") {
				prog = f
				break
			}
		}
	}
	switch {
	case prog == "sh" || prog == "bash" || prog == "zsh" || prog == "ksh" || prog == "dash" || prog == "ash" || prog == "fish":
		return editor.LanguageNamed("Shell")
	case strings.HasPrefix(prog, "python"):
		return editor.LanguageNamed("Python")
	case prog == "ruby", prog == "lua", prog == "php", prog == "node":
		return editor.LanguageNamed(prog)
	}
	return nil
}
