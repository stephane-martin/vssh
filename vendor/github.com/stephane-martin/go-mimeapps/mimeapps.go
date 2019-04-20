package mimeapps

import (
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/go-ini/ini"
)

type ErrUnknownType struct {
	Filename string
}

func (e ErrUnknownType) Error() string {
	return fmt.Sprintf("unknown type: %s", e.Filename)
}

type ErrNoDesktopEntry struct {
	Mimetype string
}

func (e ErrNoDesktopEntry) Error() string {
	return fmt.Sprintf("no desktop entry was found for mimetype: %s", e.Mimetype)
}

type ErrDesktopFileNotFound struct {
	EntryName string
}

func (e ErrDesktopFileNotFound) Error() string {
	return fmt.Sprintf("no matching desktop file was found for desktop entry: %s", e.EntryName)
}

type ErrInvalidDesktopFile struct {
	Path   string
	Reason string
}

func (e ErrInvalidDesktopFile) Error() string {
	return fmt.Sprintf("desktop entry %s is invalid because: %s", filepath.Base(e.Path), e.Reason)
}

func MimetypeToDesktopEntry(mimetype string) (string, error) {
	var section *ini.Section
	list, err := MimeAppsPathsList()
	if err != nil {
		return "", err
	}
	for _, path := range list {
		f, err := ini.Load(path)
		if err != nil {
			return "", err
		}
		section, err = f.GetSection("Default Applications")
		if err == nil {
			k := section.Key(mimetype)
			if k != nil && k.String() != "" {
				return k.String(), nil
			}
		}
	}
	list, err = DefaultsPathsList()
	if err != nil {
		return "", err
	}
	for _, path := range list {
		f, err := ini.Load(path)
		if err != nil {
			return "", err
		}
		if filepath.Base(path) == "defaults.list" {
			section, err = f.GetSection("Default Applications")
		} else {
			section, err = f.GetSection("MIME Cache")
		}
		if err != nil {
			continue
		}
		k := section.Key(mimetype)
		if k != nil && k.String() != "" {
			desktop := strings.TrimSpace(strings.SplitN(k.String(), ";", 2)[0])
			if desktop != "" {
				return desktop, nil
			}
		}
	}

	return "", ErrNoDesktopEntry{Mimetype: mimetype}
}

var ErrFound = errors.New("found")

func MimetypeToDesktopFile(mimetype string) (string, error) {
	xdg, err := XDG()
	if err != nil {
		return "", err
	}
	desktopEntry, err := MimetypeToDesktopEntry(mimetype)
	if err != nil {
		return "", err
	}
	var search []string
	d := filepath.Join(xdg.DataHome, "applications")
	if DirExists(d) {
		search = append(search, d)
	}
	for _, d := range xdg.DataDirs {
		d = filepath.Join(d, "applications")
		if DirExists(d) {
			search = append(search, d)
		}
	}
	desktopFilePath := ""
	for _, dirname := range search {
		err := filepath.Walk(dirname, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if filepath.Base(path) == desktopEntry {
				desktopFilePath = path
				return ErrFound
			}
			return nil
		})
		if err == ErrFound {
			break
		}
	}
	if desktopFilePath == "" {
		return "", ErrDesktopFileNotFound{EntryName: desktopEntry}
	}
	return desktopFilePath, nil
}

func MimeTypeToApplication(mimetype string) (string, bool, error) {
	path, err := MimetypeToDesktopFile(mimetype)
	if err != nil {
		return path, false, err
	}
	f, err := ini.Load(path)
	if err != nil {
		return "", false, err
	}
	section, err := f.GetSection("Desktop Entry")
	if err != nil {
		return "", false, ErrInvalidDesktopFile{Path: path, Reason: "section Desktop Entry is absent"}
	}
	k := section.Key("Exec")
	if k != nil && k.String() != "" {
		kt := section.Key("Terminal")
		if kt != nil && kt.String() == "true" {
			return k.String(), true, nil
		}
		return k.String(), false, nil
	}
	return "", false, ErrInvalidDesktopFile{Path: path, Reason: "no Exec key"}
}

func FilenameToApplication(filename string) ([]string, bool, error) {
	var (
		m   string
		err error
	)
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" {
		m = mime.TypeByExtension(ext)
	}
	if m == "" {
		m, _, err = mimetype.DetectFile(filename)
		if m == "" || m == "application/octet-stream" || err != nil {
			return nil, false, ErrUnknownType{Filename: filename}
		}
	}
	mt, _, err := mime.ParseMediaType(m)
	if err != nil {
		return nil, false, err
	}
	app, terminal, err := MimeTypeToApplication(mt)
	if err != nil {
		return nil, false, err
	}
	return Scan(app), terminal, nil
}

func Scan(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	s = strings.Replace(s, `\\`, `\`, -1)
	var tokens []string
	var token strings.Builder
	var insideQuotes bool
	runes := []rune(s)
	nbRunes := len(runes)
	for i := 0; i < nbRunes; i++ {
		current := string(runes[i])
		next := ""
		if i < (nbRunes - 1) {
			next = string(runes[i+1])
		}

		if insideQuotes {
			if current == `\` {
				// escaped special char
				token.WriteString(next)
				i++
			} else if current == `"` {
				// end of quoted string
				t := strings.TrimSpace(token.String())
				if t != "" {
					tokens = append(tokens, t)
				}
				token.Reset()
				insideQuotes = false
			} else {
				token.WriteString(current)
			}
		} else {
			if current == `"` {
				// beginning of quoted string
				token.Reset()
				insideQuotes = true
			} else if current == ` ` {
				// next token
				t := strings.TrimSpace(token.String())
				if t != "" {
					tokens = append(tokens, t)
				}
				token.Reset()
			} else {
				token.WriteString(current)
			}

		}
	}
	t := strings.TrimSpace(token.String())
	if t != "" {
		tokens = append(tokens, t)
	}

	return tokens
}
