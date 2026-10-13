package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"testing"
)

func TestAssetName(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "leptostack_linux_amd64.tar.gz"},
		{"darwin", "arm64", "leptostack_darwin_arm64.tar.gz"},
		{"windows", "amd64", "leptostack_windows_amd64.zip"},
		{"windows", "arm64", "leptostack_windows_arm64.zip"},
	}
	for _, c := range cases {
		if got := assetName(c.goos, c.goarch); got != c.want {
			t.Errorf("assetName(%q,%q) = %q want %q", c.goos, c.goarch, got, c.want)
		}
	}
}

func tarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipBytes(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractBinaryTarGz(t *testing.T) {
	payload := []byte("ELF-binary")
	for _, name := range []string{"leptostack", "dist/leptostack"} {
		got, err := extractBinary(tarGz(t, name, payload), "leptostack_linux_amd64.tar.gz")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("%s: got %q", name, got)
		}
	}
}

func TestExtractBinaryZip(t *testing.T) {
	payload := []byte("PE-binary")
	for _, name := range []string{"leptostack.exe", "Leptostack.EXE", "bin/leptostack.exe"} {
		got, err := extractBinary(zipBytes(t, name, payload), "leptostack_windows_amd64.zip")
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, payload) {
			t.Errorf("%s: got %q", name, got)
		}
	}
}

func TestExtractBinaryRawPassthrough(t *testing.T) {
	payload := []byte("raw")
	got, err := extractBinary(payload, "leptostack_linux_amd64")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q", got)
	}
}

func TestExtractBinaryMissing(t *testing.T) {
	if _, err := extractBinary(tarGz(t, "README.md", []byte("x")), "leptostack_linux_amd64.tar.gz"); err == nil {
		t.Error("expected an error when the binary is absent from a tar.gz")
	}
	if _, err := extractBinary(zipBytes(t, "README.md", []byte("x")), "leptostack_windows_amd64.zip"); err == nil {
		t.Error("expected an error when the binary is absent from a zip")
	}
}
