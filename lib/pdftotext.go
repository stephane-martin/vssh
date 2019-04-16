package lib

import (
	"io"
	"io/ioutil"
	"os"
	"os/exec"
)

func PDFToText(content []byte, out io.Writer) error {
	p, err := exec.LookPath("pdftotext")
	if err != nil {
		return err
	}
	temp, err := ioutil.TempFile("", "vssh-temp-*.pdf")
	if err != nil {
		return err
	}
	path := temp.Name()
	defer func() {
		_ = temp.Close()
		_ = os.Remove(path)
	}()
	_, err = temp.Write(content)
	if err != nil {
		return err
	}
	_ = temp.Close()
	cmd := exec.Command(p, "-q", "-nopgbrk", "-enc", "UTF-8", "-eol", "unix", path, "-")
	cmd.Stdout = out
	return cmd.Run()
}
